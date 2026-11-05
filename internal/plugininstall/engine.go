package plugininstall

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Error codes shown to the user and stored on the row. They are fixed strings: no value from the configuration and no
// Agent/HTTP body text ever reaches the row, the events or the API.
const (
	CodeAgentOffline       = "agent_offline"
	CodeAgentRejected      = "agent_rejected"
	CodeIdentityFailed     = "identity_failed"
	CodeRegisterFailed     = "register_failed"
	CodeHealthFailed       = "health_failed"
	CodeConfigMissing      = "config_missing"
	CodeGenerationConflict = "generation_conflict"
	CodePluginUnknown      = "plugin_unknown"
)

var (
	ErrUnknownPlugin = errors.New("plugin is not in the signed catalog")
	ErrInvalidConfig = errors.New("invalid plugin configuration")
	ErrBadState      = errors.New("operation is not allowed in the current state")
	ErrAgentDown     = errors.New("the host agent is offline")
)

// Timing knobs; tests shrink them.
type Timing struct {
	Lease          time.Duration // how long one reconcile pass owns a row
	MaintainEvery  time.Duration // grant/identity renewal and health probe cadence for enabled rows
	IdentityTTL    time.Duration
	GrantTTL       time.Duration
	HealthWait     time.Duration
	HealthInterval time.Duration
	MaxAttempts    int // consecutive waiting passes (agent offline) before the operation fails
}

func DefaultTiming() Timing {
	return Timing{Lease: 5 * time.Minute, MaintainEvery: 5 * time.Minute, IdentityTTL: 24 * time.Hour, GrantTTL: 24 * time.Hour,
		HealthWait: 90 * time.Second, HealthInterval: 3 * time.Second, MaxAttempts: 40}
}

type Engine struct {
	Repo     *Repo
	Agent    Agent
	IDs      Identities
	Services Services
	Health   Health
	Now      func() time.Time
	Owner    string
	Timing   Timing

	kick chan struct{}
	once sync.Once
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) trigger() {
	e.once.Do(func() { e.kick = make(chan struct{}, 1) })
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Run is the in-process reconcile loop (no Encore cron, no external queue). It resumes unfinished operations after a
// restart because everything it needs is in the table.
func (e *Engine) Run(ctx context.Context, every time.Duration) {
	e.once.Do(func() { e.kick = make(chan struct{}, 1) })
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		e.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.kick:
		}
	}
}

func (e *Engine) Tick(ctx context.Context) {
	now := e.now()
	rows, err := e.Repo.Candidates(now, now.Add(-e.Timing.MaintainEvery))
	if err != nil {
		return
	}
	for i := range rows {
		_ = e.Reconcile(ctx, rows[i].ID)
	}
}

// ---- public operations (called by the API) ----

type CatalogItem struct {
	CatalogEntry
	Installation *Installation `json:"installation,omitempty"`
}

func (e *Engine) entry(ctx context.Context, plugin string) (*CatalogEntry, error) {
	list, err := e.Agent.Catalog(ctx)
	if err != nil {
		return nil, ErrAgentDown
	}
	for i := range list {
		if list[i].ID == plugin {
			return &list[i], nil
		}
	}
	return nil, ErrUnknownPlugin
}

func (e *Engine) Catalog(ctx context.Context, tenant string) ([]CatalogItem, error) {
	list, err := e.Agent.Catalog(ctx)
	if err != nil {
		return nil, ErrAgentDown
	}
	rows, err := e.Repo.List(tenant)
	if err != nil {
		return nil, err
	}
	byPlugin := map[string]*Installation{}
	for i := range rows {
		byPlugin[rows[i].PluginID] = &rows[i]
	}
	out := make([]CatalogItem, 0, len(list))
	for _, c := range list {
		out = append(out, CatalogItem{CatalogEntry: c, Installation: byPlugin[c.ID]})
	}
	return out, nil
}

// Submit creates or reconfigures the installation of a plugin for a tenant and starts it when the configuration is
// complete. It is idempotent: the same tenant and plugin always address the same row, so repeated clicks, a replayed
// Idempotency-Key or a second browser tab cannot create a second instance. Secret values go straight to the Agent and
// are not kept here.
func (e *Engine) Submit(ctx context.Context, tenant, plugin, idemKey string, values map[string]string) (*Installation, error) {
	entry, err := e.entry(ctx, plugin)
	if err != nil {
		return nil, err
	}
	public, secret, err := splitValues(entry, values)
	if err != nil {
		return nil, err
	}
	row, err := e.Repo.ByPlugin(tenant, plugin)
	if errors.Is(err, ErrNotFound) {
		now := e.now()
		row = &Installation{ID: uuid.NewString(), TenantID: tenant, PluginID: plugin, State: StatePendingConfig, Desired: DesiredEnabled,
			ConfigPublic: "{}", SecretKeys: "[]", CreatedAt: now, UpdatedAt: now}
		if cerr := e.Repo.Create(row); cerr != nil { // lost a concurrent create: use the winner
			if row, err = e.Repo.ByPlugin(tenant, plugin); err != nil {
				return nil, cerr
			}
		}
	} else if err != nil {
		return nil, err
	}
	if idemKey != "" && row.IdempotencyKey == idemKey && row.State != StatePendingConfig {
		return row, nil // replayed request
	}
	switch row.State {
	case StateInstalling, StateUninstalling:
		return row, nil // an operation is already running: the click is absorbed
	}
	if row.State == StateEnabled && len(public) == 0 && len(secret) == 0 {
		return row, nil // nothing to change: do not restart a healthy plugin
	}
	return e.apply(ctx, row, entry, public, secret, idemKey)
}

// UpdateConfig changes site values. Omitted secret fields stay as the Agent holds them.
func (e *Engine) UpdateConfig(ctx context.Context, tenant, id string, values map[string]string) (*Installation, error) {
	row, err := e.Repo.Get(tenant, id)
	if err != nil {
		return nil, err
	}
	if row.State == StateInstalling || row.State == StateUninstalling || row.State == StateUninstalled {
		return nil, ErrBadState
	}
	entry, err := e.entry(ctx, row.PluginID)
	if err != nil {
		return nil, err
	}
	public, secret, err := splitValues(entry, values)
	if err != nil {
		return nil, err
	}
	return e.apply(ctx, row, entry, public, secret, row.IdempotencyKey)
}

func (e *Engine) apply(ctx context.Context, row *Installation, entry *CatalogEntry, public, secret map[string]string, idem string) (*Installation, error) {
	for k, v := range secret {
		if err := e.Agent.PutSecret(ctx, row.PluginID, k, v); err != nil {
			return nil, mapAgentErr(err)
		}
	}
	merged := row.PublicConfig()
	for k, v := range public {
		merged[k] = v
	}
	keys := map[string]bool{}
	for _, k := range row.SecretKeyList() {
		keys[k] = true
	}
	for k := range secret {
		keys[k] = true
	}
	list := make([]string, 0, len(keys))
	for k := range keys {
		list = append(list, k)
	}
	sort.Strings(list)
	row.setPublic(merged)
	row.setSecretKeys(list)
	row.CatalogVersion = entry.Version
	row.Desired = DesiredEnabled
	row.IdempotencyKey = idem
	row.ErrorCode, row.OpStep, row.OpAttempts = "", "", 0
	if missingRequired(entry, merged, list) != "" {
		row.State, row.OpID = StatePendingConfig, ""
	} else {
		row.State, row.OpID = StateInstalling, uuid.NewString()
	}
	if err := e.Repo.Save(row); err != nil {
		return nil, err
	}
	e.trigger()
	return row, nil
}

func (e *Engine) Enable(ctx context.Context, tenant, id string) (*Installation, error) {
	return e.transition(tenant, id, func(r *Installation) bool {
		if r.State != StateInstalled && r.State != StateFailed {
			return false
		}
		r.Desired, r.State = DesiredEnabled, StateInstalling
		return true
	})
}

func (e *Engine) Disable(ctx context.Context, tenant, id string) (*Installation, error) {
	return e.transition(tenant, id, func(r *Installation) bool {
		if r.State != StateEnabled && r.State != StateFailed {
			return false
		}
		r.Desired, r.State = DesiredDisabled, StateInstalling
		return true
	})
}

// Retry resumes a failed operation from the beginning of its (idempotent) steps.
func (e *Engine) Retry(ctx context.Context, tenant, id string) (*Installation, error) {
	return e.transition(tenant, id, func(r *Installation) bool {
		if r.State != StateFailed {
			return false
		}
		if r.Desired == DesiredAbsent {
			r.State = StateUninstalling
		} else {
			r.State = StateInstalling
		}
		return true
	})
}

func (e *Engine) Uninstall(ctx context.Context, tenant, id string) (*Installation, error) {
	return e.transition(tenant, id, func(r *Installation) bool {
		switch r.State {
		case StateUninstalled, StateUninstalling:
			return false
		}
		r.Desired, r.State = DesiredAbsent, StateUninstalling
		return true
	})
}

func (e *Engine) transition(tenant, id string, change func(*Installation) bool) (*Installation, error) {
	row, err := e.Repo.Get(tenant, id)
	if err != nil {
		return nil, err
	}
	if !change(row) {
		return nil, ErrBadState
	}
	row.OpID, row.OpStep, row.OpAttempts, row.ErrorCode = uuid.NewString(), "", 0, ""
	if err := e.Repo.Save(row); err != nil {
		return nil, err
	}
	e.trigger()
	return row, nil
}

func (e *Engine) Get(tenant, id string) (*Installation, []Event, error) {
	row, err := e.Repo.Get(tenant, id)
	if err != nil {
		return nil, nil, err
	}
	ev, err := e.Repo.Events(id, 50)
	return row, ev, err
}

func (e *Engine) List(tenant string) ([]Installation, error) { return e.Repo.List(tenant) }

// ---- validation ----

var singleLine = regexp.MustCompile("^[^\r\n\x00]*$")

func splitValues(entry *CatalogEntry, values map[string]string) (public, secret map[string]string, err error) {
	public, secret = map[string]string{}, map[string]string{}
	for k, v := range values {
		spec, ok := entry.EnvUser[k]
		if !ok {
			return nil, nil, fmt.Errorf("%w: unknown field %q", ErrInvalidConfig, k)
		}
		if v == "" {
			continue // blank means "unchanged" for secrets and "not given" otherwise
		}
		if !singleLine.MatchString(v) {
			return nil, nil, fmt.Errorf("%w: %s must be a single line", ErrInvalidConfig, k)
		}
		if spec.Validate == "url" {
			u, perr := url.Parse(v)
			if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, nil, fmt.Errorf("%w: %s must be an http(s) URL", ErrInvalidConfig, k)
			}
		}
		if spec.Pattern != "" {
			if ok, _ := regexp.MatchString("^(?:"+spec.Pattern+")$", v); !ok {
				return nil, nil, fmt.Errorf("%w: %s has the wrong format", ErrInvalidConfig, k)
			}
		}
		if spec.Secret {
			secret[k] = v
		} else {
			public[k] = v
		}
	}
	return public, secret, nil
}

// missingRequired returns the first required field that has neither a value nor a default.
func missingRequired(entry *CatalogEntry, public map[string]string, secretKeys []string) string {
	have := map[string]bool{}
	for _, k := range secretKeys {
		have[k] = true
	}
	keys := make([]string, 0, len(entry.EnvUser))
	for k := range entry.EnvUser {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		spec := entry.EnvUser[k]
		if !spec.Required || spec.Default != "" {
			continue
		}
		if spec.Secret && !have[k] || !spec.Secret && strings.TrimSpace(public[k]) == "" {
			return k
		}
	}
	return ""
}

func mapAgentErr(err error) error {
	if errors.Is(err, ErrAgentOffline) {
		return ErrAgentDown
	}
	return err
}

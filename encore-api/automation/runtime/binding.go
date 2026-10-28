package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// A rule bound to a device template is one body plus a small row per scope.
// This file is where the two meet at run time.
//
// Two lookups are needed to turn "device X reported" into "which values does
// rule R use for device X":
//
//   1. which template does X belong to
//   2. which bindings exist for X and for that template
//
// Both are cached, because they change when an operator edits a configuration,
// not when a device reports. The cache is deliberately short-lived and
// explicitly invalidated on write: a stale template id would attach a device to
// the wrong rule, which is worse than one extra query.

const (
	// deviceConfigTTL is how long a device→template answer is reused.
	deviceConfigTTL = 5 * time.Minute
	// templateBindingTTL is how long "this tenant uses template bindings" is
	// reused. It only gates an optimisation, so a short TTL is enough.
	templateBindingTTL = 30 * time.Second
)

type deviceConfigEntry struct {
	configID string
	at       time.Time
}

type templateFlagEntry struct {
	has bool
	at  time.Time
}

// bindingCache holds the two lookups above. It is guarded because the worker
// and the HTTP handlers share one runtime.
type bindingCache struct {
	mu       sync.RWMutex
	devices  map[string]deviceConfigEntry
	tenants  map[string]templateFlagEntry
}

func (c *bindingCache) getDevice(key string, now time.Time) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.devices[key]
	if !ok || now.Sub(e.at) > deviceConfigTTL {
		return "", false
	}
	return e.configID, true
}

func (c *bindingCache) putDevice(key, configID string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.devices == nil {
		c.devices = map[string]deviceConfigEntry{}
	}
	c.devices[key] = deviceConfigEntry{configID: configID, at: now}
}

func (c *bindingCache) getTenant(key string, now time.Time) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.tenants[key]
	if !ok || now.Sub(e.at) > templateBindingTTL {
		return false, false
	}
	return e.has, true
}

func (c *bindingCache) putTenant(key string, has bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tenants == nil {
		c.tenants = map[string]templateFlagEntry{}
	}
	c.tenants[key] = templateFlagEntry{has: has, at: now}
}

// InvalidateBindings drops the cached answers for one tenant.
//
// It is called by every binding write. Without it, "I bound it and nothing
// happened for five minutes" would be the first bug report of this feature.
func (r *Runtime) InvalidateBindings(tenantID, deviceID string) {
	r.bindings.mu.Lock()
	defer r.bindings.mu.Unlock()
	delete(r.bindings.tenants, tenantID)
	if deviceID != "" {
		delete(r.bindings.devices, tenantID+"|"+deviceID)
		return
	}
	// A template-level change can affect any device of that tenant. Dropping the
	// whole tenant's device entries is cheap (it is a cache, not a table) and
	// avoids guessing which devices belong to the template.
	for k := range r.bindings.devices {
		if len(k) > len(tenantID) && k[:len(tenantID)+1] == tenantID+"|" {
			delete(r.bindings.devices, k)
		}
	}
}

// deviceConfigOf resolves the template a device belongs to.
//
// It returns "" when the answer is unknown, and the caller must treat that as
// "no template bindings apply" rather than "this device has no template": a
// directory that is down must not silently detach a device from its rules.
// Skipping is safe because a device-level binding still matches by id; only
// inherited rules are missed, and the next report will pick them up once the
// directory answers again.
func (r *Runtime) deviceConfigOf(ctx context.Context, tenantID, deviceID string) string {
	if r.dir == nil || deviceID == "" {
		return ""
	}
	now := r.Now()
	if v, ok := r.bindings.getDevice(tenantID+"|"+deviceID, now); ok {
		return v
	}
	devices, err := r.dir.ByIDs(ctx, tenantID, []string{deviceID})
	if err != nil || len(devices) == 0 {
		return ""
	}
	r.bindings.putDevice(tenantID+"|"+deviceID, devices[0].DeviceConfigID, now)
	return devices[0].DeviceConfigID
}

// usesTemplateBindings reports whether the tenant has any template-level
// binding. Tenants that only bind rules to individual devices skip the
// device→template lookup entirely, which keeps the uplink path unchanged for
// them.
func (r *Runtime) usesTemplateBindings(ctx context.Context, tenantID string) bool {
	now := r.Now()
	if v, ok := r.bindings.getTenant(tenantID, now); ok {
		return v
	}
	has, err := r.store.HasTemplateBindings(ctx, tenantID)
	if err != nil {
		return false
	}
	r.bindings.putTenant(tenantID, has, now)
	return has
}

// paramsForRule returns the parameter values a device has for one rule.
func paramsForRule(bound map[string]*bindingResolution, ruleID string) map[string]string {
	if bound == nil {
		return nil
	}
	if b, ok := bound[ruleID]; ok {
		return b.Values
	}
	return nil
}

// bindingResolution is what one device inherits for one rule: the merged
// values and the row that won them.
//
// The winning row is kept because the device page has to answer "is this
// inherited from the template or set on this device" — a question the merged
// values alone cannot answer.
type bindingResolution struct {
	Values  map[string]string
	Binding *store.Binding
}

// bindingsFor returns, per rule, the parameter values that apply to this
// device. Template rows are merged first, device rows second, so a device-level
// override wins without a second table.
func (r *Runtime) bindingsFor(ctx context.Context, tenantID, deviceID string) map[string]*bindingResolution {
	configID := ""
	if r.usesTemplateBindings(ctx, tenantID) {
		configID = r.deviceConfigOf(ctx, tenantID, deviceID)
	}
	bindings, err := r.store.BindingsForDevice(ctx, tenantID, deviceID, configID)
	if err != nil || len(bindings) == 0 {
		return nil
	}
	out := make(map[string]*bindingResolution, len(bindings))
	for _, b := range bindings {
		cur, ok := out[b.RuleID]
		if !ok {
			cur = &bindingResolution{Values: map[string]string{}}
			out[b.RuleID] = cur
		}
		for k, v := range b.Params {
			cur.Values[k] = v
		}
		cur.Binding = b
	}
	return out
}

// bindRule returns a copy of the rule with this device's parameter values filled
// in.
//
// The copy is per evaluation and never written back: the stored rule stays one
// body, so a template with 500 devices still has one definition to edit and one
// revision history. Only the run snapshot keeps the resolved values, and only
// because "what did it actually run with" has to stay answerable.
func bindRule(rule *store.Rule, res *bindingResolution) (*store.Rule, error) {
	if !rule.Definition.HasParams() {
		return rule, nil
	}
	var params map[string]string
	var binding *store.Binding
	if res != nil {
		params = res.Values
		binding = res.Binding
	}
	def, err := domain.Materialize(rule.Definition, params)
	if err != nil {
		return nil, err
	}
	out := *rule
	out.Definition = def
	out.BindingParams = params
	if binding != nil {
		out.BindingID = binding.ID
		out.BindingSource = binding.Source
	}
	// The hash describes the body that actually runs, not the template it came
	// from. Two devices with different thresholds therefore have different
	// hashes, which is what makes "which body produced this run" checkable.
	out.DefinitionHash = domain.DefinitionHash(def)
	return &out, nil
}

// ruleForRun loads the pinned revision and applies the parameter values frozen
// into the run.
func (r *Runtime) ruleForRun(ctx context.Context, ruleID string, revision int, params map[string]string) (*store.Rule, error) {
	return r.ruleForRunWithSource(ctx, ruleID, revision, params, "")
}

// ruleForRunWithSource also restores which kind of binding supplied the values,
// so a resumed run reports the same provenance as the run that opened it.
func (r *Runtime) ruleForRunWithSource(ctx context.Context, ruleID string, revision int,
	params map[string]string, source string) (*store.Rule, error) {

	rule, err := r.store.GetRuleRevision(ctx, ruleID, revision)
	if err != nil {
		return nil, err
	}
	var res *bindingResolution
	if len(params) > 0 || source != "" {
		res = &bindingResolution{Values: params, Binding: &store.Binding{Source: source}}
	}
	return bindRule(rule, res)
}

// runBindingParams reads the frozen parameter values off a run snapshot.
//
// A run that predates bindings simply has none, which is the same shape as a
// rule with no parameters.
func runBindingParams(run *store.Run) map[string]string {
	if len(run.Snapshot) == 0 {
		return nil
	}
	var s struct {
		BindingParams map[string]string `json:"binding_params"`
	}
	if err := json.Unmarshal(run.Snapshot, &s); err != nil {
		return nil
	}
	return s.BindingParams
}

// bindingSnapshot is the part of the run snapshot that records which binding
// produced this run.
func bindingSnapshot(b *store.Binding) map[string]any {
	if b == nil {
		return nil
	}
	return map[string]any{
		"binding_id":       b.ID,
		"binding_source":   b.Source,
		"binding_scope":    bindingScope(b),
		"binding_revision": b.Revision,
	}
}

func bindingScope(b *store.Binding) string {
	if b.DeviceConfigID != "" {
		return "template:" + b.DeviceConfigID
	}
	return "device:" + b.DeviceID
}

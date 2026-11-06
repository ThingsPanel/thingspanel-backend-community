package plugininstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const secretValue = "SECRET-TOKEN-VALUE-123"

type fakeAgent struct {
	mu        sync.Mutex
	calls     []string
	secrets   map[string]string
	offline   bool
	failStep  string // return ErrAgentRejected on this call name
	conflict  string // return ErrAgentConflict on this call name
	prepares  int
	lastToken string
}

func newFakeAgent() *fakeAgent { return &fakeAgent{secrets: map[string]string{}} }

func (a *fakeAgent) rec(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.offline {
		return ErrAgentOffline
	}
	a.calls = append(a.calls, name)
	if a.failStep == name {
		return ErrAgentRejected
	}
	if a.conflict == name {
		return ErrAgentConflict
	}
	return nil
}

func (a *fakeAgent) Catalog(context.Context) ([]CatalogEntry, error) {
	if err := a.rec("catalog"); err != nil {
		return nil, err
	}
	return []CatalogEntry{{ID: "homeassistant", DisplayName: "Home Assistant", Version: "1.0.0", ServiceIdentifier: "homeassistant",
		NetworkAlias: "tp-plugin-homeassistant", HTTPPort: 9001, Health: &HealthSpec{Port: 9001, Path: "/health"},
		ServicePlugin: &ServicePluginSpec{Name: "Home Assistant", ServiceType: 2, Config: map[string]string{"http_address": "{alias}:{http_port}"}},
		EnvUser: map[string]FieldSpec{
			"HA_BASE_URL":     {Required: true, Validate: "url"},
			"HA_ACCESS_TOKEN": {Required: true, Secret: true},
		}}}, nil
}
func (a *fakeAgent) PutSecret(_ context.Context, instance, key, value string) error {
	if err := a.rec("put_secret"); err != nil {
		return err
	}
	a.mu.Lock()
	a.secrets[instance+"/"+key] = value
	a.mu.Unlock()
	return nil
}
func (a *fakeAgent) SecretKeys(_ context.Context, instance string) ([]string, error) {
	if err := a.rec("secret_keys"); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for k := range a.secrets {
		if strings.HasPrefix(k, instance+"/") {
			out = append(out, strings.TrimPrefix(k, instance+"/"))
		}
	}
	return out, nil
}
func (a *fakeAgent) ClearConfig(_ context.Context, instance string) error {
	if err := a.rec("clear_config"); err != nil {
		return err
	}
	a.mu.Lock()
	for k := range a.secrets {
		if strings.HasPrefix(k, instance+"/") {
			delete(a.secrets, k)
		}
	}
	a.mu.Unlock()
	return nil
}
func (a *fakeAgent) AcceptGeneration(_ context.Context, _ string, g uint64) error {
	return a.rec(fmt.Sprintf("accept-%d", g))
}
func (a *fakeAgent) Prepare(_ context.Context, r PrepareRequest) error {
	if err := a.rec(fmt.Sprintf("prepare-%d", r.Generation)); err != nil {
		return err
	}
	a.mu.Lock()
	a.prepares++
	a.lastToken = r.IdentityToken
	a.mu.Unlock()
	return nil
}
func (a *fakeAgent) Grant(_ context.Context, _ string, g uint64, _, id string, _ time.Duration) error {
	return a.rec(fmt.Sprintf("grant-%d", g))
}
func (a *fakeAgent) Renew(_ context.Context, _ string, _ uint64, _, _ string, _ time.Duration) error {
	return a.rec("renew")
}
func (a *fakeAgent) Activate(_ context.Context, _ string, g uint64, _ string) error {
	return a.rec(fmt.Sprintf("activate-%d", g))
}
func (a *fakeAgent) Revoke(_ context.Context, _ string, g uint64, _ string) error {
	return a.rec(fmt.Sprintf("revoke-%d", g))
}
func (a *fakeAgent) Retire(_ context.Context, _ string, g uint64) error {
	return a.rec(fmt.Sprintf("retire-%d", g))
}
func (a *fakeAgent) Status(context.Context) error { return a.rec("status") }

func (a *fakeAgent) count(prefix string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type fakeIDs struct {
	mu      sync.Mutex
	maxGen  int64
	issued  []int64
	revoked int
	live    bool
	renewed int
}

func (f *fakeIDs) Issue(_, _, _ string, g int64, _ time.Duration, _ time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g <= f.maxGen {
		return "", ErrGenerationNotHigher
	}
	f.maxGen, f.live = g, true
	f.issued = append(f.issued, g)
	return fmt.Sprintf("pri_x.%d", g), nil
}
func (f *fakeIDs) Renew(_, _ string, _ time.Duration, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.live {
		return ErrIdentityNotLive
	}
	f.renewed++
	return nil
}
func (f *fakeIDs) Revoke(_, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked++
	f.live = false
	return nil
}

type fakeServices struct{ n int }

func (s *fakeServices) Ensure(context.Context, CatalogEntry) (string, string, error) {
	s.n++
	return "sp", "updated", nil
}

type fakeHealth struct{ err error }

func (h *fakeHealth) Check(context.Context, CatalogEntry) error { return h.err }

type env struct {
	e      *Engine
	agent  *fakeAgent
	ids    *fakeIDs
	svc    *fakeServices
	health *fakeHealth
	db     *gorm.DB
	clock  *time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&Installation{}, &Event{}); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	v := &env{agent: newFakeAgent(), ids: &fakeIDs{}, svc: &fakeServices{}, health: &fakeHealth{}, db: db, clock: &clock}
	tm := DefaultTiming()
	tm.HealthWait, tm.HealthInterval, tm.MaxAttempts = 0, time.Millisecond, 3
	v.e = &Engine{Repo: NewRepo(func() *gorm.DB { return db }), Agent: v.agent, IDs: v.ids, Services: v.svc, Health: v.health,
		Now: func() time.Time { return *v.clock }, Owner: "t", Timing: tm}
	return v
}

func (v *env) advance(d time.Duration) { *v.clock = v.clock.Add(d) }

func (v *env) settle(t *testing.T, tenant, id, want string) *Installation {
	t.Helper()
	for i := 0; i < 12; i++ {
		v.e.Tick(context.Background())
		row, _, err := v.e.Get(tenant, id)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == want {
			return row
		}
		v.advance(10 * time.Minute) // past any backoff lease
	}
	row, _, _ := v.e.Get(tenant, id)
	t.Fatalf("state %s (step %s, code %s), want %s", row.State, row.OpStep, row.ErrorCode, want)
	return nil
}

func fullConfig() map[string]string {
	return map[string]string{"HA_BASE_URL": "http://ha.local:8123", "HA_ACCESS_TOKEN": secretValue}
}

func TestInstallHappyPathKeepsSecretOutOfTheDatabase(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, err := v.e.Submit(ctx, "tn1", "homeassistant", "k1", fullConfig())
	if err != nil || row.State != StateInstalling {
		t.Fatalf("submit: %v %+v", err, row)
	}
	row = v.settle(t, "tn1", row.ID, StateEnabled)
	if row.Generation != 1 || row.Health != "ok" || v.svc.n != 1 {
		t.Fatalf("row %+v services %d", row, v.svc.n)
	}
	if v.agent.secrets["homeassistant/HA_ACCESS_TOKEN"] != secretValue {
		t.Fatal("secret did not reach the agent store")
	}
	if v.agent.lastToken != "pri_x.1" {
		t.Fatalf("identity token was not handed to prepare: %q", v.agent.lastToken)
	}
	var dump []string
	var rows []Installation
	v.db.Find(&rows)
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		dump = append(dump, string(raw), r.ConfigPublic, r.SecretKeys)
	}
	var events []Event
	v.db.Find(&events)
	raw, _ := json.Marshal(events)
	dump = append(dump, string(raw))
	for _, d := range dump {
		if strings.Contains(d, secretValue) || strings.Contains(d, "pri_x") {
			t.Fatalf("secret leaked into stored data: %s", d)
		}
	}
	if got := row.SecretKeyList(); len(got) != 1 || got[0] != "HA_ACCESS_TOKEN" {
		t.Fatalf("secret keys %v", got)
	}
	if row.PublicConfig()["HA_BASE_URL"] != "http://ha.local:8123" || row.PublicConfig()["HA_ACCESS_TOKEN"] != "" {
		t.Fatalf("public config %v", row.PublicConfig())
	}
}

func TestRepeatedAndConcurrentSubmitCreateOneInstance(t *testing.T) {
	v := newEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = v.e.Submit(context.Background(), "tn1", "homeassistant", fmt.Sprintf("k%d", i), fullConfig())
		}(i)
	}
	wg.Wait()
	var n int64
	v.db.Model(&Installation{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d rows", n)
	}
	row, _ := v.e.Repo.ByPlugin("tn1", "homeassistant")
	v.settle(t, "tn1", row.ID, StateEnabled)
	// more clicks after it is up change nothing, also with the same address (no secret re-typed)
	again, err := v.e.Submit(context.Background(), "tn1", "homeassistant", "k-new", map[string]string{"HA_BASE_URL": "http://ha.local:8123"})
	if err != nil || again.State != StateEnabled {
		t.Fatalf("%v %v", err, again.State)
	}
	v.e.Tick(context.Background())
	if v.agent.prepares != 1 || len(v.ids.issued) != 1 {
		t.Fatalf("prepares %d, identities %v", v.agent.prepares, v.ids.issued)
	}
}

func TestPartialConfigWaitsThenCompletes(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, err := v.e.Submit(ctx, "tn1", "homeassistant", "", map[string]string{"HA_BASE_URL": "http://ha:8123"})
	if err != nil || row.State != StatePendingConfig {
		t.Fatalf("%v %v", err, row)
	}
	v.e.Tick(ctx)
	if v.agent.prepares != 0 {
		t.Fatal("nothing may run while configuration is missing")
	}
	if _, err := v.e.UpdateConfig(ctx, "tn1", row.ID, map[string]string{"HA_ACCESS_TOKEN": secretValue}); err != nil {
		t.Fatal(err)
	}
	v.settle(t, "tn1", row.ID, StateEnabled)
}

func TestInvalidValuesAreRejected(t *testing.T) {
	v := newEnv(t)
	for _, bad := range []map[string]string{
		{"HA_BASE_URL": "ftp://x"}, {"HA_BASE_URL": "http://ok\nhttp://evil"}, {"NOPE": "x"}, {"HA_BASE_URL": "not a url"},
	} {
		if _, err := v.e.Submit(context.Background(), "tn1", "homeassistant", "", bad); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%v accepted: %v", bad, err)
		}
	}
	if _, err := v.e.Submit(context.Background(), "tn1", "nope", "", nil); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("%v", err)
	}
}

func TestAgentOfflineWaitsThenResumesAndEventuallyFails(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, _ := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	v.agent.offline = true
	v.e.Tick(ctx)
	got, _, _ := v.e.Get("tn1", row.ID)
	if got.State != StateInstalling || got.ErrorCode != CodeAgentOffline {
		t.Fatalf("%+v", got)
	}
	v.agent.offline = false
	v.settle(t, "tn1", row.ID, StateEnabled)

	// a plugin that never comes back ends up failed, retry works once the agent is back
	w := newEnv(t)
	r2, _ := w.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	w.agent.offline = true
	for i := 0; i < 6; i++ {
		w.e.Tick(ctx)
		w.advance(10 * time.Minute)
	}
	f, _, _ := w.e.Get("tn1", r2.ID)
	if f.State != StateFailed || f.ErrorCode != CodeAgentOffline {
		t.Fatalf("%+v", f)
	}
	w.agent.offline = false
	if _, err := w.e.Retry(ctx, "tn1", r2.ID); err != nil {
		t.Fatal(err)
	}
	w.settle(t, "tn1", r2.ID, StateEnabled)
}

func TestFailedStepRevokesIdentityAndRetryUsesNewGeneration(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	v.agent.failStep = "grant-1"
	row, _ := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	f := v.settle(t, "tn1", row.ID, StateFailed)
	if f.OpStep != "agent_grant" || f.ErrorCode != CodeAgentRejected || v.ids.live {
		t.Fatalf("%+v live=%v", f, v.ids.live)
	}
	v.agent.failStep = ""
	if _, err := v.e.Retry(ctx, "tn1", row.ID); err != nil {
		t.Fatal(err)
	}
	e := v.settle(t, "tn1", row.ID, StateEnabled)
	if e.Generation != 2 {
		t.Fatalf("generation %d", e.Generation)
	}
}

func TestHealthFailureLeavesFailedNotEnabled(t *testing.T) {
	v := newEnv(t)
	v.health.err = errors.New("down")
	row, _ := v.e.Submit(context.Background(), "tn1", "homeassistant", "", fullConfig())
	f := v.settle(t, "tn1", row.ID, StateFailed)
	if f.OpStep != "health" || f.ErrorCode != CodeHealthFailed {
		t.Fatalf("%+v", f)
	}
}

func TestCrashedPassIsResumedAfterTheLeaseExpires(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, _ := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	// another process claims the row and dies without releasing it
	if ok, _ := v.e.Repo.Claim(row.ID, "dead-process", v.e.now(), v.e.now().Add(5*time.Minute)); !ok {
		t.Fatal("claim")
	}
	v.e.Tick(ctx)
	if v.agent.prepares != 0 {
		t.Fatal("a live lease must block a second worker")
	}
	v.advance(6 * time.Minute)
	v.e.Tick(ctx)
	got, _, _ := v.e.Get("tn1", row.ID)
	if got.State != StateEnabled {
		t.Fatalf("%+v", got)
	}
}

func TestDisableEnableUninstallReinstall(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, _ := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	row = v.settle(t, "tn1", row.ID, StateEnabled)

	if _, err := v.e.Disable(ctx, "tn1", row.ID); err != nil {
		t.Fatal(err)
	}
	d := v.settle(t, "tn1", row.ID, StateInstalled)
	if v.ids.live || d.Health != "" || v.agent.count("revoke-1") != 1 {
		t.Fatalf("live=%v %+v", v.ids.live, d)
	}
	if _, err := v.e.Enable(ctx, "tn1", row.ID); err != nil {
		t.Fatal(err)
	}
	if e := v.settle(t, "tn1", row.ID, StateEnabled); e.Generation != 2 {
		t.Fatalf("generation %d", e.Generation)
	}

	if _, err := v.e.Uninstall(ctx, "tn1", row.ID); err != nil {
		t.Fatal(err)
	}
	u := v.settle(t, "tn1", row.ID, StateUninstalled)
	if v.ids.live || len(v.agent.secrets) != 0 || u.Generation != 2 || len(u.SecretKeyList()) != 0 {
		t.Fatalf("live=%v secrets=%v %+v", v.ids.live, v.agent.secrets, u)
	}
	// a second uninstall is refused, not repeated
	if _, err := v.e.Uninstall(ctx, "tn1", row.ID); !errors.Is(err, ErrBadState) {
		t.Fatalf("%v", err)
	}
	// reinstall: the same row, higher generation, user types the secret again
	if _, err := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig()); err != nil {
		t.Fatal(err)
	}
	if r := v.settle(t, "tn1", row.ID, StateEnabled); r.Generation != 3 {
		t.Fatalf("generation %d", r.Generation)
	}
	var n int64
	v.db.Model(&Installation{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d rows", n)
	}
}

func TestMaintainRenewsAndDoesNotReinstallWhenRenewalRefused(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, _ := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	row = v.settle(t, "tn1", row.ID, StateEnabled)
	prepared := v.agent.prepares
	generation := row.Generation
	v.advance(6 * time.Minute)
	v.e.Tick(ctx)
	if v.agent.count("renew") != 1 || v.ids.renewed != 1 {
		t.Fatalf("renew agent=%d identity=%d", v.agent.count("renew"), v.ids.renewed)
	}
	v.advance(6 * time.Minute)
	v.agent.conflict = "renew"
	v.e.Tick(ctx)
	got, _, _ := v.e.Get("tn1", row.ID)
	if got.State != StateFailed || got.Generation != generation || v.agent.prepares != prepared {
		t.Fatalf("%+v", got)
	}
	if v.ids.live {
		t.Fatal("identity must be revoked after a refused renewal")
	}
}

func TestMaintainKeepsLiveGenerationWhenCatalogUnavailable(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, err := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	if err != nil {
		t.Fatal(err)
	}
	row = v.settle(t, "tn1", row.ID, StateEnabled)
	gen, prepared := row.Generation, v.agent.prepares
	// The Agent can still renew the live instance while its signed catalog is
	// unavailable (for example because it expired).
	v.agent.failStep = "catalog"
	for i := 0; i < v.e.Timing.MaxMaintainAttempts+2; i++ {
		v.advance(6 * time.Minute)
		v.e.Tick(ctx)
	}
	got, _, err := v.e.Get("tn1", row.ID)
	if err != nil || got.State != StateEnabled || got.Generation != gen || got.Health != "degraded" || got.ErrorCode != CodeCatalogUnavailable || got.OpAttempts != 0 {
		t.Fatalf("catalog unavailable: row=%+v err=%v", got, err)
	}
	if !v.ids.live || v.ids.revoked != 0 || v.agent.prepares != prepared || v.agent.count("renew") != v.e.Timing.MaxMaintainAttempts+2 {
		t.Fatal("catalog failure interrupted or replaced the live generation")
	}
	v.agent.failStep = ""
	v.advance(6 * time.Minute)
	v.e.Tick(ctx)
	got, _, err = v.e.Get("tn1", row.ID)
	if err != nil || got.State != StateEnabled || got.Generation != gen || got.Health != "ok" || got.ErrorCode != "" {
		t.Fatalf("catalog recovered: row=%+v err=%v", got, err)
	}
}

func TestMaintainRetriesTransientFailureWithBoundedBackoffWithoutChangingGeneration(t *testing.T) {
	v := newEnv(t)
	ctx := context.Background()
	row, _ := v.e.Submit(ctx, "tn1", "homeassistant", "", fullConfig())
	row = v.settle(t, "tn1", row.ID, StateEnabled)
	generation, prepared := row.Generation, v.agent.prepares
	v.agent.offline = true
	v.advance(6 * time.Minute)
	v.e.Tick(ctx)
	got, _, _ := v.e.Get("tn1", row.ID)
	if got.State != StateEnabled || got.Generation != generation || got.OpAttempts != 1 || v.agent.prepares != prepared {
		t.Fatalf("first retry changed install state: %+v", got)
	}
	for attempt := 1; attempt < v.e.Timing.MaxMaintainAttempts; attempt++ {
		v.advance(*backoff(attempt - 1) + time.Second)
		v.e.Tick(ctx)
	}
	got, _, _ = v.e.Get("tn1", row.ID)
	if got.State != StateFailed || got.Generation != generation || got.OpAttempts != v.e.Timing.MaxMaintainAttempts || v.agent.prepares != prepared {
		t.Fatalf("bounded retries did not stop in place: %+v", got)
	}
	if v.ids.live {
		t.Fatal("exhausted renewal attempts must revoke the platform identity")
	}
}

func TestTenantsAreIsolated(t *testing.T) {
	v := newEnv(t)
	row, _ := v.e.Submit(context.Background(), "tn1", "homeassistant", "", fullConfig())
	if _, _, err := v.e.Get("tn2", row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := v.e.Uninstall(context.Background(), "tn2", row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestGormServicesRegistersOnceAndKeepsOtherKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:svc?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ServicePlugin{}); err != nil {
		t.Fatal(err)
	}
	s := NewGormServices(func() *gorm.DB { return db })
	entry := (&fakeAgent{}).mustEntry()
	id1, o1, err := s.Ensure(context.Background(), entry)
	if err != nil || o1 != "created" {
		t.Fatalf("%v %v", err, o1)
	}
	db.Model(&model.ServicePlugin{}).Where("id = ?", id1).Update("service_config", `{"http_address":"old:1","extra":"keep"}`)
	id2, o2, err := s.Ensure(context.Background(), entry)
	if err != nil || o2 != "updated" || id2 != id1 {
		t.Fatalf("%v %v %v", err, o2, id2)
	}
	var rows []model.ServicePlugin
	db.Find(&rows)
	if len(rows) != 1 || !strings.Contains(*rows[0].ServiceConfig, "tp-plugin-homeassistant:9001") || !strings.Contains(*rows[0].ServiceConfig, "keep") {
		t.Fatalf("%+v", rows)
	}
}

func (a *fakeAgent) mustEntry() CatalogEntry { l, _ := a.Catalog(context.Background()); return l[0] }

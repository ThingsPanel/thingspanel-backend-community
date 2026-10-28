package automationcompat

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// A stand-in for the automation service.
//
// It is a fake on purpose: these tests are about the adapter's contract with the
// old clients, and the real service is covered by its own suite. The one thing
// the fake must not do is be permissive — every request is recorded, so a test
// can assert that a refused call never reached the service at all. "It returned
// an error" and "nothing happened" are different claims, and only the second one
// matters when the question is whether a valve moved.
// ---------------------------------------------------------------------------

type recordedRun struct {
	ruleID    string
	deviceID  string
	requestID string
	token     string
}

type recordedEnable struct {
	ruleID  string
	enabled bool
	version int
}

type fakeCore struct {
	mu sync.Mutex

	refs       map[string]map[string]any
	refStatus  map[string]int
	rules      map[string]map[string]any
	ruleList   []map[string]any
	ruleTotal  int
	runs       map[string]map[string]any
	alarms     []map[string]any
	alarmQuery []url.Values
	listQuery  []url.Values

	runCalls    []recordedRun
	enableCalls []recordedEnable
	tokens      []string
	requests    []string
	runStatus   string
	down        bool
}

func newFakeCore() *fakeCore {
	return &fakeCore{
		refs:      map[string]map[string]any{},
		refStatus: map[string]int{},
		rules:     map[string]map[string]any{},
		runs:      map[string]map[string]any{},
	}
}

func (f *fakeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	if tok := r.Header.Get("x-token"); tok != "" {
		f.tokens = append(f.tokens, tok)
	}
	if f.down {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":"unavailable","message":"boom"}`))
		return
	}

	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/v1/legacy-refs/"):
		id := strings.TrimPrefix(path, "/v1/legacy-refs/")
		if status, ok := f.refStatus[id]; ok {
			writeCoreError(w, status, "legacy_ref_missing", "这个旧 id 还没有迁移到新规则")
			return
		}
		body, ok := f.refs[id]
		if !ok {
			writeCoreError(w, http.StatusNotFound, "legacy_ref_missing", "没有这个旧 id")
			return
		}
		writeJSON(w, body)

	case path == "/v1/rules" && r.Method == http.MethodGet:
		f.listQuery = append(f.listQuery, r.URL.Query())
		writeJSON(w, map[string]any{"rules": f.ruleList, "total": f.ruleTotal})

	case strings.HasSuffix(path, "/run") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/rules/"), "/run")
		var body struct {
			DeviceID  string `json:"device_id"`
			RequestID string `json:"request_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.runCalls = append(f.runCalls, recordedRun{
			ruleID: id, deviceID: body.DeviceID, requestID: body.RequestID,
			token: r.Header.Get("x-token"),
		})
		status := "succeeded"
		if f.runStatus != "" {
			status = f.runStatus
		}
		writeJSON(w, map[string]any{
			"id": "run-1", "rule_id": id, "status": status,
			"reason_code": "device_not_in_directory", "reason": "目标设备不在目录里",
		})

	case strings.HasSuffix(path, "/enabled") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/rules/"), "/enabled")
		var body struct {
			Enabled bool `json:"enabled"`
			Version int  `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.enableCalls = append(f.enableCalls, recordedEnable{ruleID: id, enabled: body.Enabled, version: body.Version})
		out := map[string]any{}
		for k, v := range f.rules[id] {
			out[k] = v
		}
		out["enabled"] = body.Enabled
		out["version"] = body.Version + 1
		writeJSON(w, out)

	case strings.HasSuffix(path, "/runs") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/rules/"), "/runs")
		page, ok := f.runs[id]
		if !ok {
			page = map[string]any{"runs": []any{}, "total": 0}
		}
		writeJSON(w, page)

	case path == "/v1/alarms" && r.Method == http.MethodGet:
		f.alarmQuery = append(f.alarmQuery, r.URL.Query())
		writeJSON(w, map[string]any{"alarms": f.alarms})

	case strings.HasPrefix(path, "/v1/alarms/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "/v1/alarms/")
		for _, a := range f.alarms {
			if a["id"] == id {
				writeJSON(w, a)
				return
			}
		}
		writeCoreError(w, http.StatusNotFound, "not_found", "没有这个告警")

	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/rules/"):
		id := strings.TrimPrefix(path, "/v1/rules/")
		body, ok := f.rules[id]
		if !ok {
			writeCoreError(w, http.StatusNotFound, "not_found", "没有这条规则")
			return
		}
		writeJSON(w, body)

	default:
		writeCoreError(w, http.StatusNotFound, "not_found", "fake core: "+path)
	}
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeCoreError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    http.StatusText(status),
		"message": msg,
		"details": map[string]any{"code": code},
	})
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type harness struct {
	t     *testing.T
	core  *fakeCore
	h     *Handlers
	srv   *httptest.Server
	clock *time.Time
}

func newHarness(t *testing.T, sunset time.Time) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	core := newFakeCore()
	coreSrv := httptest.NewServer(core)
	t.Cleanup(coreSrv.Close)

	// A pointer, so a test can walk the clock past the sunset without rebuilding
	// the handler (and without sleeping).
	clock := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	clockPtr := &clock
	h := NewHandlers(Deps{
		Core:   NewCoreClient(coreSrv.URL, time.Second),
		Sunset: sunset,
		Now:    func() time.Time { return *clockPtr },
	})
	r := gin.New()
	// Stands in for the console's JWT middleware: the real one puts the claims in
	// the context before the handler runs.
	r.Use(func(c *gin.Context) {
		if tenant := c.GetHeader("X-Test-Tenant"); tenant != "" {
			c.Set("claims", &utils.UserClaims{ID: "user-1", TenantID: tenant})
		}
		c.Next()
	})
	MountShadow(r.Group("/api/v1"), h)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &harness{t: t, core: core, h: h, srv: srv, clock: clockPtr}
}

// now is the adapter's clock.
func (hs *harness) now() time.Time { return *hs.clock }

// travel moves the adapter's clock.
func (hs *harness) travel(to time.Time) { *hs.clock = to }

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (hs *harness) do(method, path string, body any, headers map[string]string) (*apiEnvelope, *http.Response) {
	hs.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, hs.srv.URL+path, reader)
	if err != nil {
		hs.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("x-token", "token-of-tenant-a")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		hs.t.Fatalf("call %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var env apiEnvelope
	raw, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(raw, &env); err != nil {
		hs.t.Fatalf("body is not the old envelope: %s", raw)
	}
	return &env, res
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

// appHeaders is what the App actually sends: x-token plus the uni-app runtime's
// user agent.
func appHeaders() map[string]string {
	return map[string]string{"User-Agent": "Mozilla/5.0 (iPhone) uni-app HBuilderX", "X-Test-Tenant": "tenant-a"}
}

func webHeaders() map[string]string {
	return map[string]string{
		"User-Agent":    "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 Chrome/129.0 Safari/537.36",
		"X-Test-Tenant": "tenant-a",
	}
}

// ---------------------------------------------------------------------------
// Scenes
// ---------------------------------------------------------------------------

// TestTheAppDashboardGetsTheRealTotal is the first thing A30 found.
//
// The App asks for one row just to print a badge (page_size=1). When the list
// endpoint reported total = len(page), that badge read "1" for every tenant.
func TestTheAppDashboardGetsTheRealTotal(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-scene"] = map[string]any{
		"legacy_id": "legacy-scene", "rule_id": "rule-1", "kind": "scene", "enabled": true,
	}
	hs.core.ruleList = []map[string]any{{
		"id": "rule-1", "name": "门厅灯光", "kind": "scene", "enabled": true,
		"legacy_id": "legacy-scene", "created_at": hs.now(), "updated_at": hs.now(),
	}}
	hs.core.ruleTotal = 7

	env, _ := hs.do(http.MethodGet, "/api/v1/compat/scene?page=1&page_size=1", nil, appHeaders())
	if env.Code != 200 {
		t.Fatalf("the App only checks code==200, got %d (%s)", env.Code, env.Message)
	}
	data := decode[struct {
		Total int              `json:"total"`
		List  []map[string]any `json:"list"`
	}](t, env.Data)
	if data.Total != 7 {
		t.Fatalf("total must be the number of matches, not the page size; got %d", data.Total)
	}
	if len(data.List) != 1 {
		t.Fatalf("page_size=1 must return one row, got %d", len(data.List))
	}
	if data.List[0]["id"] != "legacy-scene" {
		t.Fatalf("the row must be addressable by the old id, got %v", data.List[0]["id"])
	}
}

func TestTheAppActivationRunsTheRuleExactlyOnce(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-scene"] = map[string]any{
		"legacy_id": "legacy-scene", "rule_id": "rule-1", "kind": "scene",
	}

	env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene/active/legacy-scene", nil, appHeaders())
	if env.Code != 200 {
		t.Fatalf("the App shows a success toast on code 200, got %d (%s)", env.Code, env.Message)
	}
	if len(hs.core.runCalls) != 1 {
		t.Fatalf("one press is one run, got %d", len(hs.core.runCalls))
	}
	call := hs.core.runCalls[0]
	if call.ruleID != "rule-1" {
		t.Fatalf("the run must target the resolved rule, got %q", call.ruleID)
	}
	if call.requestID == "" {
		t.Fatal("the adapter must send a request id, otherwise a retried press runs twice")
	}
	if call.token != "token-of-tenant-a" {
		t.Fatalf("the caller's own token must be forwarded, got %q", call.token)
	}
}

func TestTheAdapterNeverInventsATenant(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-scene"] = map[string]any{"rule_id": "rule-1"}

	// No claims in the context at all: the shadow mount is reachable without the
	// console's JWT middleware. The adapter must still send the caller's token
	// and nothing else, so the new service decides the tenant.
	_, _ = hs.do(http.MethodPost, "/api/v1/compat/scene/active/legacy-scene", nil, map[string]string{
		"User-Agent": "uni-app",
	})
	if len(hs.core.tokens) == 0 {
		t.Fatal("no token reached the core")
	}
	for _, tok := range hs.core.tokens {
		if tok != "token-of-tenant-a" {
			t.Fatalf("the adapter sent a token the caller did not have: %q", tok)
		}
	}
}

func TestAnAmbiguousLegacyIdIsRefusedAndNothingRuns(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["dup"] = map[string]any{
		"legacy_id": "dup", "rule_id": "rule-1", "ambiguous": true,
		"candidates": []string{"rule-1", "rule-2"},
	}

	env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene/active/dup", nil, appHeaders())
	if env.Code != codeAmbiguous {
		t.Fatalf("an old id that maps to two rules must be refused with %d, got %d", codeAmbiguous, env.Code)
	}
	if len(hs.core.runCalls) != 0 {
		t.Fatalf("a refused activation must not run anything, got %d runs", len(hs.core.runCalls))
	}
}

func TestAnUnmigratedObjectSaysSoInsteadOfPretending(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	// No ref registered: this object has not been migrated yet.

	env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene/active/never-migrated", nil, appHeaders())
	if env.Code != 100404 {
		t.Fatalf("an unmigrated id is a 404 in the old envelope, got %d", env.Code)
	}
	data := decode[struct {
		ReasonCode string `json:"reason_code"`
	}](t, env.Data)
	if data.ReasonCode != "legacy_ref_missing" {
		t.Fatalf("the reason must be machine-readable, got %q", data.ReasonCode)
	}
	if len(hs.core.runCalls) != 0 {
		t.Fatal("nothing may run for an object we cannot resolve")
	}
}

func TestSceneDetailCarriesTheWarningsToTheClient(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-scene"] = map[string]any{"rule_id": "rule-1"}
	hs.core.rules["rule-1"] = map[string]any{
		"id": "rule-1", "name": "含等待的场景", "kind": "scene", "enabled": true,
		"definition": map[string]any{"steps": []map[string]any{
			{"kind": "action", "action": "telemetry", "identifier": "switch", "value": "1"},
			{"kind": "wait", "wait": "5s"},
		}},
		"created_at": hs.now(), "updated_at": hs.now(),
	}

	env, _ := hs.do(http.MethodGet, "/api/v1/compat/scene/detail/legacy-scene", nil, appHeaders())
	if env.Code != 200 {
		t.Fatalf("detail failed: %d %s", env.Code, env.Message)
	}
	data := decode[struct {
		Actions  []map[string]any `json:"actions"`
		Warnings []string         `json:"compat_warnings"`
	}](t, env.Data)
	if len(data.Actions) != 1 {
		t.Fatalf("only the representable step is listed, got %d", len(data.Actions))
	}
	if len(data.Warnings) != 1 {
		t.Fatalf("the client must be told the list is partial, got %v", data.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Scene automations
// ---------------------------------------------------------------------------

// TestTheSwitchReadsThenWritesTheVersion: the old endpoint toggled from whatever
// the row said at that moment and returned nothing. Two phones with a stale list
// would both compute the same target and the second press would be a no-op the
// user reads as "the switch does not work".
func TestTheSwitchReadsThenWritesTheVersion(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-auto"] = map[string]any{"rule_id": "rule-9"}
	hs.core.rules["rule-9"] = map[string]any{
		"id": "rule-9", "name": "有人经过", "kind": "automation", "enabled": true, "version": 4,
	}

	env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene_automations/switch/legacy-auto", nil, appHeaders())
	if env.Code != 200 {
		t.Fatalf("switch failed: %d %s", env.Code, env.Message)
	}
	if len(hs.core.enableCalls) != 1 {
		t.Fatalf("one press is one write, got %d", len(hs.core.enableCalls))
	}
	call := hs.core.enableCalls[0]
	if call.enabled {
		t.Fatal("the rule was enabled, so the toggle must turn it off")
	}
	if call.version != 4 {
		t.Fatalf("the write must carry the version it read (4), got %d", call.version)
	}
	data := decode[struct {
		Enabled string `json:"enabled"`
	}](t, env.Data)
	if data.Enabled != "N" {
		t.Fatalf("the old switch is Y/N, got %q", data.Enabled)
	}
}

func TestAnExplicitSwitchTargetWins(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-auto"] = map[string]any{"rule_id": "rule-9"}
	hs.core.rules["rule-9"] = map[string]any{"id": "rule-9", "enabled": false, "version": 1}

	env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene_automations/switch/legacy-auto",
		map[string]any{"enabled": true}, appHeaders())
	if env.Code != 200 {
		t.Fatalf("switch failed: %d %s", env.Code, env.Message)
	}
	if !hs.core.enableCalls[0].enabled {
		t.Fatal("an explicit enabled=true must be honoured, not toggled")
	}
}

func TestTheLogNeedsAnIdAndSaysWhichOne(t *testing.T) {
	hs := newHarness(t, hs2Sunset())

	env, _ := hs.do(http.MethodGet, "/api/v1/compat/scene_automations/log", nil, appHeaders())
	if env.Code != 100002 {
		t.Fatalf("a missing id is a parameter error, got %d", env.Code)
	}
	if len(hs.core.requests) != 0 {
		t.Fatalf("a malformed request must not reach the service, saw %v", hs.core.requests)
	}
}

func TestTheLogRowSaysWhatHappened(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.refs["legacy-auto"] = map[string]any{"rule_id": "rule-9"}
	hs.core.runs["rule-9"] = map[string]any{
		"total": 1,
		"runs": []map[string]any{{
			"id": "run-1", "status": "failed", "reason_code": "command_refused",
			"started_at": hs.now(),
		}},
	}

	env, _ := hs.do(http.MethodGet, "/api/v1/compat/scene_automations/log?scene_automation_id=legacy-auto", nil, appHeaders())
	if env.Code != 200 {
		t.Fatalf("log failed: %d %s", env.Code, env.Message)
	}
	data := decode[struct {
		Total int              `json:"total"`
		List  []map[string]any `json:"list"`
	}](t, env.Data)
	if data.Total != 1 || len(data.List) != 1 {
		t.Fatalf("expected one row, got %+v", data)
	}
	if data.List[0]["execution_result"] != "F" {
		t.Fatalf("a failed run is F, got %v", data.List[0]["execution_result"])
	}
	if detail, _ := data.List[0]["detail"].(string); !strings.Contains(detail, "command_refused") {
		t.Fatalf("the old log is one line of text and must name the reason: %q", detail)
	}
}

// ---------------------------------------------------------------------------
// Alarms
// ---------------------------------------------------------------------------

func TestAlarmInfoAndHistoryAskForDifferentStates(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.alarms = []map[string]any{{
		"id": "ep-1", "policy_id": "pol-1", "subject_id": "dev-1",
		"state": "active", "level": "H", "opened_at": hs.now(),
	}}
	hs.core.rules["pol-1"] = map[string]any{"id": "pol-1", "name": "高温告警"}

	if env, _ := hs.do(http.MethodGet, "/api/v1/compat/alarm/info", nil, appHeaders()); env.Code != 200 {
		t.Fatalf("alarm info failed: %d %s", env.Code, env.Message)
	}
	if env, _ := hs.do(http.MethodGet, "/api/v1/compat/alarm/info/history", nil, appHeaders()); env.Code != 200 {
		t.Fatalf("alarm history failed: %d %s", env.Code, env.Message)
	}
	if len(hs.core.alarmQuery) != 2 {
		t.Fatalf("expected two upstream reads, got %d", len(hs.core.alarmQuery))
	}
	if got := hs.core.alarmQuery[0].Get("state"); got != "active" {
		t.Fatalf("the alarm card shows active episodes, asked for %q", got)
	}
	if got := hs.core.alarmQuery[1].Get("state"); got != "cleared" {
		t.Fatalf("the history tab shows recovered episodes, asked for %q", got)
	}
}

func TestTheAlarmCardNamesThePolicy(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.alarms = []map[string]any{{
		"id": "ep-1", "policy_id": "pol-1", "subject_id": "dev-1",
		"state": "active", "level": "M", "opened_at": hs.now(),
	}}
	hs.core.rules["pol-1"] = map[string]any{"id": "pol-1", "name": "高温告警"}

	env, _ := hs.do(http.MethodGet, "/api/v1/compat/alarm/info", nil, appHeaders())
	data := decode[struct {
		List []map[string]any `json:"list"`
	}](t, env.Data)
	if len(data.List) != 1 {
		t.Fatalf("expected one alarm, got %+v", data)
	}
	if data.List[0]["name"] != "高温告警" {
		t.Fatalf("the App prints item.name, got %v", data.List[0]["name"])
	}
	if data.List[0]["alarm_status"] != "M" {
		t.Fatalf("alarm_status is the level letter, got %v", data.List[0]["alarm_status"])
	}
}

func TestThePolicyNameIsResolvedOncePerRequest(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.alarms = []map[string]any{
		{"id": "ep-1", "policy_id": "pol-1", "state": "active", "level": "H", "opened_at": hs.now()},
		{"id": "ep-2", "policy_id": "pol-1", "state": "active", "level": "H", "opened_at": hs.now()},
	}
	hs.core.rules["pol-1"] = map[string]any{"id": "pol-1", "name": "高温告警"}

	_, _ = hs.do(http.MethodGet, "/api/v1/compat/alarm/info", nil, appHeaders())

	reads := 0
	for _, req := range hs.core.requests {
		if req == "GET /v1/rules/pol-1" {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("two alarms of the same policy are one lookup, got %d", reads)
	}
}

// ---------------------------------------------------------------------------
// Failures and operations
// ---------------------------------------------------------------------------

func TestAnUpstreamFailureKeepsTheOldEnvelope(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	hs.core.down = true

	env, res := hs.do(http.MethodGet, "/api/v1/compat/scene?page=1&page_size=10", nil, appHeaders())
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the console retries non-2xx, so failures must stay 200; got %d", res.StatusCode)
	}
	if env.Code != codeUpstream {
		t.Fatalf("an unreachable core is %d, got %d", codeUpstream, env.Code)
	}
	if !strings.Contains(env.Message, "暂时不可用") {
		t.Fatalf("the message must be readable to a customer: %q", env.Message)
	}
}

func TestTheSunsetRefusesWithoutTouchingTheCore(t *testing.T) {
	sunset := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	hs := newHarness(t, sunset)
	hs.travel(sunset.Add(time.Hour)) // the adapter's clock moved past the end date
	hs.core.refs["legacy-scene"] = map[string]any{"rule_id": "rule-1"}

	env, res := hs.do(http.MethodPost, "/api/v1/compat/scene/active/legacy-scene", nil, appHeaders())
	if env.Code != codeSunset {
		t.Fatalf("after the sunset the adapter refuses, got %d", env.Code)
	}
	if len(hs.core.runCalls) != 0 {
		t.Fatal("an expired adapter must not run anything")
	}
	if res.Header.Get("Sunset") == "" {
		t.Fatal("the Sunset header is how a client learns the date without reading docs")
	}
}

func TestEveryAnswerCarriesTheSunsetHeader(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	_, res := hs.do(http.MethodGet, "/api/v1/compat/scene?page=1", nil, appHeaders())
	if res.Header.Get("Deprecation") != "true" {
		t.Fatal("a compatibility endpoint must say it is deprecated")
	}
	if res.Header.Get("Sunset") == "" {
		t.Fatal("missing Sunset header")
	}
}

func TestTheAdapterCountsCallsPerConsumer(t *testing.T) {
	hs := newHarness(t, hs2Sunset())

	_, _ = hs.do(http.MethodGet, "/api/v1/compat/scene?page=1", nil, appHeaders())
	_, _ = hs.do(http.MethodGet, "/api/v1/compat/scene?page=1", nil, webHeaders())
	_, _ = hs.do(http.MethodGet, "/api/v1/compat/scene?page=1", nil, map[string]string{"User-Agent": "curl/8.4.0"})

	stats := hs.h.Recorder().Snapshot()
	byConsumer := map[Consumer]int64{}
	for _, s := range stats {
		if s.Endpoint == "scene.list" && s.Outcome == "ok" {
			byConsumer[s.Consumer] = s.Count
		}
	}
	if byConsumer[ConsumerApp] != 1 || byConsumer[ConsumerWeb] != 1 || byConsumer[ConsumerScript] != 1 {
		t.Fatalf("calls must be attributable per consumer, got %+v", byConsumer)
	}
	if len(hs.h.Recorder().ConsumersSeen()) != 3 {
		t.Fatalf("three distinct consumers, got %v", hs.h.Recorder().ConsumersSeen())
	}
}

func TestThePageSizeIsCapped(t *testing.T) {
	hs := newHarness(t, hs2Sunset())

	_, _ = hs.do(http.MethodGet, "/api/v1/compat/scene?page=1&page_size=5000", nil, appHeaders())
	if len(hs.core.listQuery) != 1 {
		t.Fatalf("expected one upstream list, got %d", len(hs.core.listQuery))
	}
	limit, _ := strconv.Atoi(hs.core.listQuery[0].Get("limit"))
	if limit != 100 {
		t.Fatalf("page_size 5000 is a table scan; capped at 100, got %d", limit)
	}
}

func TestTheStatsEndpointExposesTheExitEvidence(t *testing.T) {
	hs := newHarness(t, hs2Sunset())
	_, _ = hs.do(http.MethodGet, "/api/v1/compat/scene?page=1", nil, appHeaders())

	req, _ := http.NewRequest(http.MethodGet, hs.srv.URL+"/api/v1/compat/metrics", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	text := string(raw)
	for _, want := range []string{
		"automation_compat_calls_total",
		`consumer="app"`,
		`endpoint="scene.list"`,
		"automation_compat_sunset_timestamp_seconds",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics output is missing %q:\n%s", want, text)
		}
	}
}

func hs2Sunset() time.Time { return time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC) }

// TestAnActivationThatDidNotRunIsNotASuccess is the defect this adapter found in
// its own first version.
//
// The runtime answers a manual run with a run record whose status can be
// "rejected" (target not in the directory, rule disabled, scope busy). The old
// client shows a success toast for any code 200, so forwarding 200 there tells
// the customer the relay moved when it did not.
func TestAnActivationThatDidNotRunIsNotASuccess(t *testing.T) {
	for _, status := range []string{"rejected", "failed", "ignored", "expired", "partial"} {
		hs := newHarness(t, hs2Sunset())
		hs.core.refs["legacy-scene"] = map[string]any{"rule_id": "rule-1"}
		hs.core.runStatus = status

		env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene/active/legacy-scene", nil, appHeaders())
		if env.Code != codeNotExecuted {
			t.Fatalf("run status %q must not answer 200, got %d (%s)", status, env.Code, env.Message)
		}
		if !strings.Contains(env.Message, "没有执行") {
			t.Fatalf("the message must say the scene did not run, got %q", env.Message)
		}
	}
}

func TestAnAdmittedActivationIsASuccess(t *testing.T) {
	for _, status := range []string{"succeeded", "accepted", "running", "queued"} {
		hs := newHarness(t, hs2Sunset())
		hs.core.refs["legacy-scene"] = map[string]any{"rule_id": "rule-1"}
		hs.core.runStatus = status

		env, _ := hs.do(http.MethodPost, "/api/v1/compat/scene/active/legacy-scene", nil, appHeaders())
		if env.Code != 200 {
			t.Fatalf("run status %q was admitted and must answer 200, got %d", status, env.Code)
		}
	}
}

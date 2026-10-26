package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// A26: the CLI is a shell over the same service
// ---------------------------------------------------------------------------
//
// The tests below never talk to a real server. That is deliberate: what is
// being checked is the *contract* — exit codes, JSON shape, idempotency, and
// which endpoint a legacy command reaches. Whether the endpoint does the right
// thing is already covered by the integration tests on the other side.

type capture struct {
	mu       sync.Mutex
	requests []recorded
}

type recorded struct {
	Method  string
	Path    string
	Query   string
	Body    string
	Idem    string
	Request string
}

func (c *capture) last() recorded {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return recorded{}
	}
	return c.requests[len(c.requests)-1]
}

func (c *capture) all() []recorded {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]recorded{}, c.requests...)
}

// newServer answers every request with status and body, and records what it saw.
func newServer(t *testing.T, status int, body string) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		cap.mu.Lock()
		cap.requests = append(cap.requests, recorded{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Body: buf.String(), Idem: r.Header.Get("Idempotency-Key"),
			Request: r.Header.Get("X-Request-ID"),
		})
		cap.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func decode(t *testing.T, s string) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, s)
	}
	return env
}

// TestAValidationFailureIsMachineReadable: "不依赖解析中文提示". The caller must
// get the field name and a code, not just a sentence.
func TestAValidationFailureIsMachineReadable(t *testing.T) {
	srv, _ := newServer(t, 400, `{
		"code": "invalid_argument",
		"message": "targets: 至少需要指定一个目标",
		"details": {
			"code": "invalid_definition",
			"fields": [{"field":"targets","code":"required","message":"至少需要指定一个目标"}]
		}
	}`)

	code, out, errText := runCLI("--base-url", srv.URL,
		"rules", "validate", "--file", writeTemp(t, `{"definition":{"kind":"automation"}}`))

	if code != exitInvalid {
		t.Fatalf("want exit %d, got %d", exitInvalid, code)
	}
	env := decode(t, out)
	if env.OK {
		t.Fatal("a failed command must not report ok")
	}
	if env.Error.Code != "invalid_argument" || env.Error.AppCode != "invalid_definition" {
		t.Fatalf("codes lost: %+v", env.Error)
	}
	if len(env.Error.Fields) != 1 || env.Error.Fields[0].Field != "targets" {
		t.Fatalf("field-level error missing: %+v", env.Error.Fields)
	}
	if env.Error.ExitCode != exitInvalid {
		t.Fatalf("the envelope must state the exit code: %+v", env.Error)
	}
	// A human still sees the sentence, on stderr, without breaking the JSON.
	if !strings.Contains(errText, "targets") {
		t.Fatalf("stderr should carry the message: %q", errText)
	}
}

// TestAConflictKeepsItsOwnExitCode: "版本冲突…均有机器可读结果".
func TestAConflictKeepsItsOwnExitCode(t *testing.T) {
	srv, _ := newServer(t, 412, `{
		"code": "failed_precondition",
		"message": "预览已过期",
		"details": {"code": "preview_expired"}
	}`)
	code, out, _ := runCLI("--base-url", srv.URL, "rules", "enable", "r1", "--version", "3")
	if code != exitConflict {
		t.Fatalf("want exit %d, got %d", exitConflict, code)
	}
	env := decode(t, out)
	if env.Error.AppCode != "preview_expired" {
		t.Fatalf("app code lost: %+v", env.Error)
	}
	if !env.Error.Retryable {
		t.Fatal("a precondition failure is worth retrying after a refresh")
	}
}

// TestAMissingRunIsNotFound: 404 must be its own exit code, because a script
// polling a run needs to tell "gone" from "still working".
func TestAMissingRunIsNotFound(t *testing.T) {
	srv, _ := newServer(t, 404, `{"code":"not_found","message":"not found"}`)
	code, out, _ := runCLI("--base-url", srv.URL, "runs", "get", "run-x")
	if code != exitNotFound {
		t.Fatalf("want exit %d, got %d", exitNotFound, code)
	}
	if decode(t, out).Error.Code != "not_found" {
		t.Fatal("code lost")
	}
}

// TestATransportFailureIsAnUnknownOutcome: the important distinction. A write
// that got no response may have happened; claiming it failed is how people open
// the light twice.
func TestATransportFailureIsAnUnknownOutcome(t *testing.T) {
	code, out, errText := runCLI("--base-url", "http://127.0.0.1:1", "--timeout", "2s",
		"rules", "run", "r1")
	if code != exitUnknownOutcome {
		t.Fatalf("want exit %d, got %d (%s)", exitUnknownOutcome, code, out)
	}
	env := decode(t, out)
	if env.Error.Code != "outcome_unknown" {
		t.Fatalf("want outcome_unknown, got %+v", env.Error)
	}
	if env.Error.Hint == "" || !strings.Contains(env.Error.Hint, "--idempotency-key") {
		t.Fatalf("an unknown outcome must say how to retry safely: %+v", env.Error)
	}
	if !strings.Contains(errText, "提示") {
		t.Fatalf("the hint should also be readable: %q", errText)
	}
}

// TestAReadThatFailsIsNotUnknown: a GET did nothing, so calling it "unknown"
// would train people to ignore the one status that matters.
func TestAReadThatFailsIsNotUnknown(t *testing.T) {
	code, out, _ := runCLI("--base-url", "http://127.0.0.1:1", "--timeout", "2s",
		"rules", "list")
	if code != exitUnavailable {
		t.Fatalf("want exit %d, got %d", exitUnavailable, code)
	}
	if decode(t, out).Error.Code != "unavailable" {
		t.Fatal("a failed read is unavailable, not unknown")
	}
}

// TestRetryingTheSameIntentUsesTheSameKey: "同一意图重试沿用幂等键". The key is
// derived, so a retry after a timeout cannot open the light twice even though
// the first attempt got no answer.
func TestRetryingTheSameIntentUsesTheSameKey(t *testing.T) {
	srv, cap := newServer(t, 200, `{"id":"r1"}`)
	file := writeTemp(t, `{"name":"高温","definition":{"kind":"automation"},"enabled":true}`)

	if code, _, _ := runCLI("--base-url", srv.URL, "rules", "create", "--file", file); code != 0 {
		t.Fatalf("first create failed: %d", code)
	}
	if code, _, _ := runCLI("--base-url", srv.URL, "rules", "create", "--file", file); code != 0 {
		t.Fatalf("retry failed: %d", code)
	}
	reqs := cap.all()
	if len(reqs) != 2 {
		t.Fatalf("want 2 requests, got %d", len(reqs))
	}
	if reqs[0].Idem == "" || reqs[0].Idem != reqs[1].Idem {
		t.Fatalf("the same intent must reuse the key: %q vs %q", reqs[0].Idem, reqs[1].Idem)
	}
	if !strings.HasPrefix(reqs[0].Idem, "tpctl-rules-create-") {
		t.Fatalf("the key should name the intent: %q", reqs[0].Idem)
	}

	// A different body is a different intent, so it must not collide.
	other := writeTemp(t, `{"name":"低温","definition":{"kind":"automation"}}`)
	if code, _, _ := runCLI("--base-url", srv.URL, "rules", "create", "--file", other); code != 0 {
		t.Fatalf("third create failed: %d", code)
	}
	if cap.last().Idem == reqs[0].Idem {
		t.Fatal("a different body must not reuse the key")
	}
}

// TestAnExplicitKeyWins: scripts that want to pin a key can.
func TestAnExplicitKeyWins(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	file := writeTemp(t, `{"name":"x","definition":{"kind":"automation"}}`)
	code, _, _ := runCLI("--base-url", srv.URL, "--idempotency-key", "fixed-key",
		"rules", "create", "--file", file)
	if code != 0 {
		t.Fatalf("create failed: %d", code)
	}
	if cap.last().Idem != "fixed-key" {
		t.Fatalf("want fixed-key, got %q", cap.last().Idem)
	}
}

// TestReadsCarryNoIdempotencyKey: a read has nothing to de-duplicate.
func TestReadsCarryNoIdempotencyKey(t *testing.T) {
	srv, cap := newServer(t, 200, `{"rules":[]}`)
	if code, _, _ := runCLI("--base-url", srv.URL, "rules", "list"); code != 0 {
		t.Fatalf("list failed: %d", code)
	}
	if cap.last().Idem != "" {
		t.Fatalf("a read should not send an idempotency key, got %q", cap.last().Idem)
	}
}

// TestSimulationAndExecutionAreNamedDifferently: "实际动作与模拟分别命名和输出".
// They are different commands, they reach different endpoints, and only the one
// that can move a device carries an idempotency key.
func TestSimulationAndExecutionAreNamedDifferently(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	samples := writeTemp(t, `[{"at":"0s","device_id":"d1","key":"temperature","value":"40"}]`)

	code, out, _ := runCLI("--base-url", srv.URL, "rules", "simulate", "r1", "--samples", samples)
	if code != 0 {
		t.Fatalf("simulate failed: %d", code)
	}
	sim := cap.last()
	if sim.Path != "/v1/rules/r1/simulate" || sim.Idem != "" {
		t.Fatalf("simulate should be a read-like POST with no key: %+v", sim)
	}
	if env := decode(t, out); env.Command != "rules.simulate" {
		t.Fatalf("the output must name the simulation: %q", env.Command)
	}

	code, out, _ = runCLI("--base-url", srv.URL, "rules", "run", "r1")
	if code != 0 {
		t.Fatalf("run failed: %d", code)
	}
	real := cap.last()
	if real.Path != "/v1/rules/r1/run" || real.Idem == "" {
		t.Fatalf("the real run must be idempotent: %+v", real)
	}
	if env := decode(t, out); env.Command != "rules.run" {
		t.Fatalf("the output must name the real run: %q", env.Command)
	}
}

// TestTheLegacySwitchReachesTheSameEndpoint: old scripts keep working, and they
// keep working *through the same endpoint*, not a second code path.
func TestTheLegacySwitchReachesTheSameEndpoint(t *testing.T) {
	srv, cap := newServer(t, 200, `{"enabled":false}`)
	code, _, _ := runCLI("--base-url", srv.URL, "automations", "switch", "r9", "--off")
	if code != 0 {
		t.Fatalf("legacy switch failed: %d", code)
	}
	got := cap.last()
	// POST, not PUT: the service declares method=POST for this path, and a
	// wrong verb is not a 405 here — it is a 404 "endpoint not found", which
	// reads like "this rule does not exist". The smoke test against the real
	// service is what caught it.
	if got.Method != "POST" || got.Path != "/v1/rules/r9/enabled" {
		t.Fatalf("wrong endpoint: %+v", got)
	}
	if !strings.Contains(got.Body, `"enabled":false`) {
		t.Fatalf("--off should send enabled=false, got %s", got.Body)
	}
}

// TestTheOldCreatePostsTheSameBodyAsTheNewOne: the migration table is honest
// only if the two spellings are byte-identical requests.
func TestTheOldCreatePostsTheSameBodyAsTheNewOne(t *testing.T) {
	srv, cap := newServer(t, 200, `{"id":"r1"}`)
	file := writeTemp(t, `{"name":"高温","definition":{"kind":"automation"}}`)

	if code, _, _ := runCLI("--base-url", srv.URL, "rules", "create", "--file", file); code != 0 {
		t.Fatalf("new create failed: %d", code)
	}
	newReq := cap.last()
	if code, _, _ := runCLI("--base-url", srv.URL, "automations", "create", "--file", file); code != 0 {
		t.Fatalf("legacy create failed: %d", code)
	}
	oldReq := cap.last()

	if oldReq.Path != newReq.Path || oldReq.Method != newReq.Method || oldReq.Body != newReq.Body {
		t.Fatalf("legacy and new differ:\n old %+v\n new %+v", oldReq, newReq)
	}
}

// TestAnUnknownFlagIsAUsageError: no request is made, so a typo cannot cause a
// partial action.
func TestAnUnknownFlagIsAUsageError(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	code, out, _ := runCLI("--base-url", srv.URL, "rules", "run", "r1", "--devce", "d1")
	if code != exitUsage {
		t.Fatalf("want exit %d, got %d", exitUsage, code)
	}
	if len(cap.all()) != 0 {
		t.Fatal("a usage error must not reach the server")
	}
	if decode(t, out).Error.Code != "usage" {
		t.Fatal("usage errors are machine-readable too")
	}
}

// TestMissingPositionalArgumentIsRefused: an empty id would otherwise become a
// request to /v1/rules//explain, which is a different endpoint.
func TestMissingPositionalArgumentIsRefused(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	code, _, _ := runCLI("--base-url", srv.URL, "rules", "get")
	if code != exitUsage {
		t.Fatalf("want exit %d, got %d", exitUsage, code)
	}
	if len(cap.all()) != 0 {
		t.Fatal("no request should be made")
	}
}

// TestRunListPassesItsFilters: the pagination A02 added has to be reachable.
func TestRunListPassesItsFilters(t *testing.T) {
	srv, cap := newServer(t, 200, `{"runs":[],"total":0}`)
	code, _, _ := runCLI("--base-url", srv.URL, "rules", "runs", "r1",
		"--status", "failed,expired", "--limit", "20", "--offset", "40",
		"--after", "2026-01-01T00:00:00Z")
	if code != 0 {
		t.Fatalf("runs failed: %d", code)
	}
	q := cap.last().Query
	for _, want := range []string{"status=failed%2Cexpired", "limit=20", "offset=40", "after="} {
		if !strings.Contains(q, want) {
			t.Fatalf("query %q missing %q", q, want)
		}
	}
}

// TestRawPrintsTheServerBodyUnwrapped: `| jq .rules` should work without
// knowing about our envelope.
func TestRawPrintsTheServerBodyUnwrapped(t *testing.T) {
	srv, _ := newServer(t, 200, `{"rules":[{"id":"r1"}],"total":1}`)
	code, out, _ := runCLI("--base-url", srv.URL, "--raw", "rules", "list")
	if code != 0 {
		t.Fatalf("list failed: %d", code)
	}
	var direct struct {
		Rules []struct{ ID string } `json:"rules"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &direct); err != nil {
		t.Fatalf("--raw output is not the server body: %v\n%s", err, out)
	}
	if len(direct.Rules) != 1 || direct.Rules[0].ID != "r1" {
		t.Fatalf("unexpected body: %s", out)
	}
}

// TestAProxysErrorPageStillBecomesJSON: when the body is not ours, the caller
// still gets a parseable error instead of HTML.
func TestAProxysErrorPageStillBecomesJSON(t *testing.T) {
	srv, _ := newServer(t, 502, `<html>bad gateway</html>`)
	code, out, _ := runCLI("--base-url", srv.URL, "rules", "list")
	if code != exitUnavailable {
		t.Fatalf("want exit %d, got %d", exitUnavailable, code)
	}
	env := decode(t, out)
	if env.Error.Code != "unavailable" || !strings.Contains(env.Error.Message, "bad gateway") {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
}

// TestBundlePreviewSendsTheMappingTheServerExpects keeps the flag spelling and
// the wire field in step; a silent rename here would make every import map
// nothing and refuse with "role_unmapped".
func TestBundlePreviewSendsTheMappingTheServerExpects(t *testing.T) {
	srv, cap := newServer(t, 200, `{"plan":{}}`)
	pkg := writeTemp(t, `{"contractVersion":"1","bundleKey":"k","version":"1","resources":{"rules":[]}}`)
	code, _, _ := runCLI("--base-url", srv.URL, "bundles", "preview", "--file", pkg,
		"--role", "door=dev-1,dev-2", "--slot", "on-call=pol-1")
	if code != 0 {
		t.Fatalf("preview failed: %d", code)
	}
	body := cap.last().Body
	if !strings.Contains(body, `"roleMapping":{"door":["dev-1","dev-2"]}`) {
		t.Fatalf("role mapping lost: %s", body)
	}
	if !strings.Contains(body, `"notifyChoices":{"on-call":"pol-1"}`) {
		t.Fatalf("notify choice lost: %s", body)
	}
	if !strings.Contains(body, `"manifest":{`) {
		t.Fatalf("manifest should be unwrapped from the package file: %s", body)
	}
}

// TestAnInvalidDefinitionFailsTheCommand: the server answers 200 with a report,
// but a validation command that exits 0 on an invalid body is useless in CI.
func TestAnInvalidDefinitionFailsTheCommand(t *testing.T) {
	srv, _ := newServer(t, 200, `{
		"valid": false,
		"issues": [{"field":"targets","code":"empty_targets","message":"至少需要指定一个目标"}]
	}`)
	code, out, _ := runCLI("--base-url", srv.URL, "rules", "validate", "--file",
		writeTemp(t, `{"definition":{"kind":"automation"}}`))
	if code != exitInvalid {
		t.Fatalf("want exit %d, got %d", exitInvalid, code)
	}
	env := decode(t, out)
	if env.OK {
		t.Fatal("an invalid definition is not ok")
	}
	if len(env.Error.Fields) != 1 || env.Error.Fields[0].Field != "targets" {
		t.Fatalf("the report must be turned into field errors: %+v", env.Error)
	}
	// The report itself is still there: "why" is the useful half.
	if env.Data == nil {
		t.Fatal("the validation report should still be printed")
	}
}

// TestAValidDefinitionSucceeds: the same command, the other answer.
func TestAValidDefinitionSucceeds(t *testing.T) {
	srv, _ := newServer(t, 200, `{"valid":true,"issues":[],"step_count":1}`)
	code, out, _ := runCLI("--base-url", srv.URL, "rules", "validate", "--file",
		writeTemp(t, `{"definition":{"kind":"automation"}}`))
	if code != 0 {
		t.Fatalf("want success, got %d (%s)", code, out)
	}
	if !decode(t, out).OK {
		t.Fatal("a valid definition is ok")
	}
}

// TestAnIncompatibleRevisionIsAConflict: "能不能套用已有设备值" is a 412-shaped
// answer even though the endpoint reports it with a 200.
func TestAnIncompatibleRevisionIsAConflict(t *testing.T) {
	srv, _ := newServer(t, 200, `{
		"compatible": false,
		"issues": [{"field":"bindings","code":"binding_incompatible","message":"limit 越界"}]
	}`)
	code, out, _ := runCLI("--base-url", srv.URL, "rules", "check", "r1", "--file",
		writeTemp(t, `{"definition":{"kind":"automation"}}`))
	if code != exitConflict {
		t.Fatalf("want exit %d, got %d", exitConflict, code)
	}
	if decode(t, out).Error.AppCode != "binding_incompatible" {
		t.Fatal("the app code should say why it is a conflict")
	}
}

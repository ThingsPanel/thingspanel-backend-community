package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type recordedCall struct {
	method string
	path   string
	query  string
	idem   string
	auth   string
	tenant string
	body   map[string]any
}

// recorder stands in for the backend. It remembers every request so a test can
// assert on what the tool actually sent, not on what it says it sent.
type recorder struct {
	mu    sync.Mutex
	calls []recordedCall
	reply func(c recordedCall) (int, string)
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var body map[string]any
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
	}
	c := recordedCall{
		method: req.Method,
		path:   req.URL.Path,
		query:  req.URL.RawQuery,
		idem:   req.Header.Get("Idempotency-Key"),
		auth:   req.Header.Get("Authorization"),
		tenant: req.Header.Get("X-Tenant-ID"),
		body:   body,
	}
	r.mu.Lock()
	r.calls = append(r.calls, c)
	r.mu.Unlock()

	status, payload := http.StatusOK, `{"ok":true}`
	if r.reply != nil {
		status, payload = r.reply(c)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, payload)
}

func (r *recorder) snapshot() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func stubClient(t *testing.T, rec *recorder) *client {
	t.Helper()
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return newClient(srv.URL, "dev-token", "", 5*time.Second)
}

// drive feeds newline-delimited JSON-RPC into the server and returns the
// replies it wrote. A clean EOF from stdin is normal shutdown, not a failure.
func drive(t *testing.T, c *client, lines ...string) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	s := newServer(c, &buf)
	if err := s.serve(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil && err != io.EOF {
		t.Fatalf("serve: %v", err)
	}
	var out []map[string]any
	dec := json.NewDecoder(&buf)
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reply is not JSON: %v (raw=%s)", err, buf.String())
		}
		out = append(out, m)
	}
	return out
}

func resultOf(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	if e, ok := reply["error"]; ok {
		t.Fatalf("expected a result, got protocol error: %v", e)
	}
	res, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("reply has no result object: %v", reply)
	}
	return res
}

// toolText pulls the text half out of a tools/call result.
func toolText(t *testing.T, res map[string]any) string {
	t.Helper()
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result has no content: %v", res)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if text == "" {
		t.Fatalf("content[0] is not text: %v", first)
	}
	return text
}

func toolErrorOf(t *testing.T, res map[string]any) apiError {
	t.Helper()
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true, got %v", res["isError"])
	}
	var wrapper struct {
		Error apiError `json:"error"`
	}
	if err := json.Unmarshal([]byte(toolText(t, res)), &wrapper); err != nil {
		t.Fatalf("error text is not the documented envelope: %v", err)
	}
	return wrapper.Error
}

func callLine(id int, name string, args map[string]any) string {
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params,
	})
	return string(raw)
}

// ---------------------------------------------------------------------------
// Protocol
// ---------------------------------------------------------------------------

func TestTheServerSpeaksTheClientsProtocolWhenItCan(t *testing.T) {
	c := stubClient(t, &recorder{})

	replies := drive(t, c,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2019-01-01"}}`,
	)
	if len(replies) != 2 {
		t.Fatalf("want 2 replies, got %d", len(replies))
	}
	known := resultOf(t, replies[0])
	if known["protocolVersion"] != "2025-06-18" {
		t.Fatalf("a version we support must be echoed, got %v", known["protocolVersion"])
	}
	unknown := resultOf(t, replies[1])
	if unknown["protocolVersion"] != defaultProtocol {
		t.Fatalf("an unknown version must fall back to %s, got %v", defaultProtocol, unknown["protocolVersion"])
	}
	info, _ := known["serverInfo"].(map[string]any)
	if info["name"] != serverName {
		t.Fatalf("serverInfo.name = %v", info["name"])
	}
	if _, ok := known["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatalf("server must advertise the tools capability: %v", known["capabilities"])
	}
}

// A notification has no id and must never be answered. Answering one makes
// some hosts treat the reply as a response to a request that was never sent.
func TestANotificationIsNeverAnswered(t *testing.T) {
	c := stubClient(t, &recorder{})
	replies := drive(t, c,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":9}}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	)
	if len(replies) != 1 {
		t.Fatalf("want exactly 1 reply (the ping), got %d: %v", len(replies), replies)
	}
	if replies[0]["id"] != float64(7) {
		t.Fatalf("the reply is not the ping's: %v", replies[0])
	}
}

func TestAnUnknownMethodIsAProtocolError(t *testing.T) {
	c := stubClient(t, &recorder{})
	replies := drive(t, c, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	errObj, ok := replies[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("want a protocol error, got %v", replies[0])
	}
	if errObj["code"] != float64(-32601) {
		t.Fatalf("unknown method must be -32601, got %v", errObj["code"])
	}
}

// ---------------------------------------------------------------------------
// tools/list
// ---------------------------------------------------------------------------

func TestTheToolListIsStableAndComplete(t *testing.T) {
	c := stubClient(t, &recorder{})
	replies := drive(t, c,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	first := resultOf(t, replies[0])["tools"].([]any)
	second := resultOf(t, replies[1])["tools"].([]any)

	// A model that caches the list must not see it shuffle between calls.
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Fatalf("tools/list is not stable between calls")
	}

	var names []string
	for _, entry := range first {
		m := entry.(map[string]any)
		name := m["name"].(string)
		names = append(names, name)
		schema, ok := m["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no inputSchema", name)
		}
		if schema["type"] != "object" {
			t.Fatalf("%s inputSchema.type = %v", name, schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Fatalf("%s inputSchema has no properties", name)
		}
		if _, ok := m["annotations"].(map[string]any); !ok {
			t.Fatalf("%s has no annotations", name)
		}
	}
	if len(names) != 23 {
		t.Fatalf("want 23 tools, got %d: %v", len(names), names)
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("tools/list must be sorted: %v", names)
	}
}

// Every tool the CLI exposes must exist here too: an assistant and a script
// hitting the same problem should not get different capabilities.
func TestEveryReadToolIsMarkedReadOnlyAndEveryDeleteIsDestructive(t *testing.T) {
	c := stubClient(t, &recorder{})
	replies := drive(t, c, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, entry := range resultOf(t, replies[0])["tools"].([]any) {
		m := entry.(map[string]any)
		name := m["name"].(string)
		ann := m["annotations"].(map[string]any)
		wantReadOnly := strings.HasPrefix(name, "list_") ||
			strings.HasPrefix(name, "get_") ||
			strings.HasPrefix(name, "validate_") ||
			strings.HasPrefix(name, "preview_") ||
			strings.HasPrefix(name, "simulate_") ||
			strings.HasPrefix(name, "explain_") ||
			strings.HasPrefix(name, "export_") ||
			name == "device_automation"
		if ann["readOnlyHint"] != wantReadOnly {
			t.Fatalf("%s readOnlyHint = %v, want %v", name, ann["readOnlyHint"], wantReadOnly)
		}
		if name == "delete_rule" && ann["destructiveHint"] != true {
			t.Fatalf("delete_rule must be marked destructive")
		}
		if name == "list_rules" && ann["destructiveHint"] != false {
			t.Fatalf("list_rules must not be marked destructive")
		}
	}
}

// ---------------------------------------------------------------------------
// tools/call — success path
// ---------------------------------------------------------------------------

func TestASuccessfulCallReturnsTextAndStructureThatAgree(t *testing.T) {
	rec := &recorder{reply: func(c recordedCall) (int, string) {
		return 200, `{"rules":[{"id":"r-1","name":"门口灯"}],"total":1}`
	}}
	c := stubClient(t, rec)
	replies := drive(t, c, callLine(1, "list_rules", map[string]any{"kind": "automation", "limit": 10}))

	res := resultOf(t, replies[0])
	if res["isError"] != false {
		t.Fatalf("isError must be false on success: %v", res["isError"])
	}
	structured, ok := res["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("successful object results must carry structuredContent: %v", res)
	}
	var fromText map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &fromText); err != nil {
		t.Fatalf("text half is not JSON: %v", err)
	}
	a, _ := json.Marshal(structured)
	b, _ := json.Marshal(fromText)
	if !bytes.Equal(a, b) {
		t.Fatalf("text and structuredContent disagree:\n%s\n%s", a, b)
	}

	calls := rec.snapshot()
	if len(calls) != 1 || calls[0].path != "/v1/rules" {
		t.Fatalf("list_rules hit %v", calls)
	}
	if !strings.Contains(calls[0].query, "kind=automation") || !strings.Contains(calls[0].query, "limit=10") {
		t.Fatalf("query not forwarded: %q", calls[0].query)
	}
}

// ---------------------------------------------------------------------------
// tools/call — failure path
// ---------------------------------------------------------------------------

// A failure must come back as a tool result, not a protocol error: the model
// needs to read the code and the fields to fix its own call, and a protocol
// error is often shown as "the server is broken" instead of "your input is".
func TestARejectedCallKeepsTheServersCodeAndFields(t *testing.T) {
	rec := &recorder{reply: func(c recordedCall) (int, string) {
		return 400, `{"code":"invalid_argument","message":"规则定义不合法","details":{` +
			`"code":"invalid_definition","fields":[{"field":"steps[0].action","code":"missing",` +
			`"message":"动作不能为空"}]}}`
	}}
	c := stubClient(t, rec)
	replies := drive(t, c, callLine(1, "create_rule", map[string]any{
		"name":       "坏的",
		"definition": map[string]any{"kind": "automation"},
	}))

	res := resultOf(t, replies[0])
	e := toolErrorOf(t, res)
	if e.Code != "invalid_argument" {
		t.Fatalf("code = %q, want the server's own code", e.Code)
	}
	if e.AppCode != "invalid_definition" {
		t.Fatalf("app_code = %q, want the details.code untouched", e.AppCode)
	}
	if len(e.Fields) != 1 || e.Fields[0].Field != "steps[0].action" {
		t.Fatalf("fields did not survive: %+v", e.Fields)
	}
	if e.HTTPStatus != 400 {
		t.Fatalf("http_status = %d", e.HTTPStatus)
	}
}

func TestAnUnknownToolIsAProtocolErrorNotAToolError(t *testing.T) {
	c := stubClient(t, &recorder{})
	replies := drive(t, c, callLine(1, "drop_database", nil))
	errObj, ok := replies[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("calling a tool that does not exist is a protocol error: %v", replies[0])
	}
	if errObj["code"] != float64(-32602) {
		t.Fatalf("code = %v, want -32602", errObj["code"])
	}
}

// Missing required arguments are caught before a request is made, so a model
// that forgot rule_id gets told which field, not a 404 from the backend.
func TestAMissingArgumentIsNamedBeforeAnyRequest(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)
	replies := drive(t, c, callLine(1, "get_rule", map[string]any{}))

	e := toolErrorOf(t, resultOf(t, replies[0]))
	if e.Code != "invalid_arguments" {
		t.Fatalf("code = %q", e.Code)
	}
	if len(e.Fields) != 1 || e.Fields[0].Field != "rule_id" || e.Fields[0].Code != "required" {
		t.Fatalf("fields = %+v", e.Fields)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("a call missing a required argument must not reach the backend, %d requests were made", n)
	}
}

// The tenant comes from the token the process was started with. A model that
// can pass tenant= is a model that can act for another customer.
func TestTheTenantCannotBeChosenByTheModel(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)
	for _, key := range []string{"tenant", "tenant_id", "tenantId"} {
		replies := drive(t, c, callLine(1, "list_rules", map[string]any{key: "someone-else"}))
		e := toolErrorOf(t, resultOf(t, replies[0]))
		if e.Code != "invalid_arguments" {
			t.Fatalf("%s: code = %q", key, e.Code)
		}
		if len(e.Fields) != 1 || e.Fields[0].Field != key {
			t.Fatalf("%s: fields = %+v", key, e.Fields)
		}
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("a tenant argument must be refused before the request, %d requests were made", n)
	}
}

// ---------------------------------------------------------------------------
// Idempotency
// ---------------------------------------------------------------------------

// minimalArgs builds a callable argument set for every tool in the table, so
// the sweep below really exercises all of them instead of a chosen few.
func minimalArgs(t *tool) map[string]any {
	args := map[string]any{}
	for _, name := range t.required {
		switch name {
		case "rule_id", "binding_id", "device_id", "alarm_id", "run_id":
			args[name] = "id-1"
		case "scope_id":
			args[name] = "dev-1"
		case "scope_type":
			args[name] = "device"
		case "name":
			args[name] = "测试"
		case "definition":
			args[name] = map[string]any{"kind": "automation"}
		case "manifest":
			args[name] = map[string]any{"bundle_key": "b", "rules": []any{}}
		case "rule_ids":
			args[name] = []any{"r-1"}
		case "samples":
			args[name] = []any{map[string]any{"at": "0s", "signal": "temp", "value": 30}}
		case "enabled":
			args[name] = true
		case "version":
			args[name] = 1
		}
	}
	return args
}

// A read that is retried twice is harmless, so it carries no key. A write that
// is retried twice turns a light on twice, so it must.
func TestEveryWriteCarriesAKeyAndNoReadDoes(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)

	var lines []string
	id := 0
	byIndex := map[int]*tool{}
	for _, tl := range tools() {
		id++
		byIndex[id] = tl
		lines = append(lines, callLine(id, tl.name, minimalArgs(tl)))
	}
	drive(t, c, lines...)

	calls := rec.snapshot()
	if len(calls) != len(byIndex) {
		t.Fatalf("want %d requests, got %d", len(byIndex), len(calls))
	}
	for i, tl := range byIndex {
		call := calls[i-1]
		if tl.readOnly && call.idem != "" {
			t.Fatalf("%s is read-only but sent Idempotency-Key %q", tl.name, call.idem)
		}
		if !tl.readOnly && call.idem == "" {
			t.Fatalf("%s writes but sent no Idempotency-Key", tl.name)
		}
		if call.auth != "Bearer dev-token" {
			t.Fatalf("%s did not send the configured token: %q", tl.name, call.auth)
		}
	}
}

// The key is derived from the tool and its arguments, so a model that retries
// after a timeout with the same arguments is safe without remembering anything.
func TestTheSameIntentRetriedUsesTheSameKey(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)
	args := map[string]any{"rule_id": "r-1", "definition": map[string]any{"kind": "automation"}, "version": 3}
	drive(t, c, callLine(1, "update_rule", args), callLine(2, "update_rule", args))

	calls := rec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("want 2 requests, got %d", len(calls))
	}
	if calls[0].idem == "" || calls[0].idem != calls[1].idem {
		t.Fatalf("a retry of the same intent must reuse the key: %q vs %q", calls[0].idem, calls[1].idem)
	}

	// A different intent must not collide with it, or the second change would
	// silently return the first one's result.
	other := map[string]any{"rule_id": "r-1", "definition": map[string]any{"kind": "automation"}, "version": 4}
	drive(t, c, callLine(3, "update_rule", other))
	calls = rec.snapshot()
	if calls[2].idem == calls[0].idem {
		t.Fatalf("two different edits produced the same key %q", calls[0].idem)
	}
}

// The key must also be in the body: an Encore typed handler cannot read
// headers, so a header-only key is one the service never sees.
func TestTheKeyTravelsInTheBodyAsWellAsTheHeader(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)
	drive(t, c, callLine(1, "create_rule", map[string]any{
		"name": "高温", "definition": map[string]any{"kind": "automation"},
	}))
	call := rec.snapshot()[0]
	if call.idem == "" {
		t.Fatal("a create must carry a key")
	}
	if got, _ := call.body["request_id"].(string); got != call.idem {
		t.Fatalf("request_id = %q, want the same key as the header %q", got, call.idem)
	}

	// A read must not pick one up: there is nothing to de-duplicate.
	drive(t, c, callLine(2, "list_rules", nil))
	if _, ok := rec.snapshot()[1].body["request_id"]; ok {
		t.Fatal("a read must not send request_id")
	}
}

// TestEveryToolReachesTheEndpointItClaims is a table of the whole surface.
//
// It exists because a stub backend accepts any method: update_rule sent POST to
// an endpoint declared as PUT and every unit test passed, while the real
// service answered 404 "endpoint not found" — which reads like "this rule does
// not exist". Only a table compared against the real declaration catches that.
func TestEveryToolReachesTheEndpointItClaims(t *testing.T) {
	want := map[string]string{
		"list_rules":          "GET /v1/rules",
		"get_rule":            "GET /v1/rules/id-1",
		"validate_rule":       "POST /v1/rule-validation",
		"preview_rule_change": "POST /v1/rules/id-1/change-preview",
		"create_rule":         "POST /v1/rules",
		"update_rule":         "PUT /v1/rules/id-1",
		"set_rule_enabled":    "POST /v1/rules/id-1/enabled",
		"delete_rule":         "DELETE /v1/rules/id-1",
		"simulate_rule":       "POST /v1/rules/id-1/simulate",
		"explain_rule":        "GET /v1/rules/id-1/explain",
		"run_rule":            "POST /v1/rules/id-1/run",
		"list_runs":           "GET /v1/rules/id-1/runs",
		"get_run":             "GET /v1/runs/id-1",
		"list_bindings":       "GET /v1/rules/id-1/bindings",
		"attach_binding":      "POST /v1/rules/id-1/bindings",
		"detach_binding":      "DELETE /v1/bindings/id-1",
		"device_automation":   "GET /v1/device-automation/id-1",
		"list_alarms":         "GET /v1/alarms",
		"ack_alarm":           "POST /v1/alarms/id-1/ack",
		"list_blueprints":     "GET /v1/blueprints",
		"export_bundle":       "POST /v1/bundles/export",
		"preview_bundle":      "POST /v1/bundles/preview",
		"import_bundle":       "POST /v1/bundles/import",
	}

	rec := &recorder{}
	c := stubClient(t, rec)
	var lines []string
	id := 0
	order := []string{}
	for _, tl := range tools() {
		id++
		order = append(order, tl.name)
		lines = append(lines, callLine(id, tl.name, minimalArgs(tl)))
	}
	drive(t, c, lines...)

	calls := rec.snapshot()
	if len(calls) != len(order) {
		t.Fatalf("want %d requests, got %d", len(order), len(calls))
	}
	for i, name := range order {
		got := calls[i].method + " " + calls[i].path
		if want[name] != got {
			t.Fatalf("%s reaches %q, want %q", name, got, want[name])
		}
	}
	if len(want) != len(order) {
		t.Fatalf("the table covers %d tools, the server exposes %d", len(want), len(order))
	}
}

// ---------------------------------------------------------------------------
// Unknown outcome
// ---------------------------------------------------------------------------

// A write whose response never arrives may or may not have happened. Saying
// "failed" is how a light gets turned on twice; saying "unknown" plus a way to
// check is the only honest answer.
func TestATimedOutWriteIsReportedAsAnUnknownOutcome(t *testing.T) {
	rec := &recorder{reply: func(c recordedCall) (int, string) {
		time.Sleep(400 * time.Millisecond)
		return 200, `{"ok":true}`
	}}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	c := newClient(srv.URL, "dev-token", "", 80*time.Millisecond)

	replies := drive(t, c, callLine(1, "run_rule", map[string]any{"rule_id": "r-1"}))
	e := toolErrorOf(t, resultOf(t, replies[0]))
	if e.Code != "outcome_unknown" {
		t.Fatalf("code = %q, want outcome_unknown", e.Code)
	}
	if !e.Retryable {
		t.Fatalf("an unknown outcome must be marked retryable")
	}
	if e.Hint == "" {
		t.Fatalf("an unknown outcome must say how to find out what happened")
	}
}

// A read that times out is simply unavailable: nothing happened, so the caller
// may just try again.
func TestATimedOutReadIsOnlyUnavailable(t *testing.T) {
	rec := &recorder{reply: func(c recordedCall) (int, string) {
		time.Sleep(400 * time.Millisecond)
		return 200, `{"ok":true}`
	}}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	c := newClient(srv.URL, "dev-token", "", 80*time.Millisecond)

	replies := drive(t, c, callLine(1, "list_rules", nil))
	e := toolErrorOf(t, resultOf(t, replies[0]))
	if e.Code != "unavailable" {
		t.Fatalf("code = %q, want unavailable", e.Code)
	}
}

// ---------------------------------------------------------------------------
// Simulation must not be able to act
// ---------------------------------------------------------------------------

// simulate_rule is the tool an assistant is told to use when it only wants to
// know what would happen. If it could reach the run endpoint, "let me check
// first" would open the door.
func TestSimulateNeverReachesTheRunEndpoint(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)
	drive(t, c, callLine(1, "simulate_rule", map[string]any{
		"rule_id": "r-1",
		"samples": []any{map[string]any{"at": "0s", "signal": "temp", "value": 30}},
	}))

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 request, got %d: %v", len(calls), calls)
	}
	if calls[0].path != "/v1/rules/r-1/simulate" {
		t.Fatalf("simulate hit %s", calls[0].path)
	}
	if calls[0].method != http.MethodPost {
		t.Fatalf("method = %s", calls[0].method)
	}
	if calls[0].idem != "" {
		t.Fatalf("a simulation must not reserve an idempotency key")
	}
	if _, ok := calls[0].body["samples"]; !ok {
		t.Fatalf("samples were not forwarded: %v", calls[0].body)
	}
}

// The one place a legacy-shaped call could sneak in: export must be a read, so
// an assistant can inspect a bundle without creating anything.
func TestExportIsAReadAndImportIsAWrite(t *testing.T) {
	rec := &recorder{}
	c := stubClient(t, rec)
	drive(t, c,
		callLine(1, "export_bundle", map[string]any{"rule_ids": []any{"r-1"}}),
		callLine(2, "preview_bundle", map[string]any{"manifest": map[string]any{"bundle_key": "b"}}),
		callLine(3, "import_bundle", map[string]any{"manifest": map[string]any{"bundle_key": "b"}}),
	)
	calls := rec.snapshot()
	if len(calls) != 3 {
		t.Fatalf("want 3 requests, got %d", len(calls))
	}
	if calls[0].idem != "" || calls[1].idem != "" {
		t.Fatalf("export/preview must not carry a key: %q / %q", calls[0].idem, calls[1].idem)
	}
	if calls[2].idem == "" {
		t.Fatalf("import writes, so it needs a key")
	}
	if calls[2].path != "/v1/bundles/import" {
		t.Fatalf("import hit %s", calls[2].path)
	}
}

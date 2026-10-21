package northbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// These tests cover the parts of the northbound service that decide what gets
// sent and how failures are classified. They deliberately do not touch the
// database: the ledger behaviour is exercised by the real acceptance scripts in
// scripts/qa/northbound, which run against PostgreSQL.

// TestClassifyPluginError is a regression test for a crash that took the whole
// service down: the classification used to be inlined as
// `var pe *pluginError; if isPluginFailure(err) { kind = pe.kind }`, which
// dereferenced a nil pointer. Restarting a plugin while deliveries were in
// flight was enough to trigger it, so the fix must stay covered.
func TestClassifyPluginError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want pluginErrorKind
	}{
		{"nil", nil, pluginUnreachable},
		{"plain error", errors.New("connection refused"), pluginUnreachable},
		{"unreachable", &pluginError{kind: pluginUnreachable, msg: "down"}, pluginUnreachable},
		{"timeout", &pluginError{kind: pluginTimeout, msg: "slow"}, pluginTimeout},
		{"rejected", &pluginError{kind: pluginRejected, msg: "401"}, pluginRejected},
		{"protocol", &pluginError{kind: pluginProtocol, msg: "bad json"}, pluginProtocol},
		{"wrapped", fmt.Errorf("open: %w", &pluginError{kind: pluginRejected, msg: "401"}), pluginRejected},
	}
	for _, c := range cases {
		if got := classifyPluginError(c.err); got != c.want {
			t.Errorf("%s: got kind %d, want %d", c.name, got, c.want)
		}
	}
}

func TestIdempotencyKeyIsStableAndVersionFree(t *testing.T) {
	a := IdempotencyKey("t1", "p1", "e1", "default")
	b := IdempotencyKey("t1", "p1", "e1", "default")
	if a != b {
		t.Fatalf("same inputs must produce the same key: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "nb1:") || len(a) != len("nb1:")+64 {
		t.Fatalf("unexpected key shape: %q", a)
	}
	// Different tenant, pipeline, event or branch must never collide.
	others := []string{
		IdempotencyKey("t2", "p1", "e1", "default"),
		IdempotencyKey("t1", "p2", "e1", "default"),
		IdempotencyKey("t1", "p1", "e2", "default"),
		IdempotencyKey("t1", "p1", "e1", "branch2"),
	}
	for _, o := range others {
		if o == a {
			t.Fatalf("key collision: %q", o)
		}
	}
	// The key must not embed the attempt counter or pipeline version, otherwise
	// a retry would look like a new business write.
	if strings.Contains(a, "1") && strings.Contains(a, "default") {
		// a hex digest may contain the digit 1; only the branch name matters
		if strings.Contains(a, "default") {
			t.Fatalf("key must not embed the branch name: %q", a)
		}
	}
}

func TestDeliveryIDIsDeterministic(t *testing.T) {
	a := DeliveryID("t1", "p1", "e1", "default")
	b := DeliveryID("t1", "p1", "e1", "default")
	if a != b {
		t.Fatalf("delivery id must be deterministic: %q vs %q", a, b)
	}
	if c := DeliveryID("t1", "p1", "e2", "default"); c == a {
		t.Fatalf("different events must not share a delivery id")
	}
}

func TestMatchEventReasons(t *testing.T) {
	env := SourceEnvelope{DeviceID: "dev-1", EventType: "telemetry", Subject: "devices/dev-1",
		Data: json.RawMessage(`{"temperature":1,"humidity":2}`)}

	cases := []struct {
		name   string
		filter Filter
		want   bool
	}{
		{"empty filter matches everything", Filter{}, true},
		{"device allowed", Filter{DeviceIDs: []string{"dev-1"}}, true},
		{"device not listed", Filter{DeviceIDs: []string{"dev-2"}}, false},
		{"event type allowed", Filter{EventTypes: []string{"telemetry"}}, true},
		{"event type not listed", Filter{EventTypes: []string{"event"}}, false},
		{"key present", Filter{Keys: []string{"temperature"}}, true},
		{"key absent", Filter{Keys: []string{"pressure"}}, false},
		{"subject prefix", Filter{SubjectMatch: "devices/"}, true},
		{"subject mismatch", Filter{SubjectMatch: "gateway/"}, false},
	}
	for _, tc := range cases {
		got, reason := matchEvent(tc.filter, env)
		if got != tc.want {
			t.Errorf("%s: got %v (%s), want %v", tc.name, got, reason, tc.want)
		}
		if reason == "" {
			t.Errorf("%s: a reason must always be reported", tc.name)
		}
	}
}

func TestApplyTransform(t *testing.T) {
	data := json.RawMessage(`{"temperature":23.5,"humidity":0.62,"battery":88,"secret":"x"}`)

	tr := Transform{
		Include:     []string{"temperature", "humidity", "secret"},
		Rename:      map[string]string{"temperature": "temp_c"},
		Constants:   map[string]json.RawMessage{"site": json.RawMessage(`"lab"`)},
		Mask:        []string{"secret"},
		UnitConvert: map[string]UnitConversion{"humidity": {From: "ratio", To: "%"}},
	}
	warnings := []string{}
	out, err := applyTransform(tr, data, &warnings)
	if err != nil {
		t.Fatalf("applyTransform: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if _, ok := got["battery"]; ok {
		t.Errorf("include must drop fields that are not listed")
	}
	if got["temp_c"] != 23.5 {
		t.Errorf("rename failed: %v", got["temp_c"])
	}
	if got["humidity"] != float64(62) {
		t.Errorf("unit conversion failed: %v", got["humidity"])
	}
	if got["secret"] != "***" {
		t.Errorf("mask failed: %v", got["secret"])
	}
	if got["site"] != "lab" {
		t.Errorf("constant not injected: %v", got["site"])
	}
}

func TestApplyTransformWrapsAndPassesThroughNonObjects(t *testing.T) {
	warnings := []string{}
	out, err := applyTransform(Transform{WrapAs: "values"}, json.RawMessage(`{"a":1}`), &warnings)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if string(out) != `{"values":{"a":1}}` {
		t.Fatalf("unexpected wrap output: %s", out)
	}

	// Arrays and scalars must pass through untouched, with a warning, instead of
	// being silently dropped.
	warnings = []string{}
	out, err = applyTransform(Transform{Include: []string{"a"}}, json.RawMessage(`[1,2]`), &warnings)
	if err != nil {
		t.Fatalf("non-object: %v", err)
	}
	if string(out) != `[1,2]` {
		t.Fatalf("non-object payload must pass through: %s", out)
	}
	if len(warnings) == 0 {
		t.Fatalf("a pass-through must be reported as a warning")
	}
}

func TestConvertUnit(t *testing.T) {
	cases := []struct {
		value    float64
		from, to string
		want     float64
		wantErr  bool
	}{
		{0, "Cel", "K", 273.15, false},
		{0.5, "ratio", "%", 50, false},
		{1000, "Pa", "kPa", 1, false},
		{1, "kWh", "Wh", 1000, false},
		{1000, "mV", "V", 1, false},
		{1, "m", "kg", 0, true},
	}
	for _, tc := range cases {
		got, err := convertUnit(tc.value, tc.from, tc.to)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%v %s->%s: expected an error", tc.value, tc.from, tc.to)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v %s->%s: %v", tc.value, tc.from, tc.to, err)
			continue
		}
		if diff := got - tc.want; diff > 0.001 || diff < -0.001 {
			t.Errorf("%v %s->%s = %v, want %v", tc.value, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 8; attempt++ {
		d := backoffFor(attempt)
		if d < 200*time.Millisecond {
			t.Fatalf("attempt %d: backoff %v is below the floor", attempt, d)
		}
		if d > 6*time.Minute {
			t.Fatalf("attempt %d: backoff %v exceeds the cap plus jitter", attempt, d)
		}
		if attempt > 1 && d < prev/2 {
			t.Fatalf("attempt %d: backoff %v shrank from %v", attempt, d, prev)
		}
		prev = d
	}
	// A target supplied retry_after wins when it asks for a longer wait.
	if d := nextBackoff(1, 7000); d != 7*time.Second {
		t.Fatalf("retry_after must be honoured, got %v", d)
	}
	// A short retry_after must not shorten the platform's own backoff.
	//
	// This has to be expressed as a band, not a lower bound: backoffFor applies
	// ±20% jitter, so attempt 6 (32s base) legitimately lands anywhere in
	// 25.6s..38.4s. Asserting ">= 30s" made this test fail roughly a third of
	// the time, which is worse than not having it: a flaky red is indistinguishable
	// from a real regression.
	if d := nextBackoff(6, 10); d < 25*time.Second || d > 39*time.Second {
		t.Fatalf("a small retry_after must leave attempt 6 inside its jitter band (25.6s..38.4s), got %v", d)
	}
	// The cap still applies to a target that asks for a very long wait.
	if d := nextBackoff(1, 3600_000); d != 5*time.Minute {
		t.Fatalf("cap must apply, got %v", d)
	}
}

func TestEventTypeValidation(t *testing.T) {
	for _, ok := range []string{"telemetry", "attribute", "event"} {
		if !isValidEventType(ok) {
			t.Errorf("%q must be accepted", ok)
		}
	}
	for _, bad := range []string{"", "response", "Telemetry", "alarm"} {
		if isValidEventType(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestRedactKeepsDiagnosisButDropsDetail(t *testing.T) {
	// redact is used when a plugin returns something the platform did not expect.
	// It must not echo an arbitrary string straight into the ledger.
	long := strings.Repeat("x", 5000)
	if got := redact(long); len(got) > 512+len("...(truncated)") {
		t.Fatalf("redact must bound the length, got %d", len(got))
	}
	if got := redact("unknown"); got != "unknown" {
		t.Fatalf("redact must keep short values unchanged, got %q", got)
	}
}

func TestOrDefault(t *testing.T) {
	if got := orDefault("", "fallback"); got != "fallback" {
		t.Fatalf("empty value must fall back, got %q", got)
	}
	if got := orDefault("explicit", "fallback"); got != "explicit" {
		t.Fatalf("explicit value must win, got %q", got)
	}
}

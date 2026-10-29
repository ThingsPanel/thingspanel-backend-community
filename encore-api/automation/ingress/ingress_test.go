package ingress

import (
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

var base = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

// A relay retry must land on the same inbox row. This is the whole point of
// deriving the id from the source instead of the receive time.
func TestTelemetryIDIsStableAcrossRetries(t *testing.T) {
	env := Envelope{
		TenantID: "t1", DeviceID: "d1",
		Values: map[string]any{"temperature": 31.5},
	}
	first, err := Normalize(ClassTelemetry, env, base)
	if err != nil {
		t.Fatal(err)
	}
	// A different receive time and a different map instance, same source data.
	later := base.Add(37 * time.Second)
	env2 := Envelope{
		TenantID: "t1", DeviceID: "d1", ObservedAt: base,
		Values: map[string]any{"temperature": 31.5},
	}
	second, err := Normalize(ClassTelemetry, env2, later)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].EventID != second[0].EventID {
		t.Fatalf("retry changed the event id: %s vs %s", first[0].EventID, second[0].EventID)
	}
}

// Two readings of the same value at different times are two pieces of evidence.
// Collapsing them would break a "held for 5 minutes" window.
func TestTelemetrySameValueDifferentTimeIsNewEvidence(t *testing.T) {
	a, _ := Normalize(ClassTelemetry, Envelope{
		TenantID: "t1", DeviceID: "d1", ObservedAt: base,
		Values: map[string]any{"temperature": 31.5},
	}, base)
	b, _ := Normalize(ClassTelemetry, Envelope{
		TenantID: "t1", DeviceID: "d1", ObservedAt: base.Add(time.Minute),
		Values: map[string]any{"temperature": 31.5},
	}, base.Add(time.Minute))
	if a[0].EventID == b[0].EventID {
		t.Fatal("distinct readings must not share an event id")
	}
}

// One envelope with several keys becomes several events, because the candidate
// index is per (signal, key).
func TestTelemetrySplitsPerKey(t *testing.T) {
	events, err := Normalize(ClassTelemetry, Envelope{
		TenantID: "t1", DeviceID: "d1",
		Values: map[string]any{"humidity": 40, "temperature": 31.5, "battery": 88},
		Units:  map[string]string{"temperature": "℃"},
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	keys := []string{events[0].Key, events[1].Key, events[2].Key}
	want := []string{"battery", "humidity", "temperature"}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys not stable: %v", keys)
		}
	}
	if events[2].Unit != "℃" {
		t.Fatalf("unit lost: %+v", events[2])
	}
	if events[2].Signal != domain.SignalTelemetry {
		t.Fatalf("wrong signal: %s", events[2].Signal)
	}
}

// A source message id beats a content hash: the same message re-sent with
// reformatted JSON must still dedupe.
func TestMessageIDWinsOverContentHash(t *testing.T) {
	a, _ := Normalize(ClassAttribute, Envelope{
		TenantID: "t1", DeviceID: "d1", MessageID: "m-9",
		Values: map[string]any{"mode": "auto"},
	}, base)
	// Same message id, extra key and a later receive time: still the same
	// message, so the "mode" event must dedupe.
	b, _ := Normalize(ClassAttribute, Envelope{
		TenantID: "t1", DeviceID: "d1", MessageID: "m-9",
		Values: map[string]any{"mode": "auto", "ignored": 1},
	}, base.Add(time.Second))
	if byKey(a, "mode").EventID != byKey(b, "mode").EventID {
		t.Fatalf("message id not used for dedupe: %s vs %s",
			byKey(a, "mode").EventID, byKey(b, "mode").EventID)
	}
}

// byKey finds the event a given key produced, so a test can assert on one key
// without depending on the emission order.
func byKey(events []store.IngestEvent, key string) store.IngestEvent {
	for _, e := range events {
		if e.Key == key {
			return e
		}
	}
	panic("no event for key " + key)
}

// An event means "this happened now". It is never a lasting state, so the value
// is the literal true and the parameters ride along for matching.
func TestEventIsInstantaneous(t *testing.T) {
	events, err := Normalize(ClassEvent, Envelope{
		TenantID: "t1", DeviceID: "d1", MessageID: "e-1",
		Values: map[string]any{"identify": "door_open", "level": 3},
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].Key != "door_open" {
		t.Fatalf("event key should be the identifier, got %q", events[0].Key)
	}
	if events[0].Value != true {
		t.Fatalf("event value must be the literal true, got %#v", events[0].Value)
	}
	if events[0].Params["level"] != 3 {
		t.Fatalf("params not carried: %+v", events[0].Params)
	}
	if events[0].Signal != domain.SignalEvent {
		t.Fatalf("wrong signal: %s", events[0].Signal)
	}
}

func TestEventWithoutIdentifierIsRejected(t *testing.T) {
	if _, err := Normalize(ClassEvent, Envelope{
		TenantID: "t1", DeviceID: "d1", Values: map[string]any{"level": 3},
	}, base); err == nil {
		t.Fatal("an event without an identifier must be rejected, not guessed")
	}
}

// The MQTT topic carries "0"/"1" as text while the internal bus carries a bool.
// Both must land on the same normalized value.
func TestStatusAcceptsEveryWireShape(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]any
		want   bool
	}{
		{"topic string online", map[string]any{"1": nil}, true},
		{"explicit online", map[string]any{"online": "1"}, true},
		{"explicit offline", map[string]any{"online": "0"}, false},
		{"bool", map[string]any{"online": true}, true},
		{"number", map[string]any{"status": float64(0)}, false},
		{"text", map[string]any{"status": "offline"}, false},
		{"community broker online", map[string]any{"login": "ON-LINE"}, true},
		{"community broker offline", map[string]any{"login": "OFF-LINE"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The single unnamed value case needs a value, not a nil marker.
			vals := tc.values
			if _, ok := vals["1"]; ok {
				vals = map[string]any{"": "1"}
			}
			events, err := Normalize(ClassStatus, Envelope{
				TenantID: "t1", DeviceID: "d1", Values: vals,
			}, base)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if events[0].Value != tc.want {
				t.Fatalf("want %v, got %#v", tc.want, events[0].Value)
			}
			if events[0].Key != "online" {
				t.Fatalf("status key must be normalized, got %q", events[0].Key)
			}
		})
	}
}

// The alarm owner generates the transition id once and republishes it on retry,
// so it is the dedupe key and the hop counter is incremented exactly once.
func TestAlarmUsesAuthoritativeTransitionID(t *testing.T) {
	env := Envelope{
		TenantID: "t1", DeviceID: "d1",
		Values:     map[string]any{"transition_id": "tr-1", "kind": "qualified", "state": "active"},
		CausalRoot: "root-1", CausalHop: 2,
	}
	a, err := Normalize(ClassAlarm, env, base)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Normalize(ClassAlarm, env, base.Add(time.Hour))
	if a[0].EventID != b[0].EventID {
		t.Fatal("a redelivered transition must not produce a second event")
	}
	if a[0].Key != "qualified" || a[0].Signal != domain.SignalAlarm {
		t.Fatalf("wrong classification: %+v", a[0])
	}
	if a[0].CausalHop != 3 {
		t.Fatalf("hop must increment once per link, got %d", a[0].CausalHop)
	}
	if a[0].CausalRoot != "root-1" {
		t.Fatalf("root must be preserved, got %q", a[0].CausalRoot)
	}
}

// A first alarm has no upstream chain; it becomes its own root so a later link
// can still be traced.
func TestAlarmWithoutCauseBecomesItsOwnRoot(t *testing.T) {
	events, err := Normalize(ClassAlarm, Envelope{
		TenantID: "t1", DeviceID: "d1",
		Values: map[string]any{"transition_id": "tr-9", "kind": "cleared"},
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].CausalRoot != "tr-9" {
		t.Fatalf("want self root tr-9, got %q", events[0].CausalRoot)
	}
}

func TestGatewayClassesKeepTheirOrigin(t *testing.T) {
	cases := []struct {
		class  Class
		signal domain.SignalKind
	}{
		{ClassGatewayTelemetry, domain.SignalTelemetry},
		{ClassGatewayAttribute, domain.SignalAttribute},
		{ClassGatewayEvent, domain.SignalEvent},
	}
	for _, tc := range cases {
		if tc.class.Signal() != tc.signal {
			t.Fatalf("%s mapped to %s", tc.class, tc.class.Signal())
		}
		if !tc.class.IsGateway() {
			t.Fatalf("%s should be a gateway class", tc.class)
		}
	}
	if ClassTelemetry.IsGateway() {
		t.Fatal("direct telemetry is not gateway traffic")
	}
}

func TestEnvelopeValidation(t *testing.T) {
	if _, err := Normalize(ClassTelemetry, Envelope{DeviceID: "d1", Values: map[string]any{"a": 1}}, base); err == nil {
		t.Fatal("missing tenant must be rejected")
	}
	if _, err := Normalize(ClassTelemetry, Envelope{TenantID: "t1", Values: map[string]any{"a": 1}}, base); err == nil {
		t.Fatal("missing device must be rejected")
	}
	if _, err := Normalize(ClassTelemetry, Envelope{TenantID: "t1", DeviceID: "d1"}, base); err == nil {
		t.Fatal("empty payload must be rejected")
	}
	if _, err := Normalize(Class("nonsense"), Envelope{TenantID: "t1", DeviceID: "d1"}, base); err == nil {
		t.Fatal("unknown class must be rejected")
	}
}

// The receive time is only a fallback. An explain view must be able to tell a
// source timestamp from a substituted one, so the fallback is recorded in the
// source label rather than silently changing observed_at.
func TestMissingObservedAtFallsBackToReceiveTime(t *testing.T) {
	events, err := Normalize(ClassTelemetry, Envelope{
		TenantID: "t1", DeviceID: "d1", Values: map[string]any{"temperature": 1},
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if !events[0].ObservedAt.Equal(base) {
		t.Fatalf("want %v, got %v", base, events[0].ObservedAt)
	}
}

package domain

import (
	"testing"
	"time"
)

func sample(device, key string, v any, at time.Time) Sample {
	return Sample{DeviceID: device, Signal: SignalTelemetry, Key: key, Value: v,
		Unit: "℃", Quality: QualityGood, ObservedAt: at}
}

func lookupOf(samples ...Sample) func(string, SignalKind, string) (Sample, bool) {
	m := map[string]Sample{}
	for _, s := range samples {
		m[s.DeviceID+"|"+string(s.Signal)+"|"+s.Key] = s
	}
	return func(deviceID string, sig SignalKind, key string) (Sample, bool) {
		s, ok := m[deviceID+"|"+string(sig)+"|"+key]
		return s, ok
	}
}

func trigger(key, op, val string) Trigger {
	return Trigger{Groups: []Group{{Conditions: []Condition{{
		Source: "self", Signal: SignalTelemetry, Key: key, Operator: op, Value: val, Unit: "℃",
	}}}}}
}

// TestOperators pins every operator the legacy model allowed, so a rewrite can
// never silently drop or reorder comparison semantics.
func TestOperators(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		op    string
		want  string
		value any
		verdict Verdict
	}{
		{"eq number", OpEq, "30", 30.0, VerdictTrue},
		{"eq number from string value", OpEq, "30", "30", VerdictTrue},
		{"eq mismatch", OpEq, "30", 29.9, VerdictFalse},
		{"neq", OpNeq, "30", 31.0, VerdictTrue},
		{"gt", OpGt, "30", 30.1, VerdictTrue},
		{"gt boundary", OpGt, "30", 30.0, VerdictFalse},
		{"lt", OpLt, "30", 29.9, VerdictTrue},
		{"gte boundary", OpGte, "30", 30.0, VerdictTrue},
		{"lte boundary", OpLte, "30", 30.0, VerdictTrue},
		{"between inside", OpBetween, "20-30", 25.0, VerdictTrue},
		{"between boundary", OpBetween, "20-30", 30.0, VerdictTrue},
		{"between outside", OpBetween, "20-30", 30.5, VerdictFalse},
		{"in hit", OpIn, "on,off,auto", "auto", VerdictTrue},
		{"in miss", OpIn, "on,off", "auto", VerdictFalse},
		{"exists true", OpExists, "true", 1.0, VerdictTrue},
		{"exists false", OpExists, "false", nil, VerdictTrue},
		{"bool eq", OpEq, "true", true, VerdictTrue},
		{"string eq", OpEq, "auto", "auto", VerdictTrue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := trigger("temperature", c.op, c.want)
			s := sample("dev-1", "temperature", c.value, now)
			res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(s)})
			if res.Verdict != c.verdict {
				t.Fatalf("op=%s want=%s got=%s reasons=%+v", c.op, c.verdict, res.Verdict, res.Reasons)
			}
		})
	}
}

// TestMalformedComparisonsAreErrors makes sure a bad threshold is reported as a
// configuration error instead of a quiet "false".
func TestMalformedComparisonsAreErrors(t *testing.T) {
	now := time.Now()
	for _, c := range []struct{ op, want string }{
		{OpGt, "hot"}, {OpBetween, "20"}, {OpBetween, "a-b"}, {"~~", "1"},
	} {
		tr := trigger("temperature", c.op, c.want)
		s := sample("dev-1", "temperature", 25.0, now)
		res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(s)})
		if res.Verdict != VerdictError {
			t.Fatalf("op=%q want=error got=%s", c.op, res.Verdict)
		}
	}
}

// TestMissingDataIsUnknown is the core four-value guarantee: no data is not the
// same as "condition is false".
func TestMissingDataIsUnknown(t *testing.T) {
	now := time.Now()
	res := Evaluate(trigger("temperature", OpGt, "30"),
		EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf()})
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown got %s", res.Verdict)
	}
}

// TestUnknownQualityIsUnknown covers a device that reports a value but flags it
// as untrustworthy.
func TestUnknownQualityIsUnknown(t *testing.T) {
	now := time.Now()
	s := sample("dev-1", "temperature", 35.0, now)
	s.Quality = QualityUnknown
	res := Evaluate(trigger("temperature", OpGt, "30"),
		EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(s)})
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown got %s", res.Verdict)
	}
}

// TestUnitMismatchIsError: comparing ℃ with ℉ is a configuration error, not a
// false result.
func TestUnitMismatchIsError(t *testing.T) {
	now := time.Now()
	tr := trigger("temperature", OpGt, "30")
	tr.Groups[0].Conditions[0].Unit = "℉"
	s := sample("dev-1", "temperature", 35.0, now)
	res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(s)})
	if res.Verdict != VerdictError {
		t.Fatalf("want error got %s", res.Verdict)
	}
}

// TestGroupAndShortCircuit: a definite false inside an AND group decides it,
// even when another leaf is unknown.
func TestGroupAndShortCircuit(t *testing.T) {
	now := time.Now()
	tr := Trigger{Groups: []Group{{Conditions: []Condition{
		{Source: "self", Signal: SignalTelemetry, Key: "temperature", Operator: OpGt, Value: "30"},
		{Source: "self", Signal: SignalTelemetry, Key: "humidity", Operator: OpGt, Value: "80"},
	}}}}
	s := sample("dev-1", "temperature", 20.0, now)
	res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(s)})
	if res.Verdict != VerdictFalse {
		t.Fatalf("want false got %s", res.Verdict)
	}
	if !res.Groups[0].ShortCircuit {
		t.Fatal("expected short circuit on definite false")
	}
}

// TestGroupsAreOr: one satisfied group is enough.
func TestGroupsAreOr(t *testing.T) {
	now := time.Now()
	tr := Trigger{Groups: []Group{
		{Conditions: []Condition{{Source: "self", Signal: SignalTelemetry, Key: "temperature", Operator: OpGt, Value: "30"}}},
		{Conditions: []Condition{{Source: "self", Signal: SignalTelemetry, Key: "humidity", Operator: OpGt, Value: "80"}}},
	}}
	hot := sample("dev-1", "temperature", 35.0, now)
	res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(hot)})
	if res.Verdict != VerdictTrue {
		t.Fatalf("want true got %s", res.Verdict)
	}
}

// TestEventConditionOnlyAtEventTime: an event condition is unprovable outside
// the event itself, so it must be unknown rather than false.
func TestEventConditionOnlyAtEventTime(t *testing.T) {
	now := time.Now()
	tr := Trigger{Groups: []Group{{Conditions: []Condition{{
		Source: "self", Signal: SignalEvent, Key: "alarm", Operator: OpExists, Value: "true",
	}}}}}
	res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf()})
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown got %s", res.Verdict)
	}
}

// TestEventParamsMatch: parameter matching narrows an event to a specific
// variant, which is how the legacy event trigger filters payloads.
func TestEventParamsMatch(t *testing.T) {
	now := time.Now()
	tr := Trigger{Groups: []Group{{Conditions: []Condition{{
		Source: "self", Signal: SignalEvent, Key: "alarm", Operator: OpExists, Value: "true",
		EventMatch: &EventMatch{Fields: []EventField{{Path: "level", Operator: OpEq, Value: "critical"}}},
	}}}}}
	ev := Sample{DeviceID: "dev-1", Signal: SignalEvent, Key: "alarm", Value: 1.0,
		Quality: QualityGood, ObservedAt: now, Params: map[string]any{"level": "critical"}}
	res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Trigger: &ev, Lookup: lookupOf()})
	if res.Verdict != VerdictTrue {
		t.Fatalf("want true got %s reasons=%+v", res.Verdict, res.Reasons)
	}
	ev.Params["level"] = "info"
	res = Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Trigger: &ev, Lookup: lookupOf()})
	if res.Verdict != VerdictFalse {
		t.Fatalf("want false got %s", res.Verdict)
	}
}

// TestCrossDeviceCondition: a condition pinned to another device must read that
// device, not the triggering one.
func TestCrossDeviceCondition(t *testing.T) {
	now := time.Now()
	tr := Trigger{Groups: []Group{{Conditions: []Condition{
		{Source: "device", DeviceID: "gateway-1", Signal: SignalTelemetry, Key: "online", Operator: OpEq, Value: "true"},
	}}}}
	gw := Sample{DeviceID: "gateway-1", Signal: SignalTelemetry, Key: "online", Value: true,
		Quality: QualityGood, ObservedAt: now}
	res := Evaluate(tr, EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(gw)})
	if res.Verdict != VerdictTrue {
		t.Fatalf("want true got %s reasons=%+v", res.Verdict, res.Reasons)
	}
}

// TestDurationParsing covers the JSON duration contract used by "for", wait and
// freshness fields.
func TestDurationParsing(t *testing.T) {
	for _, c := range []struct {
		in   string
		want time.Duration
	}{
		{`"5m"`, 5 * time.Minute},
		{`"90s"`, 90 * time.Second},
		{`30`, 30 * time.Second},
	} {
		var d Duration
		if err := d.UnmarshalJSON([]byte(c.in)); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if d.Std() != c.want {
			t.Fatalf("%s: want %v got %v", c.in, c.want, d.Std())
		}
	}
}

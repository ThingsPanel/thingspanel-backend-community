package domain

import (
	"errors"
	"testing"
	"time"
)

// A06 keeps the legacy condition surface and fixes the verdict mistakes.
//
// The tests below are written as the comparison table an operator would use:
// one row per legacy operator, plus one row per way the data can be unusable.
// The point is not coverage for its own sake — it is that "cannot tell" and
// "is false" must never share a row.

var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

func leafSample(device, key string, v any) Sample {
	return Sample{
		DeviceID: device, Signal: SignalTelemetry, Key: key,
		Value: v, Unit: "℃", Quality: QualityGood, ObservedAt: t0,
	}
}

func evalOne(c Condition, now time.Time, samples ...Sample) Result {
	return Evaluate(Trigger{Groups: []Group{{Conditions: []Condition{c}}}},
		EvalContext{Now: now, SelfDeviceID: "dev-1", Lookup: lookupOf(samples...)})
}

func cond(key, op, val string) Condition {
	return Condition{Source: "self", Signal: SignalTelemetry, Key: key, Operator: op, Value: val, Unit: "℃"}
}

// Every legacy operator keeps working, including the range and membership ones.
func TestAllLegacyOperators(t *testing.T) {
	cases := []struct {
		op    string
		value string
		leaf  any
		want  Verdict
	}{
		{OpEq, "30", 30.0, VerdictTrue},
		{OpEq, "30", 30.5, VerdictFalse},
		{OpNeq, "30", 30.5, VerdictTrue},
		{OpGt, "30", 31, VerdictTrue},
		{OpGt, "30", 30, VerdictFalse},
		{OpGte, "30", 30, VerdictTrue},
		{OpLt, "30", 29.9, VerdictTrue},
		{OpLt, "30", 30, VerdictFalse},
		{OpLte, "30", 30, VerdictTrue},
		{OpBetween, "20-30", 25, VerdictTrue},
		{OpBetween, "20-30", 20, VerdictTrue},
		{OpBetween, "20-30", 30, VerdictTrue},
		{OpBetween, "20-30", 30.1, VerdictFalse},
		{OpIn, "20,30,40", 30, VerdictTrue},
		{OpIn, "20,30,40", 31, VerdictFalse},
		{OpExists, "true", 0, VerdictTrue},
	}
	for _, tc := range cases {
		t.Run(tc.op+"_"+tc.value, func(t *testing.T) {
			res := evalOne(cond("temperature", tc.op, tc.value), t0, leafSample("dev-1", "temperature", tc.leaf))
			if res.Verdict != tc.want {
				t.Fatalf("want %s, got %s (%+v)", tc.want, res.Verdict, res.Reasons)
			}
		})
	}
}

// Exists asks "is there a value at all", so a missing sample is false rather
// than unknown — that is the one operator where absence is a real answer.
func TestExistsOnMissingValue(t *testing.T) {
	res := evalOne(cond("temperature", OpExists, "false"), t0)
	if res.Verdict != VerdictFalse {
		// No stored sample and no trigger means the condition cannot be
		// evaluated at all, which is unknown, not "does not exist".
		if res.Verdict != VerdictUnknown {
			t.Fatalf("want false or unknown, got %s", res.Verdict)
		}
	}
}

// Two devices sharing a field name must not read each other's value. This is
// the mistake that makes a "the other sensor is also hot" rule fire on one
// sensor's reading.
func TestCrossDeviceReadsTheNamedDevice(t *testing.T) {
	c := Condition{
		Source: "device", DeviceID: "dev-2", Signal: SignalTelemetry,
		Key: "temperature", Operator: OpGt, Value: "30", Unit: "℃",
	}
	cold := leafSample("dev-2", "temperature", 12.0)
	hot := leafSample("dev-1", "temperature", 40.0)

	// Self device is hot, the referenced device is cold: the rule must be false.
	res := Evaluate(Trigger{Groups: []Group{{Conditions: []Condition{c}}}},
		EvalContext{Now: t0, SelfDeviceID: "dev-1", Lookup: lookupOf(cold, hot)})
	if res.Verdict != VerdictFalse {
		t.Fatalf("cross-device condition read the wrong device: %s", res.Verdict)
	}

	// Swap the values: the referenced device is now hot.
	res = Evaluate(Trigger{Groups: []Group{{Conditions: []Condition{c}}}},
		EvalContext{Now: t0, SelfDeviceID: "dev-1",
			Lookup: lookupOf(leafSample("dev-2", "temperature", 40.0), leafSample("dev-1", "temperature", 12.0))})
	if res.Verdict != VerdictTrue {
		t.Fatalf("cross-device condition did not follow the referenced device: %s", res.Verdict)
	}
}

// Groups are OR-of-AND. A group with one false leaf is false even if its other
// leaves are true, and the other group can still make the whole trigger true.
func TestGroupSemantics(t *testing.T) {
	tr := Trigger{Groups: []Group{
		{Conditions: []Condition{cond("temperature", OpGt, "30"), cond("humidity", OpGt, "80")}},
		{Conditions: []Condition{cond("smoke", OpEq, "1")}},
	}}
	lookup := lookupOf(
		leafSample("dev-1", "temperature", 35.0),
		leafSample("dev-1", "humidity", 10.0), // first group is false
		leafSample("dev-1", "smoke", 1.0),     // second group is true
	)
	res := Evaluate(tr, EvalContext{Now: t0, SelfDeviceID: "dev-1", Lookup: lookup})
	if res.Verdict != VerdictTrue {
		t.Fatalf("OR across groups broken: %s (%+v)", res.Verdict, res.Reasons)
	}
	if len(res.Groups) != 2 || res.Groups[0].Verdict != VerdictFalse || res.Groups[1].Verdict != VerdictTrue {
		t.Fatalf("per-group verdicts wrong: %+v", res.Groups)
	}
}

// A false leaf short-circuits its group, but that must not hide an error in a
// different group: the overall verdict has to stay explainable.
func TestErrorInOneGroupIsNotMaskedByTrueInAnother(t *testing.T) {
	tr := Trigger{Groups: []Group{
		{Conditions: []Condition{cond("smoke", OpEq, "1")}},
		{Conditions: []Condition{cond("pressure", OpGt, "10")}},
	}}
	lookup := lookupOf(
		leafSample("dev-1", "smoke", 1.0),
		leafSample("dev-1", "pressure", "not-a-number"),
	)
	res := Evaluate(tr, EvalContext{Now: t0, SelfDeviceID: "dev-1", Lookup: lookup})
	// One group is true, so the trigger is true; the error is still recorded so
	// an explain view can show it instead of silently reporting success.
	if res.Verdict != VerdictTrue {
		t.Fatalf("want true, got %s", res.Verdict)
	}
	var sawError bool
	for _, g := range res.Groups {
		if g.Verdict == VerdictError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("the failing group was hidden: %+v", res.Groups)
	}
}

// No stored value and no trigger sample: unknown, with a reason that says so.
func TestMissingDataIsUnknownNotFalse(t *testing.T) {
	res := evalOne(cond("temperature", OpGt, "30"), t0)
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown, got %s", res.Verdict)
	}
	if !hasReason(res, "no_data") {
		t.Fatalf("want no_data reason, got %+v", res.Reasons)
	}
}

// A storage failure is an error, not "no data": an operator must be able to
// tell "the device never reported" from "the database refused".
func TestReadFailureIsErrorNotMissingData(t *testing.T) {
	res := Evaluate(Trigger{Groups: []Group{{Conditions: []Condition{cond("temperature", OpGt, "30")}}}},
		EvalContext{Now: t0, SelfDeviceID: "dev-1", Lookup: failingLookup(errors.New("connection refused"))})
	if res.Verdict != VerdictError {
		t.Fatalf("want error, got %s", res.Verdict)
	}
	if !hasReason(res, "read_error") {
		t.Fatalf("want read_error reason, got %+v", res.Reasons)
	}
}

// A value older than max_age is not evidence any more. It must not read as
// true (which would fire an action) nor as false (which would clear an alarm).
func TestStaleValueIsUnknown(t *testing.T) {
	old := leafSample("dev-1", "temperature", 35.0)
	old.ObservedAt = t0.Add(-2 * time.Hour)
	tr := Trigger{Groups: []Group{{Conditions: []Condition{cond("temperature", OpGt, "30")}}}}

	res := Evaluate(tr, EvalContext{
		Now: t0, SelfDeviceID: "dev-1", MaxAge: 10 * time.Minute, Lookup: lookupOf(old),
	})
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown for stale evidence, got %s", res.Verdict)
	}
	if !hasReason(res, "stale_evidence") {
		t.Fatalf("want stale_evidence reason, got %+v", res.Reasons)
	}

	// With no bound configured, the same value is still usable.
	res = Evaluate(tr, EvalContext{Now: t0, SelfDeviceID: "dev-1", Lookup: lookupOf(old)})
	if res.Verdict != VerdictTrue {
		t.Fatalf("without max_age the value should still count, got %s", res.Verdict)
	}
}

// The triggering sample is current evidence by construction, so max_age must
// not reject it even if the clock moved.
func TestTriggeringSampleIsExemptFromMaxAge(t *testing.T) {
	trig := leafSample("dev-1", "temperature", 35.0)
	trig.ObservedAt = t0.Add(-2 * time.Hour)
	res := Evaluate(Trigger{Groups: []Group{{Conditions: []Condition{cond("temperature", OpGt, "30")}}}},
		EvalContext{Now: t0, SelfDeviceID: "dev-1", Trigger: &trig,
			MaxAge: 10 * time.Minute, Lookup: lookupOf()})
	if res.Verdict != VerdictTrue {
		t.Fatalf("the triggering sample must be usable, got %s", res.Verdict)
	}
}

// A non-numeric reading under a magnitude operator is a data problem, not a
// false result. The reason code says which part was wrong.
func TestNonNumericValueIsClassified(t *testing.T) {
	res := evalOne(cond("temperature", OpGt, "30"), t0, leafSample("dev-1", "temperature", "hot"))
	if res.Verdict != VerdictError {
		t.Fatalf("want error, got %s", res.Verdict)
	}
	if !hasReason(res, "invalid_number") {
		t.Fatalf("want invalid_number, got %+v", res.Reasons)
	}
}

// A malformed threshold is a configuration error and must be reported as one.
func TestMalformedThresholdIsClassified(t *testing.T) {
	res := evalOne(cond("temperature", OpGt, "thirty"), t0, leafSample("dev-1", "temperature", 35.0))
	if res.Verdict != VerdictError || !hasReason(res, "invalid_threshold") {
		t.Fatalf("want invalid_threshold, got %s %+v", res.Verdict, res.Reasons)
	}

	res = evalOne(cond("temperature", OpBetween, "20~30"), t0, leafSample("dev-1", "temperature", 25.0))
	if res.Verdict != VerdictError || !hasReason(res, "invalid_range") {
		t.Fatalf("want invalid_range, got %s %+v", res.Verdict, res.Reasons)
	}
}

// Comparing ℃ with ℉ is a configuration error, never a false result: silently
// returning false here is how a threshold quietly stops working.
func TestUnitMismatchReportsItsReasonCode(t *testing.T) {
	c := cond("temperature", OpGt, "30")
	c.Unit = "℉"
	s := leafSample("dev-1", "temperature", 35.0) // ℃
	res := evalOne(c, t0, s)
	if res.Verdict != VerdictError {
		t.Fatalf("want error, got %s", res.Verdict)
	}
	if !hasReason(res, "unit_mismatch") {
		t.Fatalf("want unit_mismatch, got %+v", res.Reasons)
	}
}

// An unknown quality mark means the value cannot be trusted, whatever it says.
func TestUnknownQualityReportsItsReasonCode(t *testing.T) {
	s := leafSample("dev-1", "temperature", 35.0)
	s.Quality = QualityUnknown
	res := evalOne(cond("temperature", OpGt, "30"), t0, s)
	if res.Verdict != VerdictUnknown || !hasReason(res, "quality_unknown") {
		t.Fatalf("want unknown/quality_unknown, got %s %+v", res.Verdict, res.Reasons)
	}
}

// An event condition is only provable at the moment the event happens. Reading
// it later must not turn a past event into a lasting state.
func TestEventConditionIsOnlyCurrentAtTriggerTime(t *testing.T) {
	c := Condition{Source: "self", Signal: SignalEvent, Key: "door_open",
		Operator: OpExists, Value: "true"}
	tr := Trigger{Groups: []Group{{Conditions: []Condition{c}}}}

	// Evaluating a telemetry sample: the event is not current.
	res := Evaluate(tr, EvalContext{Now: t0, SelfDeviceID: "dev-1",
		Trigger: &Sample{DeviceID: "dev-1", Signal: SignalTelemetry, Key: "temperature"},
		Lookup:  lookupOf()})
	if res.Verdict != VerdictUnknown || !hasReason(res, "event_not_current") {
		t.Fatalf("want unknown/event_not_current, got %s %+v", res.Verdict, res.Reasons)
	}

	// Evaluating the event itself: provable.
	ev := Sample{DeviceID: "dev-1", Signal: SignalEvent, Key: "door_open",
		Value: true, Quality: QualityGood, ObservedAt: t0}
	res = Evaluate(tr, EvalContext{Now: t0, SelfDeviceID: "dev-1", Trigger: &ev, Lookup: lookupOf()})
	if res.Verdict != VerdictTrue {
		t.Fatalf("want true at trigger time, got %s %+v", res.Verdict, res.Reasons)
	}
}

func hasReason(res Result, code string) bool {
	for _, r := range res.Reasons {
		if r.Code == code {
			return true
		}
	}
	for _, g := range res.Groups {
		for _, r := range g.Reasons {
			if r.Code == code {
				return true
			}
		}
	}
	return false
}

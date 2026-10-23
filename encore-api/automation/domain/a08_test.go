package domain

import (
	"testing"
	"time"
)

// A08, domain layer: how a stored value earns the right to be treated as
// current evidence.
//
// The defect these tests guard against is a single freshness rule applied to two
// kinds of device. A change-only sensor whose last value is a day old is not
// broken; a periodic sensor that has been silent for an hour is. Using the value
// timestamp for both makes the first look dead, and using nothing makes the
// second look alive.

func statusSample(device string, online bool, at time.Time) Sample {
	return Sample{
		DeviceID: device, Signal: SignalStatus, Key: "online",
		Value: online, Quality: QualityGood, ObservedAt: at,
	}
}

// evalWith builds a context with an explicit freshness contract.
func evalWith(c Condition, ctx EvalContext, samples ...Sample) Result {
	ctx.SelfDeviceID = "dev-1"
	ctx.Lookup = lookupOf(samples...)
	return Evaluate(Trigger{Groups: []Group{{Conditions: []Condition{c}}}}, ctx)
}

// A change-only signal is not stale just because its value is old. What decides
// is whether the device is still alive.
func TestOnChangeValueStaysTrustedWhileHeartbeatIsFresh(t *testing.T) {
	value := leafSample("dev-1", "mode", 1.0) // observed at t0
	ctx := EvalContext{
		Now: t0.Add(6 * time.Hour), Mode: FreshnessOnChange,
		Heartbeat: 10 * time.Minute,
	}
	// The status was refreshed five minutes ago, so the device is alive.
	res := evalWith(cond("mode", OpGt, "0"), ctx,
		value, statusSample("dev-1", true, t0.Add(6*time.Hour-5*time.Minute)))
	if res.Verdict != VerdictTrue {
		t.Fatalf("a fresh heartbeat must keep a change-only value usable, got %s (%v)",
			res.Verdict, res.Reasons)
	}
}

// The same value, same age, but the heartbeat has lapsed: now it is unknown.
func TestOnChangeValueIsUnknownWhenHeartbeatLapsed(t *testing.T) {
	value := leafSample("dev-1", "mode", 1.0)
	ctx := EvalContext{
		Now: t0.Add(6 * time.Hour), Mode: FreshnessOnChange,
		Heartbeat: 10 * time.Minute,
	}
	res := evalWith(cond("mode", OpGt, "0"), ctx,
		value, statusSample("dev-1", true, t0.Add(6*time.Hour-11*time.Minute)))
	if res.Verdict != VerdictUnknown {
		t.Fatalf("a lapsed heartbeat must make the value unknown, got %s", res.Verdict)
	}
	if !hasReason(res, "no_heartbeat") {
		t.Fatalf("want no_heartbeat, got %v", res.Reasons)
	}
}

// Without a heartbeat at all there is no way to tell "quiet" from "gone", and
// guessing either way is the defect. The answer is unknown, with a reason that
// tells the operator to declare the window.
func TestOnChangeWithoutHeartbeatIsUnknownNotStale(t *testing.T) {
	value := leafSample("dev-1", "mode", 1.0)
	ctx := EvalContext{Now: t0.Add(time.Hour), Mode: FreshnessOnChange}
	res := evalWith(cond("mode", OpGt, "0"), ctx, value)
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown, got %s", res.Verdict)
	}
	if !hasReason(res, "heartbeat_not_declared") {
		t.Fatalf("want heartbeat_not_declared, got %v", res.Reasons)
	}
}

// An offline device keeps its last value but that value is not evidence.
func TestOnChangeOfflineDeviceIsUnknown(t *testing.T) {
	value := leafSample("dev-1", "mode", 1.0)
	ctx := EvalContext{Now: t0.Add(time.Minute), Mode: FreshnessOnChange, Heartbeat: 10 * time.Minute}
	res := evalWith(cond("mode", OpGt, "0"), ctx,
		value, statusSample("dev-1", false, t0.Add(time.Minute)))
	if res.Verdict != VerdictUnknown {
		t.Fatalf("want unknown for an offline device, got %s", res.Verdict)
	}
	if !hasReason(res, "device_offline") {
		t.Fatalf("want device_offline, got %v", res.Reasons)
	}
}

// The periodic mode keeps the legacy behaviour exactly: the value's own age
// decides, and a fresh value is simply true.
func TestPeriodicModeStillUsesValueAge(t *testing.T) {
	value := leafSample("dev-1", "temperature", 35.0)
	fresh := EvalContext{Now: t0.Add(time.Minute), Mode: FreshnessPeriodic, MaxAge: 10 * time.Minute}
	if res := evalWith(cond("temperature", OpGt, "30"), fresh, value); res.Verdict != VerdictTrue {
		t.Fatalf("a fresh periodic value must be usable, got %s", res.Verdict)
	}
	stale := EvalContext{Now: t0.Add(time.Hour), Mode: FreshnessPeriodic, MaxAge: 10 * time.Minute}
	res := evalWith(cond("temperature", OpGt, "30"), stale, value)
	if res.Verdict != VerdictUnknown || !hasReason(res, "stale_evidence") {
		t.Fatalf("a stale periodic value must be unknown/stale_evidence, got %s %v",
			res.Verdict, res.Reasons)
	}
}

// An empty mode means periodic. Existing rules must not change meaning because a
// new field appeared.
func TestEmptyModeBehavesAsPeriodic(t *testing.T) {
	value := leafSample("dev-1", "temperature", 35.0)
	res := evalWith(cond("temperature", OpGt, "30"),
		EvalContext{Now: t0.Add(time.Hour), MaxAge: 10 * time.Minute}, value)
	if res.Verdict != VerdictUnknown || !hasReason(res, "stale_evidence") {
		t.Fatalf("empty mode must behave as periodic, got %s %v", res.Verdict, res.Reasons)
	}
}

// The status value arrives in several wire shapes; one helper decides them all,
// so "online" is not read differently in two places.
func TestStatusValueShapesAreAllUnderstood(t *testing.T) {
	cases := map[any]bool{
		true: true, false: false,
		"1": true, "0": false, "true": true, "false": false,
		"online": true, "offline": false,
		float64(1): true, float64(0): false, nil: false,
	}
	for value, want := range cases {
		if got := truthy(value); got != want {
			t.Errorf("truthy(%#v) = %v, want %v", value, got, want)
		}
	}
}

// Validation: an on-change rule without a heartbeat cannot be saved, because the
// runtime could only ever answer "unknown" for it.
func TestOnChangeRequiresHeartbeat(t *testing.T) {
	def := Definition{
		Kind: KindAutomation, Mode: ModeSingle, Scope: ScopeDevice,
		Trigger: Trigger{Groups: []Group{{Conditions: []Condition{cond("temperature", OpGt, "30")}}}},
		Freshness: Freshness{
			MaxAge: Duration(10 * time.Minute), MaxGap: Duration(10 * time.Minute),
			Mode: FreshnessOnChange,
		},
		Targets: TargetSpec{DeviceIDs: []string{"dev-1"}},
		Steps:   []Step{{Kind: StepAction, Action: "telemetry", Identifier: "switch", Value: "1"}},
	}
	def.Normalize()
	err := Validate(&def)
	if err == nil {
		t.Fatal("on_change without heartbeat must be rejected")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("want a validation error, got %T", err)
	}
	found := false
	for _, i := range ve.Issues {
		if i.Code == "heartbeat_required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want heartbeat_required, got %v", ve.Issues)
	}

	// Declaring it makes the same definition publishable.
	def.Freshness.Heartbeat = Duration(5 * time.Minute)
	if err := Validate(&def); err != nil {
		t.Fatalf("a declared heartbeat must be accepted: %v", err)
	}
}

// An unknown mode is a configuration error, not a silent fallback.
func TestUnknownFreshnessModeIsRejected(t *testing.T) {
	def := Definition{
		Kind: KindAutomation, Mode: ModeSingle, Scope: ScopeDevice,
		Trigger: Trigger{Groups: []Group{{Conditions: []Condition{cond("temperature", OpGt, "30")}}}},
		Freshness: Freshness{
			MaxAge: Duration(10 * time.Minute), MaxGap: Duration(10 * time.Minute),
			Mode: FreshnessMode("sometimes"),
		},
		Targets: TargetSpec{DeviceIDs: []string{"dev-1"}},
		Steps:   []Step{{Kind: StepAction, Action: "telemetry", Identifier: "switch", Value: "1"}},
	}
	def.Normalize()
	if err := Validate(&def); err == nil {
		t.Fatal("an unknown freshness mode must be rejected")
	}
}

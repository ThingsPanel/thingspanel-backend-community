package domain

import (
	"testing"
	"time"
)

func TestLinkageWeekdayTimeWindow(t *testing.T) {
	w := TimeWindow{Weekdays: []int{7}, Start: 23 * 3600, End: 2 * 3600, Offset: 330}
	for _, tc := range []struct {
		utc    string
		inside bool
	}{
		{"2026-11-22T17:29:59Z", false}, {"2026-11-22T17:30:00Z", true},
		{"2026-11-22T20:29:59Z", true}, {"2026-11-22T20:30:00Z", false},
		{"2026-11-23T17:30:00Z", false},
	} {
		now, err := time.Parse(time.RFC3339, tc.utc)
		if err != nil {
			t.Fatal(err)
		}
		if got := w.contains(now); got != tc.inside {
			t.Errorf("%s: got %v, want %v", tc.utc, got, tc.inside)
		}
	}
	w = TimeWindow{Weekdays: []int{1}, Start: 8 * 3600, End: 9 * 3600, Offset: -180}
	now := time.Date(2026, 11, 23, 11, 0, 0, 0, time.UTC)
	if !w.contains(now) || w.contains(now.Add(time.Hour)) {
		t.Fatal("same-day negative-offset boundary")
	}
	group := Group{TimeWindows: []TimeWindow{w}, Conditions: []Condition{{Source: "self", Signal: SignalTelemetry, Key: "temp", Operator: ">", Value: "30"}}}
	report := Sample{DeviceID: "qa", Signal: SignalTelemetry, Key: "temp", Value: 40, ObservedAt: now, ReceivedAt: now, Quality: QualityGood}
	ctx := EvalContext{Now: now, SelfDeviceID: "qa", Trigger: &report, MaxAge: time.Minute, Lookup: func(string, SignalKind, string) (Sample, bool, error) { return report, true, nil }}
	if got := Evaluate(Trigger{Groups: []Group{group}}, ctx).Verdict; got != VerdictTrue {
		t.Fatalf("inside: %s", got)
	}
	ctx.Now = now.Add(time.Hour)
	if got := Evaluate(Trigger{Groups: []Group{group}}, ctx); got.Verdict != VerdictFalse || got.Reasons[0].Code != "outside_time_window" {
		t.Fatalf("outside: %+v", got)
	}
}

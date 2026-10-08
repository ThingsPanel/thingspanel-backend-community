package service

import (
	"testing"
	"time"

	"project/internal/model"
)

func TestProcessTimeRangePresetsCoverThroughNow(t *testing.T) {
	cases := map[string]time.Duration{
		"last_5m": 5 * time.Minute, "last_15m": 15 * time.Minute,
		"last_30m": 30 * time.Minute, "last_1h": time.Hour,
		"last_3h": 3 * time.Hour, "last_6h": 6 * time.Hour,
		"last_12h": 12 * time.Hour, "last_24h": 24 * time.Hour,
		"last_3d": 72 * time.Hour, "last_7d": 7 * 24 * time.Hour,
		"last_15d": 15 * 24 * time.Hour, "last_30d": 30 * 24 * time.Hour,
		"last_60d": 60 * 24 * time.Hour, "last_90d": 90 * 24 * time.Hour,
		"last_6m": 180 * 24 * time.Hour, "last_1y": 365 * 24 * time.Hour,
	}
	for preset, duration := range cases {
		t.Run(preset, func(t *testing.T) {
			req := &model.GetTelemetryStatisticReq{
				TimeRange: preset, AggregateWindow: "1h",
				StartTime: 1, EndTime: 2,
			}
			before := time.Now().UnixMilli()
			if err := processTimeRange(req); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			after := time.Now().UnixMilli()
			if req.EndTime < before || req.EndTime > after {
				t.Fatalf("end time %d is not current time [%d, %d]", req.EndTime, before, after)
			}
			if got := req.EndTime - req.StartTime; got != duration.Milliseconds() {
				t.Fatalf("range = %d ms, want %d ms", got, duration.Milliseconds())
			}
		})
	}
}

func TestProcessTimeRangeRejectsLongPresetNonAggregateQuery(t *testing.T) {
	req := &model.GetTelemetryStatisticReq{
		TimeRange:       "last_30d",
		AggregateWindow: "no_aggregate",
	}

	if err := processTimeRange(req); err == nil {
		t.Fatal("expected last_30d non-aggregate query to be rejected")
	}
	if got := req.EndTime - req.StartTime; got <= 24*time.Hour.Milliseconds() {
		t.Fatalf("expected resolved range to remain longer than one day, got %d ms", got)
	}
}

func TestProcessTimeRangeAllowsShortPresetNonAggregateQuery(t *testing.T) {
	req := &model.GetTelemetryStatisticReq{
		TimeRange:       "last_24h",
		AggregateWindow: "no_aggregate",
	}

	if err := processTimeRange(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := req.EndTime - req.StartTime; got <= 0 || got > 24*time.Hour.Milliseconds() {
		t.Fatalf("unexpected resolved range: %d ms", got)
	}
}

func TestProcessTimeRangeRejectsLongCustomNonAggregateQuery(t *testing.T) {
	start := time.Now().Add(-48 * time.Hour).UnixMilli()
	req := &model.GetTelemetryStatisticReq{
		TimeRange:       "custom",
		StartTime:       start,
		EndTime:         time.Now().UnixMilli(),
		AggregateWindow: "no_aggregate",
	}

	if err := processTimeRange(req); err == nil {
		t.Fatal("expected long custom non-aggregate query to be rejected")
	}
}

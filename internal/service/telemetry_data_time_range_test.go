package service

import (
	"testing"
	"time"

	"project/internal/model"
)

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

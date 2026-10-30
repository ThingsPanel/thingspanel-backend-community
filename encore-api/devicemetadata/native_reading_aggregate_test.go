package devicemetadata

import (
	"encoding/json"
	"testing"
	"time"

	"encore.app/internal/communityclient"
)

func sample(at string, value string) communityclient.HistoryReading {
	ts, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		panic(err)
	}
	return communityclient.HistoryReading{Key: "temp", Ts: ts, Value: json.RawMessage(value)}
}

func TestAggregateHistoryBucketsAreFixedUTCWindowsWithRealCounts(t *testing.T) {
	rows := []communityclient.HistoryReading{
		sample("2026-10-29T01:00:05Z", "10"),
		sample("2026-10-29T01:59:59.999Z", "20"),
		sample("2026-10-29T02:00:00Z", "4"), // next bucket: window ends are exclusive
		sample("2026-10-29T04:30:00Z", "7"), // buckets 02:00 and 04:00 exist, 03:00 stays empty
	}
	got, err := aggregateHistory("dev", "temp", "avg", "1h", rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("empty buckets must be omitted, got %d buckets: %+v", len(got), got)
	}
	// newest bucket first
	if got[0].ObservedAt != "2026-10-29T04:00:00.000Z" || string(got[0].Value) != "7" || *got[0].SampleCount != 1 {
		t.Errorf("04:00 bucket: %+v", got[0])
	}
	if got[1].ObservedAt != "2026-10-29T02:00:00.000Z" || string(got[1].Value) != "4" {
		t.Errorf("02:00 bucket: %+v", got[1])
	}
	first := got[2]
	if first.ObservedAt != "2026-10-29T01:00:00.000Z" || *first.WindowStart != first.ObservedAt || *first.WindowEnd != "2026-10-29T02:00:00.000Z" {
		t.Errorf("01:00 bucket boundaries: %+v", first)
	}
	if string(first.Value) != "15" || *first.SampleCount != 2 || first.Aggregation != "avg" || first.CacheVersion != nil || first.SnapshotID != nil {
		t.Errorf("01:00 bucket value: %+v", first)
	}
	if first.SampleID != "legacy-agg:temp:avg:1h:2026-10-29T01:00:00.000Z" {
		t.Errorf("bucket identity must be stable: %s", first.SampleID)
	}
}

func TestAggregateHistoryFunctions(t *testing.T) {
	rows := []communityclient.HistoryReading{
		sample("2026-10-29T00:00:10Z", "3"),
		sample("2026-10-29T00:00:20Z", "9.5"),
		sample("2026-10-29T00:00:25Z", "1"),
	}
	want := map[string]string{"min": "1", "max": "9.5", "sum": "13.5", "avg": "4.5", "diff": "8.5", "last": "1"}
	for fn, expected := range want {
		got, err := aggregateHistory("dev", "temp", fn, "1m", rows)
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %+v %v", fn, got, err)
		}
		if string(got[0].Value) != expected || *got[0].SampleCount != 3 {
			t.Errorf("%s = %s (count %d), want %s over 3 samples", fn, got[0].Value, *got[0].SampleCount, expected)
		}
	}
}

func TestAggregateHistoryWindowAlignmentAndNonNumericRules(t *testing.T) {
	// 7d and 30d windows are multiples of the Unix epoch, not calendar weeks or months.
	got, err := aggregateHistory("dev", "temp", "sum", "7d", []communityclient.HistoryReading{sample("1970-01-09T00:00:00Z", "1")})
	if err != nil || *got[0].WindowStart != "1970-01-08T00:00:00.000Z" || *got[0].WindowEnd != "1970-01-15T00:00:00.000Z" {
		t.Fatalf("7d alignment: %+v %v", got, err)
	}
	// Text samples cannot be summed but can be reported as the last value.
	text := []communityclient.HistoryReading{sample("2026-10-29T00:00:00Z", `"on"`), sample("2026-10-29T00:00:30Z", `"off"`)}
	if _, err := aggregateHistory("dev", "mode", "avg", "1m", text); err == nil {
		t.Error("avg over strings must be refused")
	}
	last, err := aggregateHistory("dev", "mode", "last", "1m", text)
	if err != nil || string(last[0].Value) != `"off"` {
		t.Fatalf("last over strings: %+v %v", last, err)
	}
	// Raw is not an aggregation here, and an unknown window is refused.
	if _, err := aggregateHistory("dev", "temp", "raw", "1m", nil); err == nil {
		t.Error("raw must not be handled by the aggregator")
	}
	if _, err := aggregateHistory("dev", "temp", "avg", "45m", nil); err == nil {
		t.Error("unknown window must be refused")
	}
	empty, err := aggregateHistory("dev", "temp", "avg", "1m", nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("no samples means no buckets: %+v %v", empty, err)
	}
}

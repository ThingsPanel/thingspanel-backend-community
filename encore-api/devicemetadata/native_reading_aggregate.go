package devicemetadata

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"

	"encore.app/internal/communityclient"
	"encore.dev/beta/errs"
)

// Aggregated readings. Community's own aggregation does not say how many samples
// went into a bucket, which the contract requires (`sampleCount`), so the
// aggregation is done here over the raw samples instead. That makes the buckets
// and counts exact, at the price of a row cap: a request whose range holds more
// than aggregateMaxRows samples is refused rather than answered approximately.

const (
	aggregateMaxRows  = 50000
	aggregateMaxRange = 366 * 24 * time.Hour
	aggregateTimeFmt  = "2006-01-02T15:04:05.000Z"
)

var readingWindows = map[string]time.Duration{
	"30s": 30 * time.Second, "1m": time.Minute, "2m": 2 * time.Minute, "5m": 5 * time.Minute,
	"10m": 10 * time.Minute, "30m": 30 * time.Minute, "1h": time.Hour, "3h": 3 * time.Hour,
	"6h": 6 * time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
}

var numericAggregations = map[string]bool{"min": true, "max": true, "avg": true, "sum": true, "diff": true}

func validAggregation(name string) bool {
	return name == "raw" || name == "last" || numericAggregations[name]
}

type aggregateBucket struct {
	start   int64 // epoch ms, multiple of the window
	count   int64
	min     float64
	max     float64
	sum     float64
	lastTs  int64
	lastRaw json.RawMessage
}

// aggregateHistory buckets samples into fixed UTC windows aligned to the Unix
// epoch (a 30d window is exactly 30 days, not a calendar month). Empty buckets
// are omitted. Result order is newest bucket first.
func aggregateHistory(deviceID, identifier, aggregation, window string, rows []communityclient.HistoryReading) ([]NativeReading, error) {
	width, ok := readingWindows[window]
	if !ok || !validAggregation(aggregation) || aggregation == "raw" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "unsupported aggregation or window"}
	}
	widthMs := width.Milliseconds()
	buckets := map[int64]*aggregateBucket{}
	for _, row := range rows {
		ms := row.Ts.UnixMilli()
		start := ms - ((ms%widthMs)+widthMs)%widthMs
		b := buckets[start]
		if b == nil {
			b = &aggregateBucket{start: start, min: math.Inf(1), max: math.Inf(-1)}
			buckets[start] = b
		}
		b.count++
		if numericAggregations[aggregation] {
			var v float64
			if err := json.Unmarshal(row.Value, &v); err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, &errs.Error{Code: errs.InvalidArgument, Message: aggregation + " needs numeric samples, but " + identifier + " has a non-numeric value"}
			}
			b.sum += v
			b.min = math.Min(b.min, v)
			b.max = math.Max(b.max, v)
		} else if b.lastRaw == nil || ms > b.lastTs || (ms == b.lastTs && string(row.Value) > string(b.lastRaw)) {
			b.lastTs, b.lastRaw = ms, row.Value
		}
	}
	ordered := make([]*aggregateBucket, 0, len(buckets))
	for _, b := range buckets {
		ordered = append(ordered, b)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].start > ordered[j].start })
	out := make([]NativeReading, 0, len(ordered))
	for _, b := range ordered {
		var value json.RawMessage
		switch aggregation {
		case "last":
			value = b.lastRaw
		default:
			number := map[string]float64{"min": b.min, "max": b.max, "sum": b.sum, "avg": b.sum / float64(b.count), "diff": b.max - b.min}[aggregation]
			if math.IsInf(number, 0) || math.IsNaN(number) {
				return nil, &errs.Error{Code: errs.InvalidArgument, Message: "the aggregate is outside the representable number range"}
			}
			value = json.RawMessage(strconv.FormatFloat(number, 'g', -1, 64))
		}
		startAt := time.UnixMilli(b.start).UTC().Format(aggregateTimeFmt)
		endAt := time.UnixMilli(b.start + widthMs).UTC().Format(aggregateTimeFmt)
		count := b.count
		out = append(out, NativeReading{
			DeviceID: deviceID, Identifier: identifier, Kind: "telemetry", Value: value, ObservedAt: startAt,
			SampleID:    "legacy-agg:" + identifier + ":" + aggregation + ":" + window + ":" + startAt,
			Aggregation: aggregation, WindowStart: &startAt, WindowEnd: &endAt, SampleCount: &count,
		})
	}
	return out, nil
}

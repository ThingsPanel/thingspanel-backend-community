package automationcompat

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CallStat is one (consumer, endpoint, outcome) counter.
type CallStat struct {
	Consumer  Consumer  `json:"consumer"`
	Endpoint  string    `json:"endpoint"`
	Outcome   string    `json:"outcome"`
	Count     int64     `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Recorder counts compat calls.
//
// In-process, not in the database, on purpose: this number answers "can we
// delete this adapter yet", and a per-request INSERT would make the adapter more
// expensive than the code it is standing in for. The counters reset on restart,
// which is why they are exported to Prometheus as well — the long-term series is
// the one an operator reads before flipping the switch.
type Recorder struct {
	mu     sync.Mutex
	now    func() time.Time
	counts map[statKey]*CallStat
}

type statKey struct {
	consumer Consumer
	endpoint string
	outcome  string
}

// NewRecorder builds a recorder. now may be nil.
func NewRecorder(now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{now: now, counts: make(map[statKey]*CallStat)}
}

// Observe records one call.
//
// outcome is one of "ok", "refused" (the adapter said no and the caller can see
// why), "not_found", "unsupported" (a path the adapter deliberately does not
// translate) or "error" (the upstream or the network failed). Keeping "refused"
// apart from "error" is what lets an operator tell "customers are hitting a
// limit" from "our upstream is down".
func (r *Recorder) Observe(consumer Consumer, endpoint, outcome string) {
	if r == nil {
		return
	}
	k := statKey{consumer: consumer, endpoint: endpoint, outcome: outcome}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.counts[k]
	if !ok {
		now := r.now()
		s = &CallStat{Consumer: consumer, Endpoint: endpoint, Outcome: outcome, FirstSeen: now}
		r.counts[k] = s
	}
	s.Count++
	s.LastSeen = r.now()
}

// Snapshot returns a stable, sorted copy.
func (r *Recorder) Snapshot() []CallStat {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CallStat, 0, len(r.counts))
	for _, s := range r.counts {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Consumer != out[j].Consumer {
			return out[i].Consumer < out[j].Consumer
		}
		if out[i].Endpoint != out[j].Endpoint {
			return out[i].Endpoint < out[j].Endpoint
		}
		return out[i].Outcome < out[j].Outcome
	})
	return out
}

// ConsumersSeen lists the consumers that have called at least once, which is the
// first half of an exit condition ("nobody from the App has called for a
// release").
func (r *Recorder) ConsumersSeen() []Consumer {
	seen := map[Consumer]bool{}
	for _, s := range r.Snapshot() {
		seen[s.Consumer] = true
	}
	out := make([]Consumer, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Prometheus renders the counters in the text exposition format.
//
// Hand-rolled instead of using the client library so this package stays
// dependency-free: the adapter is meant to be deletable, and a package that
// pulls in a metrics stack is harder to delete than one that prints nine lines.
func (r *Recorder) Prometheus(sunset time.Time) string {
	var b strings.Builder
	b.WriteString("# HELP automation_compat_calls_total Calls answered by the legacy compatibility adapter.\n")
	b.WriteString("# TYPE automation_compat_calls_total counter\n")
	for _, s := range r.Snapshot() {
		fmt.Fprintf(&b,
			"automation_compat_calls_total{consumer=%q,endpoint=%q,outcome=%q} %d\n",
			s.Consumer, s.Endpoint, s.Outcome, s.Count)
	}
	b.WriteString("# HELP automation_compat_last_call_timestamp_seconds Last time this combination was seen.\n")
	b.WriteString("# TYPE automation_compat_last_call_timestamp_seconds gauge\n")
	for _, s := range r.Snapshot() {
		fmt.Fprintf(&b,
			"automation_compat_last_call_timestamp_seconds{consumer=%q,endpoint=%q,outcome=%q} %d\n",
			s.Consumer, s.Endpoint, s.Outcome, s.LastSeen.Unix())
	}
	if !sunset.IsZero() {
		b.WriteString("# HELP automation_compat_sunset_timestamp_seconds When the adapter stops answering.\n")
		b.WriteString("# TYPE automation_compat_sunset_timestamp_seconds gauge\n")
		fmt.Fprintf(&b, "automation_compat_sunset_timestamp_seconds %d\n", sunset.Unix())
	}
	return b.String()
}

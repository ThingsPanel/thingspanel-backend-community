// Package runtime implements the finite execution model: durable ingress,
// persistent pending state, a single evaluator, finite runs with durable waits,
// and a transactional outbox for side effects.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/store"
)

// Clock is injectable so tests can drive virtual time.
type Clock func() time.Time

const (
	// MaxCausalHop bounds an alarm-driven chain. Each alarm-triggered run adds
	// one hop; a chain that reaches the limit is stopped and the reason is
	// published, so "why did nothing happen" stays answerable.
	MaxCausalHop = 8

	// StatusCoalesce is how long a repeated online/offline report is treated as
	// the same state. Devices re-announce online on every reconnect; without
	// this window each announcement would start another run.
	StatusCoalesce = 60 * time.Second
)

// cause carries the chain origin through the evaluation path so a run can be
// traced back to the alarm transition that started it.
type cause struct {
	Root    string
	Hop     int
	EventID string
	// Manual marks a run started by an explicit user action ("激活" on a scene)
	// instead of an evaluated trigger. It travels into the run snapshot so the
	// detail view can say "手动执行" instead of inferring it from an empty
	// trigger_event_id — an empty id is also what a timer-driven run looks like.
	Manual bool
}

// causeOf reads the causal context off an inbox event.
func causeOf(e *store.IngestEvent) cause {
	return cause{Root: e.CausalRoot, Hop: e.CausalHop, EventID: e.EventID}
}

// Runtime wires the store and the outbound ports.
type Runtime struct {
	store *store.Store
	cmds  ports.CommandOwner
	notif ports.NotificationOwner
	dir   ports.Directory
	clock Clock

	// workerID identifies this process when it leases a schedule or a fire.
	// Two replicas therefore never claim the same due row, and a lease left
	// behind by a crashed process simply expires.
	workerID string
}

// New builds a runtime. Any port may be nil, in which case the corresponding
// side effect is recorded as unknown instead of pretending to succeed.
func New(st *store.Store, cmds ports.CommandOwner, notif ports.NotificationOwner, dir ports.Directory, clock Clock) *Runtime {
	if clock == nil {
		clock = time.Now
	}
	return &Runtime{
		store:    st,
		cmds:     cmds,
		notif:    notif,
		dir:      dir,
		clock:    clock,
		workerID: "w-" + NewID()[:8],
	}
}

// WorkerID exposes this process identity for logs and diagnostics.
func (r *Runtime) WorkerID() string { return r.workerID }

// Store exposes the store for the API layer (read paths only).
func (r *Runtime) Store() *store.Store { return r.store }

// Directory exposes the device directory port.
//
// It is exported for the template path only: instantiating a template must check
// that the chosen device really exposes the signal the template needs, and doing
// that here keeps a single reader of the community device list instead of a
// second client in the API layer. It may be nil, which callers must treat as
// "cannot verify" rather than "verified".
func (r *Runtime) Directory() ports.Directory { return r.dir }

// Now returns the runtime clock.
func (r *Runtime) Now() time.Time { return r.clock() }

// NewID returns a random identifier.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("id-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func hexEncode(b []byte) string { return hex.EncodeToString(b) }

// ScopeKey decides which runs compete with each other.
func ScopeKey(def *domain.Definition, ruleID, deviceID string) string {
	if def.Scope == domain.ScopeRule {
		return "rule:" + ruleID
	}
	return "rule:" + ruleID + ":device:" + deviceID
}

// ---------------------------------------------------------------------------
// Finite program
// ---------------------------------------------------------------------------

// OpCode is one instruction of the compiled rule body.
type OpCode int

const (
	OpStep OpCode = iota // a real step
	OpJump               // unconditional jump
)

// Instruction is a compiled step with branch targets.
type Instruction struct {
	Op         OpCode
	Path       string
	Step       domain.Step
	ElseTarget int
	JumpTarget int
}

// Compile flattens the step tree into a linear program. Branches become
// conditional jumps, so a run is a simple program counter that can be persisted
// and resumed. It is bounded by the validated step budget.
func Compile(steps []domain.Step) []Instruction {
	var out []Instruction
	var walk func(steps []domain.Step, prefix string)
	walk = func(steps []domain.Step, prefix string) {
		for i, s := range steps {
			path := fmt.Sprintf("%s%d", prefix, i)
			if s.Kind != domain.StepBranch {
				out = append(out, Instruction{Op: OpStep, Path: path, Step: s})
				continue
			}
			brIdx := len(out)
			out = append(out, Instruction{Op: OpStep, Path: path, Step: s})
			walk(s.Then, path+".")
			jumpIdx := len(out)
			out = append(out, Instruction{Op: OpJump})
			elseStart := len(out)
			walk(s.Else, path+".")
			end := len(out)
			out[brIdx].ElseTarget = elseStart
			out[jumpIdx].JumpTarget = end
		}
	}
	walk(steps, "")
	return out
}

// ---------------------------------------------------------------------------
// Freshness
// ---------------------------------------------------------------------------

// Freshness describes whether the newest evidence is still trustworthy.
type Freshness struct {
	OK       bool
	Reason   string
	Age      time.Duration
	LastSeen time.Time
}

// checkFreshness decides whether a stored sample may be used to prove that a
// condition still holds at `now`.
func checkFreshness(s domain.Sample, now time.Time, f domain.Freshness) Freshness {
	if s.ObservedAt.IsZero() {
		return Freshness{OK: false, Reason: "no_evidence"}
	}
	age := now.Sub(s.ObservedAt)
	if age < 0 {
		age = 0
	}
	if f.MaxAge > 0 && age > f.MaxAge.Std() {
		return Freshness{OK: false, Reason: "stale", Age: age, LastSeen: s.ObservedAt}
	}
	return Freshness{OK: true, Age: age, LastSeen: s.ObservedAt}
}

// evalContext builds the evaluator context from a rule definition.
//
// Every path goes through it — real evaluation, recovery, branch conditions,
// pending timers — so a rule's freshness contract cannot mean one thing on the
// ingest path and something else on the timer path.
func (r *Runtime) evalContext(ctx context.Context, def *domain.Definition, deviceID string,
	trigger *domain.Sample, now time.Time) domain.EvalContext {

	return domain.EvalContext{
		Now:          now,
		SelfDeviceID: deviceID,
		Trigger:      trigger,
		MaxAge:       def.Freshness.MaxAge.Std(),
		Mode:         def.Freshness.Mode,
		Heartbeat:    def.Freshness.Heartbeat.Std(),
		Lookup:       r.lookup(ctx),
	}
}

// lookupFunc adapts the store to the evaluator's Lookup signature.
// lookup reads the latest stored sample for a condition.
//
// The storage error is passed through instead of being folded into "not found":
// "the database refused" and "the device never reported" lead an operator to
// completely different actions, and the evaluator reports them as different
// verdicts (error vs unknown).
func (r *Runtime) lookup(ctx context.Context) func(string, domain.SignalKind, string) (domain.Sample, bool, error) {
	return func(deviceID string, signal domain.SignalKind, key string) (domain.Sample, bool, error) {
		s, ok, err := r.store.LatestState(ctx, deviceID, signal, key)
		if err != nil {
			return domain.Sample{}, false, err
		}
		if !ok {
			return domain.Sample{}, false, nil
		}
		return s, true, nil
	}
}

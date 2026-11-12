// Package health decides whether the backend is ready to serve, for the
// host-side manager (tp-agent) and for the container healthcheck.
//
// Reports never carry error text, addresses or credentials: only a component
// name, a pass/fail flag and a short fixed reason code.
package health

import (
	"context"
	"fmt"
	"time"
)

// Reason codes. Keep them fixed so the Agent can match on them.
const (
	ReasonUnreachable      = "unreachable"
	ReasonTimeout          = "timeout"
	ReasonSchemaMismatch   = "schema_mismatch"
	ReasonSchemaUnreadable = "schema_unreadable"
	ReasonNotConfigured    = "not_configured"
)

// Check is the result of one component probe.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Report is the overall readiness.
type Report struct {
	Ready         bool    `json:"ready"`
	SchemaVersion int     `json:"schema_version"`
	Checks        []Check `json:"checks"`
}

// Probe checks one component. A non-empty reason with ok=false explains the failure.
type Probe struct {
	Name string
	Run  func(ctx context.Context) (ok bool, reason string)
}

// Run executes every probe, each under its own timeout, and never stops at
// the first failure, so the caller sees every broken component at once.
func Run(ctx context.Context, perProbe time.Duration, schemaVersion int, probes ...Probe) Report {
	report := Report{Ready: true, SchemaVersion: schemaVersion, Checks: make([]Check, 0, len(probes))}
	for _, p := range probes {
		c := Check{Name: p.Name}
		pctx, cancel := context.WithTimeout(ctx, perProbe)
		done := make(chan Check, 1)
		go func(p Probe) {
			defer func() {
				if recover() != nil {
					done <- Check{Name: p.Name, Reason: ReasonUnreachable}
				}
			}()
			ok, reason := p.Run(pctx)
			done <- Check{Name: p.Name, OK: ok, Reason: reason}
		}(p)
		select {
		case c = <-done:
		case <-pctx.Done():
			c = Check{Name: p.Name, Reason: ReasonTimeout}
		}
		cancel()
		if !c.OK {
			report.Ready = false
			if c.Reason == "" {
				c.Reason = ReasonUnreachable
			}
		}
		report.Checks = append(report.Checks, c)
	}
	return report
}

// SchemaProbe compares the ledger version read from the database with the
// version this program was built for.
func SchemaProbe(read func(ctx context.Context) (int, error), want int) Probe {
	return Probe{Name: "schema", Run: func(ctx context.Context) (bool, string) {
		got, err := read(ctx)
		if err != nil {
			return false, ReasonSchemaUnreadable
		}
		if got != want {
			return false, fmt.Sprintf("%s:have=%d,want=%d", ReasonSchemaMismatch, got, want)
		}
		return true, ""
	}}
}

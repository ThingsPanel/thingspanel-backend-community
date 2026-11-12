package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunReportsEveryFailureNotJustTheFirst(t *testing.T) {
	r := Run(context.Background(), time.Second, 25,
		Probe{Name: "database", Run: func(context.Context) (bool, string) { return false, ReasonUnreachable }},
		Probe{Name: "redis", Run: func(context.Context) (bool, string) { return false, ReasonUnreachable }},
		SchemaProbe(func(context.Context) (int, error) { return 25, nil }, 25),
	)
	if r.Ready || len(r.Checks) != 3 {
		t.Fatalf("report=%+v", r)
	}
	if r.Checks[0].OK || r.Checks[1].OK || !r.Checks[2].OK {
		t.Fatalf("checks=%+v", r.Checks)
	}
}

func TestRunAllGood(t *testing.T) {
	r := Run(context.Background(), time.Second, 25, Probe{Name: "database", Run: func(context.Context) (bool, string) { return true, "" }})
	if !r.Ready || r.SchemaVersion != 25 {
		t.Fatalf("report=%+v", r)
	}
}

func TestSlowProbeTimesOut(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	r := Run(context.Background(), 20*time.Millisecond, 1, Probe{Name: "redis", Run: func(context.Context) (bool, string) { <-block; return true, "" }})
	if r.Ready || r.Checks[0].Reason != ReasonTimeout {
		t.Fatalf("report=%+v", r)
	}
}

func TestPanickingProbeFailsClosed(t *testing.T) {
	r := Run(context.Background(), time.Second, 1, Probe{Name: "x", Run: func(context.Context) (bool, string) { panic("boom") }})
	if r.Ready || r.Checks[0].Reason != ReasonUnreachable {
		t.Fatalf("report=%+v", r)
	}
}

func TestSchemaMismatchAndUnreadable(t *testing.T) {
	r := Run(context.Background(), time.Second, 25, SchemaProbe(func(context.Context) (int, error) { return 24, nil }, 25))
	if r.Ready || !strings.HasPrefix(r.Checks[0].Reason, ReasonSchemaMismatch) {
		t.Fatalf("report=%+v", r)
	}
	r = Run(context.Background(), time.Second, 25, SchemaProbe(func(context.Context) (int, error) { return 0, errors.New("password=secret host=db") }, 25))
	if r.Ready || r.Checks[0].Reason != ReasonSchemaUnreadable {
		t.Fatalf("report=%+v", r)
	}
	if strings.Contains(r.Checks[0].Reason, "secret") {
		t.Fatal("error text leaked into the report")
	}
}

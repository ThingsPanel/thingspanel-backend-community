package brokercommands

import (
	"testing"
	"time"
)

func TestAcknowledgementDeadlineFromDispatch(t *testing.T) {
	start := time.Now().UTC()
	row := Row{DispatchedAt: &start, UpdatedAt: start.Add(time.Hour), CanonicalRequest: []byte(`{"ack_timeout_seconds":2}`)}
	deadline, explicit := acknowledgementDeadline(row, 30*time.Second)
	if !explicit || !deadline.Equal(start.Add(2*time.Second)) {
		t.Fatalf("deadline %v explicit %v", deadline, explicit)
	}
	row.Intent = "expected.command"
	row.CanonicalRequest = []byte(`{"Payload":"{\"method\":\"set\",\"ack_timeout_seconds\":300}"}`)
	deadline, explicit = acknowledgementDeadline(row, time.Second)
	if !explicit || !deadline.Equal(start.Add(300*time.Second)) {
		t.Fatalf("queued deadline %v explicit %v", deadline, explicit)
	}
	for _, raw := range []string{`{"ack_timeout_seconds":0}`, `{"ack_timeout_seconds":301}`, `{"ack_timeout_seconds":"2"}`} {
		row.CanonicalRequest = []byte(raw)
		d, _ := acknowledgementDeadline(row, time.Second)
		if !d.IsZero() {
			t.Fatalf("accepted invalid %s", raw)
		}
	}
}

func TestExpectedNonCommandTimeoutKeyIsBusinessData(t *testing.T) {
	now := time.Now().UTC()
	for _, intent := range []string{"expected.telemetry", "expected.attribute"} {
		row := Row{Intent: intent, UpdatedAt: now, CanonicalRequest: []byte(`{"Payload":"{\"ack_timeout_seconds\":2}"}`)}
		deadline, explicit := acknowledgementDeadline(row, 30*time.Second)
		if explicit || !deadline.Equal(now.Add(30*time.Second)) {
			t.Fatalf("%s business data treated as control metadata", intent)
		}
	}
}

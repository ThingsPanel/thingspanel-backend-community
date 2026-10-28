package mqttadapter

import (
	"context"
	"errors"
	"testing"

	bc "project/internal/brokercontract"
	"project/internal/brokerledger"
	"project/internal/uplink"
	"project/pkg/global"
)

// A missing command database is transient. It must not be mistaken for a
// permanent identity/scope denial and sent to quarantine.
func TestCommandResponseScopeDatabaseFailureIsTransient(t *testing.T) {
	old := global.DB
	global.DB = nil
	t.Cleanup(func() { global.DB = old })

	a := NewAdapter(nil, nil, nil)
	_, err := a.commandResponseScope("tenant", "device", "command-id", 1)
	if err == nil {
		t.Fatal("expected unavailable database error")
	}
	if errors.Is(err, bc.ErrUnauthorized) {
		t.Fatalf("database failure was classified as permanent unauthorized: %v", err)
	}
}

type quarantineTestAcceptor struct {
	rejectCalls int
	acceptCalls int
	firstErr    error
	records     []brokerledger.Rejection
}

func (a *quarantineTestAcceptor) Accept(context.Context, *uplink.DeviceMessage) error {
	a.acceptCalls++
	return nil
}

func (a *quarantineTestAcceptor) Reject(_ context.Context, r brokerledger.Rejection) error {
	a.rejectCalls++
	a.records = append(a.records, r)
	if a.firstErr != nil {
		err := a.firstErr
		a.firstErr = nil
		return err
	}
	return nil
}

func TestQuarantineMustCommitBeforeManualAckAndDoesNotAcceptBusiness(t *testing.T) {
	a := NewAdapter(nil, nil, nil)
	acceptor := &quarantineTestAcceptor{firstErr: errors.New("quarantine database unavailable")}
	a.SetIngressAcceptor(acceptor)
	m := &ackBoundaryMessage{}
	attempts := 0
	a.consumeTrustedMessage(m, func() error {
		attempts++
		err := a.rejectTrustedIngress("$tp/ingress/v1/t/d/k", []byte{0, 1, 2}, bc.Envelope{
			DeviceID: "d",
			Metadata: bc.Metadata{TenantID: "t", BrokerCell: "cell", RouteEpoch: 3},
		}, "command_ack_scope_denied", bc.ErrUnauthorized)
		if attempts == 1 {
			if m.acked != 0 {
				t.Fatal("delivery acknowledged before quarantine transaction committed")
			}
			if err == nil || !errors.Is(err, bc.ErrUnauthorized) {
				t.Fatalf("failed quarantine should preserve the original denial and retry: %v", err)
			}
			return err
		}
		return err
	})
	if attempts != 2 || acceptor.rejectCalls != 2 || m.acked != 1 {
		t.Fatalf("attempts=%d rejects=%d ack=%d", attempts, acceptor.rejectCalls, m.acked)
	}
	if acceptor.acceptCalls != 0 {
		t.Fatalf("quarantined ACK entered business acceptance %d times", acceptor.acceptCalls)
	}
	for _, r := range acceptor.records {
		if r.Reason != "command_ack_scope_denied" || r.TenantID != "t" || r.DeviceID != "d" || r.BrokerCell != "cell" || r.RouteEpoch != 3 {
			t.Fatalf("unexpected trusted quarantine record: %+v", r)
		}
	}
}

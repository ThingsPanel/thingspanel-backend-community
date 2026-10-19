package mqttadapter

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	bc "project/internal/brokercontract"
	"project/internal/brokerledger"
	"project/internal/uplink"
)

type permanentConflictAcceptor struct {
	mu          sync.Mutex
	acceptCalls int
	rejectCalls int
	firstErr    error
	seen        map[string]int
	onReject    func()
}

func (a *permanentConflictAcceptor) Accept(context.Context, *uplink.DeviceMessage) error {
	a.mu.Lock()
	a.acceptCalls++
	a.mu.Unlock()
	return brokerledger.ErrConflict
}

func (a *permanentConflictAcceptor) Reject(_ context.Context, r brokerledger.Rejection) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejectCalls++
	key := r.TenantID + "/" + r.DeviceID + "/" + r.Reason + "/" + string(r.Body)
	a.seen[key]++
	if a.onReject != nil {
		a.onReject()
	}
	if a.firstErr != nil {
		err := a.firstErr
		a.firstErr = nil
		return err
	}
	return nil
}

type capturedPublish struct {
	topic   string
	payload []byte
}

type captureMQTTClient struct {
	mqtt.Client
	mu        sync.Mutex
	published []capturedPublish
}

func (c *captureMQTTClient) Publish(topic string, _ byte, _ bool, payload interface{}) mqtt.Token {
	var body []byte
	switch v := payload.(type) {
	case []byte:
		body = append([]byte(nil), v...)
	case string:
		body = []byte(v)
	}
	c.mu.Lock()
	c.published = append(c.published, capturedPublish{topic: topic, payload: body})
	c.mu.Unlock()
	return completedToken{}
}

type completedToken struct{ mqtt.Token }

func (completedToken) Wait() bool                     { return true }
func (completedToken) WaitTimeout(time.Duration) bool { return true }
func (completedToken) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (completedToken) Error() error                   { return nil }

type pendingToken struct {
	mqtt.Token
	waitCalls chan struct{}
	release   <-chan struct{}
}

func (t pendingToken) Wait() bool {
	select {
	case t.waitCalls <- struct{}{}:
	default:
	}
	<-t.release
	return true
}
func (t pendingToken) WaitTimeout(time.Duration) bool {
	select {
	case t.waitCalls <- struct{}{}:
	default:
	}
	select {
	case <-t.release:
		return true
	default:
		return false
	}
}
func (pendingToken) Done() <-chan struct{} { return make(chan struct{}) }
func (pendingToken) Error() error          { return nil }

type permanentIngressMessage struct {
	ackCount int
}

func (m *permanentIngressMessage) Duplicate() bool   { return false }
func (m *permanentIngressMessage) Qos() byte         { return 1 }
func (m *permanentIngressMessage) Retained() bool    { return false }
func (m *permanentIngressMessage) Topic() string     { return "$tp/ingress/v1/t/d/k" }
func (m *permanentIngressMessage) MessageID() uint16 { return 17 }
func (m *permanentIngressMessage) Payload() []byte   { return []byte(`{"event":1}`) }
func (m *permanentIngressMessage) Ack()              { m.ackCount++ }

// This follows HandleIngressMessage's reliable-acceptance branch with a stable
// source conflict, and checks that only committed quarantine produces a
// failure response and ACK. The repeated delivery models redelivery after the
// same source ID was permanently rejected.
func TestPermanentReceiptConflictQuarantinesBeforeFailureResponseAndAck(t *testing.T) {
	if got := permanentAcceptanceReason(brokerledger.ErrConflict); got != "source_event_id_conflict" {
		t.Fatalf("conflict quarantine reason=%q", got)
	}
	for _, transient := range []error{brokerledger.ErrTenantCapacity, brokerledger.ErrFenced, errors.New("database unavailable")} {
		if got := permanentAcceptanceReason(transient); got != "" {
			t.Fatalf("transient error %v classified permanent as %q", transient, got)
		}
	}

	client := &captureMQTTClient{}
	adapter := NewAdapter(nil, client, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.SetContext(ctx)
	acceptor := &permanentConflictAcceptor{firstErr: errors.New("quarantine transaction failed"), seen: make(map[string]int)}
	adapter.SetIngressAcceptor(acceptor)
	responseBaseline := 0
	acceptor.onReject = func() {
		client.mu.Lock()
		defer client.mu.Unlock()
		if len(client.published) != responseBaseline {
			t.Error("application response published before quarantine commit")
		}
	}
	env := bc.Envelope{DeviceID: "device-a", Metadata: bc.Metadata{TenantID: "tenant-a", BrokerCell: "cell-a", RouteEpoch: 4}}
	message := &permanentIngressMessage{}
	process := func() error {
		msg := &uplink.DeviceMessage{Type: "event", DeviceID: env.DeviceID, TenantID: env.Metadata.TenantID, Payload: message.Payload()}
		if err := acceptor.Accept(ctx, msg); err != nil {
			reason := permanentAcceptanceReason(err)
			if reason == "" {
				return err
			}
			if err = adapter.rejectTrustedIngress(message.Topic(), message.Payload(), env, reason, err); err != nil {
				return err
			}
			adapter.publishEventResponse("device-number-a", "event-42", "alarm", errors.New("business message rejected; see platform receipt diagnostics"))
			return nil
		}
		return nil
	}
	adapter.consumeTrustedMessage(message, process)
	if message.ackCount != 1 {
		t.Fatalf("delivery ACK count after quarantine recovered=%d, want 1", message.ackCount)
	}
	client.mu.Lock()
	if len(client.published) != 1 {
		client.mu.Unlock()
		t.Fatalf("published responses=%d, want only one failure response", len(client.published))
	}
	first := client.published[0]
	client.mu.Unlock()
	assertFailureResponse(t, first)

	// Broker redelivery or a duplicate application publish uses the same source
	// conflict. Durable quarantine is idempotent and does not leave it pending.
	responseBaseline = 1
	secondMessage := &permanentIngressMessage{}
	adapter.consumeTrustedMessage(secondMessage, process)
	if secondMessage.ackCount != 1 {
		t.Fatalf("duplicate conflict delivery ACK count=%d, want 1", secondMessage.ackCount)
	}
	acceptor.mu.Lock()
	defer acceptor.mu.Unlock()
	if acceptor.acceptCalls != 3 || acceptor.rejectCalls != 3 {
		t.Fatalf("accept calls=%d reject calls=%d, want retry plus duplicate", acceptor.acceptCalls, acceptor.rejectCalls)
	}
	for key, n := range acceptor.seen {
		if n != 3 {
			t.Fatalf("stable quarantine key recorded %d times: %q", n, key)
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.published) != 2 {
		t.Fatalf("response count=%d, want two failure responses for two acknowledged deliveries", len(client.published))
	}
	for _, response := range client.published {
		assertFailureResponse(t, response)
	}
}

func assertFailureResponse(t *testing.T, p capturedPublish) {
	t.Helper()
	if p.topic != "devices/event/response/device-number-a/event-42" {
		t.Fatalf("response topic=%q", p.topic)
	}
	var body map[string]any
	if err := json.Unmarshal(p.payload, &body); err != nil {
		t.Fatalf("invalid response JSON %q: %v", p.payload, err)
	}
	if body["result"] != float64(1) || body["message"] == "success" {
		t.Fatalf("permanent rejection emitted success response: %s", p.payload)
	}
}

func TestIngressResponseWaitUnblocksWhenAdapterContextIsCancelled(t *testing.T) {
	release := make(chan struct{})
	waits := make(chan struct{}, 1)
	token := pendingToken{waitCalls: waits, release: release}
	adapter := NewAdapter(nil, nil, nil)
	adapter.inputProfile = "ingress_v1"
	ctx, cancel := context.WithCancel(context.Background())
	adapter.SetContext(ctx)
	finished := make(chan error, 1)
	go func() { finished <- adapter.waitResponseToken(token) }()
	select {
	case <-waits:
	case <-time.After(time.Second):
		t.Fatal("response publisher did not start waiting")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled response wait error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ingress response wait did not unblock after cancellation")
	}
}

func TestLegacyResponseWaitRetainsTokenWaitPolicy(t *testing.T) {
	release := make(chan struct{})
	waits := make(chan struct{}, 1)
	token := pendingToken{waitCalls: waits, release: release}
	adapter := NewAdapter(nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	adapter.SetContext(ctx)
	finished := make(chan error, 1)
	go func() { finished <- adapter.waitResponseToken(token) }()
	select {
	case <-waits:
	case <-time.After(time.Second):
		t.Fatal("legacy response publisher did not start waiting")
	}
	cancel()
	select {
	case <-finished:
		t.Fatal("legacy response wait unexpectedly followed adapter cancellation")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("legacy token wait error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("legacy token wait did not finish after token release")
	}
}

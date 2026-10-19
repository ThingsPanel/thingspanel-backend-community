package mqttadapter

import (
	"context"
	"errors"
	"github.com/spf13/viper"
	bc "project/internal/brokercontract"
	"project/internal/brokermapper"
	"testing"
)

func TestIngressProfileMustBeExplicitAndComplete(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	a := NewAdapter(nil, nil, nil)
	if e := a.ConfigureIngressFromConfig(); e != nil || a.inputProfile != "legacy" {
		t.Fatal("legacy compatibility")
	}
	viper.Set("mqtt.input_profile", "ingress_v1")
	if e := a.ConfigureIngressFromConfig(); e == nil {
		t.Fatal("accepted untrusted broker")
	}
	viper.Set("mqtt.broker_cell", "c")
	viper.Set("mqtt.broker_instance", "i")
	a.mapperRepo = brokermapper.NewRepository(nil) // built-in standard snapshot needs no database
	if e := a.ConfigureIngressFromConfig(); e != nil {
		t.Fatal(e)
	}
	viper.Set("mqtt.retained_profile", "wire-retained")
	if e := a.ConfigureIngressFromConfig(); e == nil {
		t.Fatal("uncertified wire retained")
	}
}
func TestPlatformRetainedClearHasNoBusinessEffect(t *testing.T) {
	a := NewAdapter(nil, nil, nil)
	a.inputProfile = "ingress_v1"
	a.retainedProfile = bc.RetainedPlatform
	topic, e := bc.IngressTopic("tenant", "device", []byte("devices/telemetry"))
	if e != nil {
		t.Fatal(e)
	}
	// Nil bus/cache/DB proves the empty clear takes no business path, even RAP=0.
	if e = a.HandleIngressMessage(topic, []byte{}, false); e != nil {
		t.Fatal(e)
	}
	a.retainedProfile = bc.RetainedLiveIngress
	if e = a.HandleIngressMessage(topic, nil, false); e == nil {
		t.Fatal("clear in live binding accepted")
	}
}
func TestIngressBusinessFamiliesAreExact(t *testing.T) {
	for _, tt := range []struct{ topic, kind string }{{"devices/telemetry", "telemetry"}, {"devices/event/e1", "event"}, {"gateway/attributes/a1", "gateway_attribute"}, {"devices/command/response/c1", "command_response"}, {"gateway/attributes/set/response/a1", "gateway_attribute_set_response"}} {
		k, _, e := classifyIngressTopic(tt.topic)
		if e != nil || k != tt.kind {
			t.Fatalf("%s: %s %v", tt.topic, k, e)
		}
	}
	for _, topic := range []string{"devices/command/a/b", "devices/status/a", "devices/event/", "devices/command/response/c1/extra", "$tp/state/v1/a/b"} {
		if _, _, e := classifyIngressTopic(topic); e == nil {
			t.Fatalf("unsafe family %s", topic)
		}
	}
}

type ackBoundaryMessage struct{ acked int }

func (m *ackBoundaryMessage) Duplicate() bool   { return false }
func (m *ackBoundaryMessage) Qos() byte         { return 1 }
func (m *ackBoundaryMessage) Retained() bool    { return false }
func (m *ackBoundaryMessage) Topic() string     { return "$tp/ingress/v1/test" }
func (m *ackBoundaryMessage) MessageID() uint16 { return 1 }
func (m *ackBoundaryMessage) Payload() []byte   { return nil }
func (m *ackBoundaryMessage) Ack()              { m.acked++ }
func TestManualAckWaitsForAcceptedBoundary(t *testing.T) {
	a := NewAdapter(nil, nil, nil)
	m := &ackBoundaryMessage{}
	attempt := 0
	a.consumeTrustedMessage(m, func() error {
		attempt++
		if m.acked != 0 {
			t.Fatal("ack before successful acceptance")
		}
		if attempt == 1 {
			return errors.New("database unavailable")
		}
		return nil
	})
	if attempt != 2 || m.acked != 1 {
		t.Fatalf("attempt=%d ack=%d", attempt, m.acked)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.SetContext(ctx)
	cancel()
	m = &ackBoundaryMessage{}
	a.consumeTrustedMessage(m, func() error { t.Fatal("cancelled callback dispatched"); return nil })
	if m.acked != 0 {
		t.Fatal("cancelled delivery acknowledged")
	}
}

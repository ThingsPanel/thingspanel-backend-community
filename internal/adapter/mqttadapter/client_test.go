package mqttadapter

import (
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestConfigureSubscriptionSessionUsesFreshBrokerSession(t *testing.T) {
	opts := mqtt.NewClientOptions().SetCleanSession(false).SetResumeSubs(true)

	configureSubscriptionSession(opts)

	if !opts.CleanSession || opts.ResumeSubs {
		t.Fatalf("unexpected MQTT session options: clean=%v resume=%v", opts.CleanSession, opts.ResumeSubs)
	}
}

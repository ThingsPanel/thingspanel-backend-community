package app

import (
	"testing"

	"github.com/spf13/viper"
)

func TestWithConfigPreservesEnvironmentOnlyMQTTIngressSettings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	config := viper.New()
	config.Set("mqtt.broker", "tcp://example.invalid:1883")
	settings := map[string]string{
		"mqtt.input_profile":   "ingress_v1",
		"mqtt.broker_cell":     "community",
		"mqtt.broker_instance": "community-mosquitto",
	}
	t.Setenv("GOTP_MQTT_INPUT_PROFILE", settings["mqtt.input_profile"])
	t.Setenv("GOTP_MQTT_BROKER_CELL", settings["mqtt.broker_cell"])
	t.Setenv("GOTP_MQTT_BROKER_INSTANCE", settings["mqtt.broker_instance"])
	if err := WithConfig(config)(&Application{}); err != nil {
		t.Fatal(err)
	}
	for key, want := range settings {
		if got := viper.GetString(key); got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
}

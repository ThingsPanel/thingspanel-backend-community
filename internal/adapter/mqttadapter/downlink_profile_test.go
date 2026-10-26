package mqttadapter

import (
	"testing"

	"project/internal/downlink"
)

func TestIngressV1RejectsRawLegacyPublishBeforeClientAccess(t *testing.T) {
	a := &Adapter{inputProfile: "ingress_v1"}
	err := a.PublishMessage("device-number", downlink.MessageTypeCommand, "1", "", "legacy-id", 1, []byte(`{"on":true}`))
	if err == nil {
		t.Fatal("ingress_v1 raw legacy publish was accepted")
	}
}

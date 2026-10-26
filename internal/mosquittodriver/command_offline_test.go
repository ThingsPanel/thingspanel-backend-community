package mosquittodriver

import (
	"testing"
	"time"
)

func TestOfflineCommandQueueAdmissionDoesNotAuthorizeOutput(t *testing.T) {
	d := commandDriver(t, testGrant())
	frame := testDeliveryFrame(t, time.Now())
	topic := "devices/command/device-a/cmd-1"
	if !d.AuthorizePayload(99, 1, topic, frame, 1, false) {
		t.Fatal("valid frame was denied as an offline queue candidate")
	}
	if _, _, err := d.Outgoing(99, topic, frame, 1, false); err == nil {
		t.Fatal("offline queue admission authorized output without a lease")
	}
	d.Closed(1)
	if !d.AuthorizePayload(1, 1, topic, frame, 1, false) {
		t.Fatal("valid frame was denied for a closed-session queue candidate")
	}
	if _, _, err := d.Outgoing(1, topic, frame, 1, false); err == nil {
		t.Fatal("closed-session queue admission authorized output")
	}
	if d.AuthorizePayload(99, 1, topic, []byte("plain"), 1, false) {
		t.Fatal("unframed command was admitted into an offline queue")
	}
}

func TestDeviceWillCannotInjectCommandFrameOrTopic(t *testing.T) {
	d := commandDriver(t, testGrant())
	d.Closed(1)
	frame := testDeliveryFrame(t, time.Now())
	if _, _, _, err := d.RewriteWill(1, "devices/event/will", frame, 1, false); err == nil {
		t.Fatal("device Will injected a private command frame")
	}
	if _, _, _, err := d.RewriteWill(1, "devices/command/device-a/cmd-1", []byte("forged"), 1, false); err == nil {
		t.Fatal("device Will published to canonical command topic")
	}
}

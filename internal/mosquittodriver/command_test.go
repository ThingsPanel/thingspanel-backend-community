package mosquittodriver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	bc "project/internal/brokercontract"
)

func commandDriver(t *testing.T, grant bc.Grant) *Driver {
	t.Helper()
	d := newTestDriver(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(grant)
	}, 2)
	if !d.Begin(1, "client-a", "user", "password", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatal("command test authentication failed")
	}
	d.Connected(1)
	return d
}

func testDeliveryFrame(t *testing.T, now time.Time) []byte {
	t.Helper()
	b, err := bc.EncodeCommandFrame(bc.CommandFrame{
		CommandID: "cmd-1", TenantID: "tenant-a", TargetDeviceID: "device-a", RouteEpoch: 1,
		MappingVersion: "standard-1", WireTopic: "devices/command/device-a/cmd-1",
		ExpiresAt: now.Add(time.Minute).UnixMilli(), Payload: []byte{0, 0xff, '{', '}', 0x80},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCommandDeliveryChecksExpiryIdentityEpochMappingAndRetain(t *testing.T) {
	grant := testGrant()
	d := commandDriver(t, grant)
	now := time.Now()
	valid := testDeliveryFrame(t, now)
	topic := "devices/command/device-a/cmd-1"
	if !d.AuthorizePayload(1, 1, topic, valid, 1, false) {
		t.Fatal("valid command frame denied")
	}
	for name, mutate := range map[string]func(*bc.CommandFrame){
		"expired":          func(f *bc.CommandFrame) { f.ExpiresAt = now.Add(-time.Second).UnixMilli() },
		"wrong tenant":     func(f *bc.CommandFrame) { f.TenantID = "tenant-b" },
		"wrong target":     func(f *bc.CommandFrame) { f.TargetDeviceID = "device-b" },
		"wrong epoch":      func(f *bc.CommandFrame) { f.RouteEpoch++ },
		"wrong mapping":    func(f *bc.CommandFrame) { f.MappingVersion = "mapping-other" },
		"wrong wire topic": func(f *bc.CommandFrame) { f.WireTopic = "devices/command/device-a/other" },
	} {
		t.Run(name, func(t *testing.T) {
			f, err := bc.ParseCommandFrame(valid, now)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&f)
			bad, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			bad = append([]byte("\x00TP-COMMAND/1\n"), bad...)
			if d.AuthorizePayload(1, 1, topic, bad, 1, false) {
				t.Fatal("invalid command delivery accepted")
			}
		})
	}
	if d.AuthorizePayload(1, 1, topic, valid, 1, true) {
		t.Fatal("retained command delivery accepted")
	}
	if d.AuthorizePayload(1, 2, topic, valid, 1, false) {
		t.Fatal("command frame accepted as client publish")
	}
}

func TestOutgoingStripsFrameOnlyFromDeliveryAndPreservesStoredBytes(t *testing.T) {
	d := commandDriver(t, testGrant())
	stored := testDeliveryFrame(t, time.Now())
	before := bytes.Clone(stored)
	wire, changed, err := d.Outgoing(1, "devices/command/device-a/cmd-1", stored, 1, false)
	if err != nil || !changed {
		t.Fatalf("outgoing command denied: changed=%v err=%v", changed, err)
	}
	if !bytes.Equal(stored, before) || !bc.IsCommandFrame(stored) {
		t.Fatal("delivery transform modified broker storage frame")
	}
	if !bytes.Equal(wire, []byte{0, 0xff, '{', '}', 0x80}) || bc.IsCommandFrame(wire) {
		t.Fatalf("device wire payload was not restored exactly: %v", wire)
	}
	plain := []byte("ordinary")
	if _, transformed, err := d.Outgoing(1, "devices/command/device-a/cmd-1", plain, 1, false); err == nil || transformed {
		t.Fatal("unframed canonical command was allowed from queued storage")
	}
	if out, transformed, err := d.Outgoing(1, "devices/event/device-a/event-1", plain, 1, false); err != nil || transformed || out != nil {
		t.Fatalf("ordinary non-command payload unexpectedly transformed: %q %v %v", out, transformed, err)
	}
}

func TestServiceRewriteRequiresLegalDurableCommandFrame(t *testing.T) {
	grant := testGrant()
	grant.Principal = bc.Principal{TenantID: "tenant-a", DeviceID: "service-a", Kind: bc.PrincipalService}
	grant.Policy.Publish = []string{"devices/command/device-a/+"}
	d := commandDriver(t, grant)
	topic := "devices/command/device-a/cmd-1"
	if _, _, _, err := d.Rewrite(1, topic, []byte(`{"value":1}`), 1, false); err == nil {
		t.Fatal("bare canonical command publish accepted")
	}
	frame := testDeliveryFrame(t, time.Now())
	gotTopic, gotPayload, retain, err := d.Rewrite(1, topic, frame, 1, false)
	if err != nil || gotTopic != topic || !bytes.Equal(gotPayload, frame) || retain {
		t.Fatalf("legal durable frame rewrite mismatch: %q %v %v %v", gotTopic, gotPayload, retain, err)
	}
	if _, _, _, err = d.Rewrite(1, topic, frame, 1, true); err == nil {
		t.Fatal("retained command frame accepted")
	}
}

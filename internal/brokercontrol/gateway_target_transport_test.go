package brokercontrol

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	bc "project/internal/brokercontract"
	"project/internal/mosquittodriver"
)

func TestTargetPublisherLeaseExpiryIsEnforcedByDriver(t *testing.T) {
	s := serviceFixture()
	s.TargetScopedPublisher = true
	s.TargetTenantScope = "platform"
	s.AllowedDeviceIDs = nil
	s.AllowedTenantIDs = nil
	row := DeviceRecord{ID: "target", TenantID: "tenant-new", DeviceNumber: "new-number", DeviceNumberCount: 1, IsEnabled: "enabled", ActivateFlag: "active"}
	repo := &fakeRepository{serviceTargets: map[string]DeviceRecord{row.ID: row}}
	o, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{s}, GrantTTL: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(o.Handler())
	t.Cleanup(srv.Close)
	d, err := mosquittodriver.New(mosquittodriver.Config{OwnerURL: srv.URL, Token: testToken(), Cell: "community", Instance: "node-1", Timeout: time.Second, MaxPending: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	req := serviceRequest()
	req.WireClientID, _ = bc.TargetPublisherClientID(s.AllowedClientIDs[0], row.ID)
	if !d.Begin(1, req.WireClientID, req.Username, req.Password, "127.0.0.1", 4) {
		t.Fatal("begin failed")
	}
	deadline := time.Now().Add(time.Second)
	connected := false
	for time.Now().Before(deadline) {
		if c := d.Poll(); c != nil {
			if !c.Allow {
				t.Fatal("owner denied")
			}
			d.Connected(1)
			connected = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !connected {
		t.Fatal("authentication timeout")
	}
	topic := "devices/event/response/new-number/evt-1"
	if !d.Authorize(1, 2, topic, 1, 1, false) {
		t.Fatal("current grant denied")
	}
	row.IsEnabled = "disabled"
	repo.serviceTargets[row.ID] = row
	if g, e := o.Authenticate(context.Background(), req); e != nil || g.Allow {
		t.Fatal("disabled target renewed")
	}
	// Scaled TTL exercises the actual deadline check; the owner's separate test
	// asserts the production maximum is 60 s. No claim of instantaneous revocation.
	time.Sleep(520 * time.Millisecond)
	if d.Authorize(1, 2, topic, 1, 1, false) {
		t.Fatal("expired target grant still publishes")
	}
}

// Exercises the real owner HTTP contract and driver receive gate. Repository
// records are isolated fixtures, not proof of a deployed gateway's wire behavior.
func TestGatewayTwoChildrenAndOtherGatewayCommandIsolation(t *testing.T) {
	newDriver := func(root string, children []DeviceRecord) *mosquittodriver.Driver {
		t.Helper()
		device := activeDevice()
		device.ID = root
		device.DeviceNumber = root + "-number"
		device.DeviceType = "2"
		repo := &fakeRepository{devices: []DeviceRecord{device}, children: children}
		o := testOwner(t, repo, nil)
		srv := httptest.NewServer(o.Handler())
		t.Cleanup(srv.Close)
		d, err := mosquittodriver.New(mosquittodriver.Config{OwnerURL: srv.URL, Token: testToken(), Cell: "community", Instance: "node-1", Timeout: time.Second, MaxPending: 4})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(d.Close)
		if !d.Begin(1, root+"-client", "fixture", "password", "127.0.0.1", 4) {
			t.Fatal("begin failed")
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if c := d.Poll(); c != nil {
				if !c.Allow {
					t.Fatal("owner denied gateway")
				}
				d.Connected(1)
				return d
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("owner timeout")
		return nil
	}
	child := func(id string) DeviceRecord {
		r := activeDevice()
		r.ID = id
		r.DeviceNumber = id + "-number"
		r.DeviceType = "3"
		return r
	}
	g1 := newDriver("g1", []DeviceRecord{child("s1"), child("s2")})
	g2 := newDriver("g2", []DeviceRecord{child("s3")})
	for _, id := range []string{"s1", "s2"} {
		filter := "gateway/command/" + id + "-number/+"
		if !g1.Authorize(1, 4, filter, 0, 1, false) || g2.Authorize(1, 4, filter, 0, 1, false) {
			t.Fatalf("child subscription isolation failed: %s", id)
		}
	}
	if g1.Authorize(1, 4, "gateway/command/s3-number/+", 0, 1, false) {
		t.Fatal("gateway obtained sibling gateway scope")
	}
	topic := "gateway/command/g1-number/cmd-1"
	frame, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "cmd-1", TenantID: "t1", TargetDeviceID: "g1", RouteEpoch: 1, MappingVersion: MappingVersion, WireTopic: topic, ExpiresAt: time.Now().Add(time.Minute).UnixMilli(), Payload: []byte(`{"sub_device_data":{"01":{"method":"switch","params":true}}}`)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !g1.AuthorizePayload(1, 1, topic, frame, 1, false) || g2.AuthorizePayload(1, 1, topic, frame, 1, false) {
		t.Fatal("gateway receive target crossed physical root")
	}
	if _, changed, e := g1.Outgoing(1, topic, frame, 1, false); e != nil || !changed {
		t.Fatalf("valid root command not delivered: %v", e)
	}
}

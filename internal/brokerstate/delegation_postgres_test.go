package brokerstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	bc "project/internal/brokercontract"
	"testing"
	"time"
)

func delegatedStateFixture(t *testing.T) (*Store, bc.StateObservation) {
	t.Helper()
	s := activityStore(t)
	for _, name := range []string{"016_plugin_context.sql", "017_delegated_routes.sql"} {
		body, e := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if e != nil {
			t.Fatal(e)
		}
		if e = s.db.Exec(string(body)).Error; e != nil {
			t.Fatal(e)
		}
	}
	if e := s.db.Exec(`INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online) VALUES('root','ta','enabled','active','root-number','{}',0);
 UPDATE devices SET parent_id='root' WHERE id='da';
 INSERT INTO broker_transport_bindings VALUES('ta','root','c',9,'active')`).Error; e != nil {
		t.Fatal(e)
	}
	target := bc.PluginTarget{TenantID: "ta", DeviceID: "da", DeviceNumber: "n-da", TopicPrefix: "plugin/a/", MappingVersion: "standard-v1", Kind: bc.PrincipalDevice, RouteEpoch: 1, TransportDeviceID: "root", TransportRouteEpoch: 9, BindingPhase: "active"}
	rootTarget := target
	rootTarget.DeviceID = target.TransportDeviceID
	rootTarget.DeviceNumber = "root-number"
	rootTarget.RouteEpoch = target.TransportRouteEpoch
	rootTarget.TransportDeviceID = rootTarget.DeviceID
	rootTarget.TransportRouteEpoch = rootTarget.RouteEpoch
	actor := bc.Principal{TenantID: "ta", DeviceID: "service", Kind: bc.PrincipalService}
	id, e := bc.PluginContextID(actor, "c", "i", "shared", []bc.PluginTarget{target, rootTarget})
	if e != nil {
		t.Fatal(e)
	}
	principal, _ := json.Marshal(actor)
	targets, _ := json.Marshal([]bc.PluginTarget{target, rootTarget})
	now := time.Now().UTC()
	if e := s.db.Exec(`INSERT INTO broker_plugin_contexts(id,principal_json,broker_cell,broker_instance,connection_id,targets_json,issued_at,expires_at) VALUES(?,?::jsonb,'c','i','shared',?::jsonb,?,?)`, id, string(principal), string(targets), now.Add(-time.Minute), now.Add(time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	o := observation("shared", 1, true)
	o.MappingVersion = "standard-v1"
	o.ObservedAt = now
	o.Delegation = &bc.Delegation{ContextID: id, Actor: actor, ConnectionID: "shared", BrokerCell: "c", BrokerInstance: "i", TargetDeviceID: "da", TargetRouteEpoch: 1, TransportDeviceID: "root", TransportRouteEpoch: 9, MappingVersion: "standard-v1", ObservedAt: now}
	return s, o
}

func TestDelegatedStateProjectionRechecksRootEpoch(t *testing.T) {
	s, o := delegatedStateFixture(t)
	ctx := context.Background()
	d, e := s.Apply(ctx, o)
	if e != nil || d.Status != "online" {
		t.Fatalf("valid shared plugin status: %+v %v", d, e)
	}
	if e := s.db.Exec(`UPDATE broker_transport_bindings SET route_epoch=10 WHERE device_id='root'`).Error; e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, o); !errors.Is(e, ErrFenced) {
		t.Fatalf("old root observation accepted: %v", e)
	}
	projected, e := s.ProjectCurrent(ctx, "ta", "da")
	if e != nil {
		t.Fatal(e)
	}
	if projected.Status != "unknown" || projected.Changed {
		t.Fatalf("old delegated observation projected after root migration: %+v", projected)
	}
}

func TestDelegatedStateAutomationRetainsBothEpochs(t *testing.T) {
	s, o := delegatedStateFixture(t)
	ctx := context.Background()
	if _, e := s.Apply(ctx, o); e != nil {
		t.Fatal(e)
	}
	projected, e := s.ProjectCurrent(ctx, "ta", "da")
	if e != nil || !projected.Changed || projected.Status != "online" {
		t.Fatalf("delegated state projection: %+v %v", projected, e)
	}
	var row struct {
		DeviceID   string
		RouteEpoch uint64
		Metadata   json.RawMessage
	}
	if e := s.db.Table("broker_ingress_receipts").Where("stream='state_transition'").Take(&row).Error; e != nil {
		t.Fatal(e)
	}
	if row.DeviceID != "root" || row.RouteEpoch != 9 {
		t.Fatalf("automation source lost transport route: %+v", row)
	}
	var meta map[string]any
	if e := json.Unmarshal(row.Metadata, &meta); e != nil {
		t.Fatal(e)
	}
	proof, e := bc.ParseDelegation(meta["delegated_route"])
	if e != nil || proof.TargetRouteEpoch != 1 || proof.TransportRouteEpoch != 9 {
		t.Fatalf("automation lost dual proof: %+v %v", proof, e)
	}
}

package storage

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	bc "project/internal/brokercontract"
	"testing"
	"time"
)

func delegatedTelemetryFixture(t *testing.T) (*telemetryBrokerFixture, *telemetryBatchItem) {
	t.Helper()
	f := newTelemetryBrokerFixture(t)
	if e := f.db.Exec(`ALTER TABLE devices ADD COLUMN activate_flag text NOT NULL DEFAULT 'active'`).Error; e != nil {
		t.Fatal(e)
	}
	f.insertDevice(t, "delegated-tenant", "root", "")
	f.insertDevice(t, "delegated-tenant", "target", "root")
	f.insertDevice(t, "delegated-tenant", "other", "root")
	f.insertDevice(t, "delegated-tenant", "leaf", "target")
	f.insertBinding(t, "delegated-tenant", "root", "cell", 9, "active")
	f.insertBinding(t, "delegated-tenant", "target", "cell", 3, "active")
	actor := bc.Principal{TenantID: "delegated-tenant", DeviceID: "shared-service", Kind: bc.PrincipalService}
	target := bc.PluginTarget{TenantID: "delegated-tenant", DeviceID: "target", DeviceNumber: "target", TopicPrefix: "plugin/a/", MappingVersion: "standard-v1", Kind: bc.PrincipalGateway, RouteEpoch: 3, BindingPhase: "active", TransportDeviceID: "root", TransportRouteEpoch: 9}
	root := target
	root.DeviceID = "root"
	root.DeviceNumber = "root"
	root.RouteEpoch = 9
	root.TransportDeviceID = "root"
	root.TransportRouteEpoch = 9
	id, e := bc.PluginContextID(actor, "cell", "instance", "connection", []bc.PluginTarget{target, root})
	if e != nil {
		t.Fatal(e)
	}
	principal, _ := json.Marshal(actor)
	targets, _ := json.Marshal([]bc.PluginTarget{target, root})
	now := time.Now().UTC()
	if e := f.db.Exec(`INSERT INTO broker_plugin_contexts(id,principal_json,broker_cell,broker_instance,connection_id,targets_json,issued_at,expires_at) VALUES(?,?::jsonb,'cell','instance','connection',?::jsonb,?,?)`, id, string(principal), string(targets), now.Add(-time.Minute), now.Add(time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	item := telemetryTrustedItem("delegated-tenant", "leaf", "root", uuid.NewString(), "cell", 9, now.Add(-time.Second).UnixMilli(), "temperature", 21.5)
	item.route.Delegation = &bc.Delegation{ContextID: id, Actor: actor, ConnectionID: "connection", BrokerCell: "cell", BrokerInstance: "instance", TargetDeviceID: "target", TargetRouteEpoch: 3, TransportDeviceID: "root", TransportRouteEpoch: 9, MappingVersion: "standard-v1", ObservedAt: item.route.ObservedAt}
	return f, item
}

func TestDelegatedTelemetryFinalBatchFencesTargetTransportActorAndSubtree(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		forge     bool
	}{
		{name: "current"},
		{name: "target migration", sql: `UPDATE broker_transport_bindings SET route_epoch=4 WHERE device_id='target'`},
		{name: "root migration", sql: `UPDATE broker_transport_bindings SET route_epoch=10 WHERE device_id='root'`},
		{name: "same root different subtree", sql: `UPDATE devices SET parent_id='other' WHERE id='leaf'`},
		{name: "disabled root", sql: `UPDATE devices SET is_enabled='disabled' WHERE id='root'`},
		{name: "forged actor", forge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, item := delegatedTelemetryFixture(t)
			if tc.sql != "" {
				if e := f.db.Exec(tc.sql).Error; e != nil {
					t.Fatal(e)
				}
			}
			if tc.forge {
				item.route.Delegation.Actor.DeviceID = "unlisted-service"
			}
			accepted, written, failed, e := telemetryBrokerWriter(f).commitTrustedTelemetry([]*telemetryBatchItem{item})
			if e != nil {
				t.Fatal(e)
			}
			want := 0
			if tc.name == "current" {
				want = 1
			}
			if len(accepted) != want || written != want || failed != 1-want {
				t.Fatalf("incorrect fenced batch outcome: accepted=%d written=%d failed=%d", len(accepted), written, failed)
			}
			for _, table := range []string{"telemetry_datas", "telemetry_current_datas", "broker_ingress_receipts", "broker_ingress_effects", "broker_telemetry_activity", "broker_state_projection_queue"} {
				assertTelemetryCount(t, f.db, table, "1=1", want)
			}
			if want == 1 {
				assertTelemetryCount(t, f.db, "broker_ingress_receipts", `metadata->'delegated_route'->>'target_device_id'='target' AND metadata->'delegated_route'->>'target_route_epoch'='3' AND metadata->'delegated_route'->>'transport_route_epoch'='9'`, 1)
			}
		})
	}
}

func TestDelegatedTelemetryTimescaleHypertableFencesIndependentTargetEpoch(t *testing.T) {
	if os.Getenv("TEST_BROKER_TIMESCALE") != "1" {
		t.Skip("set TEST_BROKER_TIMESCALE=1 for isolated TimescaleDB acceptance")
	}
	f, item := delegatedTelemetryFixture(t)
	if e := f.db.Exec(`CREATE EXTENSION IF NOT EXISTS timescaledb WITH SCHEMA public`).Error; e != nil {
		t.Fatal(e)
	}
	if e := f.db.Exec(`SELECT public.create_hypertable('telemetry_datas','ts',chunk_time_interval=>86400000::bigint,if_not_exists=>true)`).Error; e != nil {
		t.Fatal(e)
	}
	w := telemetryBrokerWriter(f)
	if _, written, failed, e := w.commitTrustedTelemetry([]*telemetryBatchItem{item}); e != nil || written != 1 || failed != 0 {
		t.Fatalf("hypertable commit failed: written=%d failed=%d err=%v", written, failed, e)
	}
	if e := f.db.Exec(`UPDATE broker_transport_bindings SET route_epoch=4 WHERE device_id='target'`).Error; e != nil {
		t.Fatal(e)
	}
	item.route.ReceiptID = uuid.NewString()
	if _, written, failed, e := w.commitTrustedTelemetry([]*telemetryBatchItem{item}); e != nil || written != 0 || failed != 1 {
		t.Fatalf("stale target reached hypertable: written=%d failed=%d err=%v", written, failed, e)
	}
	for _, table := range []string{"telemetry_datas", "telemetry_current_datas", "broker_ingress_receipts", "broker_ingress_effects", "broker_telemetry_activity"} {
		assertTelemetryCount(t, f.db, table, "1=1", 1)
	}
}

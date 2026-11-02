package uplink

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	bc "project/internal/brokercontract"
	"project/internal/brokerledger"
	"project/internal/model"
	"project/internal/storage"
	"testing"
	"time"
)

func TestDelegatedGatewayExpansionLocksChildThroughReceiptCommit(t *testing.T) {
	db, ledger := reliableFixture(t)
	seedReliableDevice(t, db, "gateway-a", "tenant-a")
	seedReliableDevice(t, db, "gateway-b", "tenant-a")
	parent, addr := "gateway-a", "child-address"
	if e := db.Create(&model.Device{ID: "child-a", TenantID: "tenant-a", DeviceNumber: "child-number", IsEnabled: "enabled", ActivateFlag: "active", ParentID: &parent, SubDeviceAddr: &addr, Voucher: `{}`}).Error; e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "016_plugin_context.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Exec(string(migration)).Error; e != nil {
		t.Fatal(e)
	}
	actor := bc.Principal{TenantID: "tenant-a", DeviceID: "service-a", Kind: bc.PrincipalService}
	target := bc.PluginTarget{TenantID: "tenant-a", DeviceID: "gateway-a", DeviceNumber: "gateway-a", TopicPrefix: "plugin/a/", MappingVersion: "standard-v1", Kind: bc.PrincipalGateway, RouteEpoch: 1, BindingPhase: "active"}
	id, e := bc.PluginContextID(actor, "community", "instance", "connection", []bc.PluginTarget{target})
	if e != nil {
		t.Fatal(e)
	}
	principal, _ := json.Marshal(actor)
	targets, _ := json.Marshal([]bc.PluginTarget{target})
	now := time.Now().UTC()
	if e := db.Exec(`INSERT INTO broker_plugin_contexts(id,principal_json,broker_cell,broker_instance,connection_id,targets_json,issued_at,expires_at) VALUES(?,?::jsonb,'community','instance','connection',?::jsonb,?,?)`, id, string(principal), string(targets), now.Add(-time.Minute), now.Add(time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	proof := &bc.Delegation{ContextID: id, Actor: actor, ConnectionID: "connection", BrokerCell: "community", BrokerInstance: "instance", TargetDeviceID: "gateway-a", TargetRouteEpoch: 1, TransportDeviceID: "gateway-a", TransportRouteEpoch: 1, MappingVersion: "standard-v1", ObservedAt: now}
	msg := reliableMessage("gateway-a", "tenant-a", "gateway_event", []byte(`{"sub_device_data":{"child-address":{"method":"alarm","params":{"value":1}}}}`))
	msg.Metadata["delegated_route"] = proof
	if _, _, e := ledger.Accept(context.Background(), msg); e != nil {
		t.Fatal(e)
	}
	processor := NewReliableProcessor(logrus.New())
	worked, e := ledger.ProcessOne(context.Background(), func(ctx context.Context, tx *gorm.DB, accepted brokerledger.Message) error {
		if e := processor.Process(ctx, tx, accepted); e != nil {
			return e
		}
		deadline, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		if e := db.WithContext(deadline).Exec(`UPDATE devices SET parent_id='gateway-b' WHERE id='child-a'`).Error; e == nil {
			return errors.New("child reparented before delegated receipt commit")
		}
		return nil
	})
	if e != nil || !worked {
		t.Fatalf("delegated expansion failed: worked=%v err=%v", worked, e)
	}
	var count int64
	if e := db.Model(&storage.EventDataModel{}).Where("device_id='child-a'").Count(&count).Error; e != nil || count != 1 {
		t.Fatalf("receipt did not atomically commit its event: count=%d err=%v", count, e)
	}
	if e := db.Exec(`UPDATE devices SET parent_id='gateway-b' WHERE id='child-a'`).Error; e != nil {
		t.Fatal(e)
	}
}

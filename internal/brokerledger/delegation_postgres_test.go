package brokerledger

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	bc "project/internal/brokercontract"
	"testing"
	"time"
)

func TestDelegatedPendingReceiptFencesIndependentTargetOrRootMigration(t *testing.T) {
	for _, changed := range []string{"target", "root"} {
		t.Run(changed, func(t *testing.T) {
			store, db := postgresStore(t, Limits{})
			if e := db.Exec(`CREATE TABLE devices(id text PRIMARY KEY,tenant_id text,parent_id text,is_enabled text,activate_flag text);
 INSERT INTO devices VALUES('root','tenant',NULL,'enabled','active'),('target','tenant','root','enabled','active')`).Error; e != nil {
				t.Fatal(e)
			}
			insertBinding(t, db, "tenant", "root", "cell", 9, "active")
			insertBinding(t, db, "tenant", "target", "cell", 3, "active")
			migration, e := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "016_plugin_context.sql"))
			if e != nil {
				t.Fatal(e)
			}
			if e = db.Exec(string(migration)).Error; e != nil {
				t.Fatal(e)
			}
			actor := bc.Principal{TenantID: "tenant", DeviceID: "service", Kind: bc.PrincipalService}
			target := bc.PluginTarget{TenantID: "tenant", DeviceID: "target", DeviceNumber: "target-number", TopicPrefix: "plugin/a/", MappingVersion: "standard-v1", Kind: bc.PrincipalDevice, RouteEpoch: 3, BindingPhase: "active", TransportDeviceID: "root", TransportRouteEpoch: 9}
			root := target
			root.DeviceID = "root"
			root.DeviceNumber = "root-number"
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
			if e := db.Exec(`INSERT INTO broker_plugin_contexts(id,principal_json,broker_cell,broker_instance,connection_id,targets_json,issued_at,expires_at) VALUES(?,?::jsonb,'cell','instance','connection',?::jsonb,?,?)`, id, string(principal), string(targets), now.Add(-time.Minute), now.Add(time.Minute)).Error; e != nil {
				t.Fatal(e)
			}
			proof := &bc.Delegation{ContextID: id, Actor: actor, ConnectionID: "connection", BrokerCell: "cell", BrokerInstance: "instance", TargetDeviceID: "target", TargetRouteEpoch: 3, TransportDeviceID: "root", TransportRouteEpoch: 9, MappingVersion: "standard-v1", ObservedAt: now}
			receipt, _, e := store.Accept(context.Background(), Message{TenantID: "tenant", DeviceID: "target", Stream: "event", BrokerCell: "cell", RouteEpoch: 3, Payload: []byte(`{"method":"event"}`), Metadata: map[string]any{"delegated_route": proof}})
			if e != nil {
				t.Fatal(e)
			}
			if e := db.Exec(`UPDATE broker_transport_bindings SET route_epoch=route_epoch+1 WHERE device_id=?`, changed).Error; e != nil {
				t.Fatal(e)
			}
			called := false
			processed, e := store.ProcessOne(context.Background(), func(context.Context, *gorm.DB, Message) error { called = true; return nil })
			if e != nil || !processed || called {
				t.Fatalf("migrated receipt reached business handler: processed=%v called=%v err=%v", processed, called, e)
			}
			var state string
			if e := db.Table("broker_ingress_receipts").Select("state").Where("id=?", receipt).Scan(&state).Error; e != nil {
				t.Fatal(e)
			}
			if state != stateFenced {
				t.Fatalf("migrated receipt state=%s", state)
			}
		})
	}
}

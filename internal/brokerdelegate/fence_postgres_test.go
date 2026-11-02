package brokerdelegate

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	bc "project/internal/brokercontract"
	"project/internal/brokerroute"
)

func delegationFixture(t *testing.T) (*gorm.DB, *bc.Delegation) {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated PostgreSQL dependency")
	}
	schema := "delegation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal("create isolated schema")
	}
	scoped := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal(e)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scoped = u.String()
	}
	db, err := gorm.Open(postgres.Open(scoped), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated test schema")
	}
	t.Cleanup(func() {
		if pool, e := db.DB(); e == nil {
			pool.Close()
		}
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		if pool, e := admin.DB(); e == nil {
			pool.Close()
		}
	})
	if err := db.Exec(`CREATE TABLE devices(id text PRIMARY KEY,tenant_id text,parent_id text,is_enabled text,activate_flag text);
 INSERT INTO devices VALUES('root','t',NULL,'enabled','active'),('child','t','root','enabled','active'),('other','other',NULL,'enabled','active');
 CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id));
 INSERT INTO broker_transport_bindings VALUES('t','root','cell',9,'active'),('t','child','cell',3,'active');
 CREATE TABLE broker_plugin_contexts(id text PRIMARY KEY,principal_json jsonb,broker_cell text,broker_instance text,connection_id text,targets_json jsonb,issued_at timestamptz,expires_at timestamptz);
 CREATE TABLE committed_effects(id text PRIMARY KEY);`).Error; err != nil {
		t.Fatal("create isolated fixture")
	}
	target := bc.PluginTarget{TenantID: "t", DeviceID: "child", DeviceNumber: "number", TopicPrefix: "plugin/test/", MappingVersion: "standard-v1", Kind: bc.PrincipalDevice, RouteEpoch: 3, BindingPhase: "active", TransportDeviceID: "root", TransportRouteEpoch: 9}
	rootTarget := target
	rootTarget.DeviceID = target.TransportDeviceID
	rootTarget.DeviceNumber = "root-number"
	rootTarget.RouteEpoch = target.TransportRouteEpoch
	rootTarget.TransportDeviceID = rootTarget.DeviceID
	rootTarget.TransportRouteEpoch = rootTarget.RouteEpoch
	actor := bc.Principal{TenantID: "t", DeviceID: "service", Kind: bc.PrincipalService}
	id, err := bc.PluginContextID(actor, "cell", "instance", "connection", []bc.PluginTarget{target, rootTarget})
	if err != nil {
		t.Fatal(err)
	}
	principal, _ := json.Marshal(actor)
	targets, _ := json.Marshal([]bc.PluginTarget{target, rootTarget})
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO broker_plugin_contexts VALUES(?,?::jsonb,'cell','instance','connection',?::jsonb,?,?)`, id, string(principal), string(targets), now.Add(-time.Minute), now.Add(time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	return db, &bc.Delegation{ContextID: id, Actor: actor, ConnectionID: "connection", BrokerCell: "cell", BrokerInstance: "instance", TargetDeviceID: "child", TargetRouteEpoch: 3, TransportDeviceID: "root", TransportRouteEpoch: 9, MappingVersion: "standard-v1", ObservedAt: now}
}

func TestDelegatedFenceRejectsEitherMigrationAndChangedOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		mutate    func(*bc.Delegation)
	}{
		{name: "current"},
		{name: "target migration", sql: `UPDATE broker_transport_bindings SET route_epoch=4 WHERE device_id='child'`},
		{name: "transport migration", sql: `UPDATE broker_transport_bindings SET route_epoch=10 WHERE device_id='root'`},
		{name: "wrong cell", sql: `UPDATE broker_transport_bindings SET broker_cell='elsewhere' WHERE device_id='root'`},
		{name: "disabled ancestor", sql: `UPDATE devices SET is_enabled='disabled' WHERE id='root'`},
		{name: "inactive target", sql: `UPDATE devices SET activate_flag='inactive' WHERE id='child'`},
		{name: "cross tenant reparent", sql: `UPDATE devices SET parent_id='other' WHERE id='child'`},
		{name: "cycle", sql: `UPDATE devices SET parent_id='child' WHERE id='root'`},
		{name: "actor substitution", mutate: func(d *bc.Delegation) { d.Actor.DeviceID = "other-service" }},
		{name: "target substitution", mutate: func(d *bc.Delegation) { d.TargetDeviceID = "unauthorized-device"; d.TargetRouteEpoch = 9 }},
		{name: "expired observation", mutate: func(d *bc.Delegation) { d.ObservedAt = d.ObservedAt.Add(2 * time.Minute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, proof := delegationFixture(t)
			if tc.sql != "" {
				if err := db.Exec(tc.sql).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.mutate != nil {
				tc.mutate(proof)
			}
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := Lock(tx, "t", proof, false); err != nil {
					return err
				}
				return tx.Exec(`INSERT INTO committed_effects VALUES('effect')`).Error
			})
			if tc.name == "current" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, brokerroute.ErrFenced) {
				t.Fatalf("scope was not fenced: %v", err)
			}
			var count int64
			if e := db.Table("committed_effects").Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			if tc.name != "current" && count != 0 {
				t.Fatal("fenced effect committed")
			}
		})
	}
}

func TestDelegatedFenceHoldsTransportMigrationUntilEffectCommit(t *testing.T) {
	db, proof := delegationFixture(t)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := Lock(tx, "t", proof, false); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		// A competing connection must not migrate the root through the held fence.
		err := db.WithContext(ctx).Exec(`UPDATE broker_transport_bindings SET route_epoch=10 WHERE device_id='root'`).Error
		if err == nil {
			return errors.New("migration passed a held transport fence")
		}
		return tx.Exec(`INSERT INTO committed_effects VALUES('effect')`).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE broker_transport_bindings SET route_epoch=10 WHERE device_id='root'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return Lock(tx, "t", proof, false) }); !errors.Is(err, brokerroute.ErrFenced) {
		t.Fatalf("post-migration write not fenced: %v", err)
	}
}

func TestDelegatedConnectingRouteAdmitsButDefersExecution(t *testing.T) {
	db, proof := delegationFixture(t)
	if err := db.Exec(`UPDATE broker_transport_bindings SET phase='connecting' WHERE device_id='root'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return Lock(tx, "t", proof, true) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return Lock(tx, "t", proof, false) }); !errors.Is(err, brokerroute.ErrConnecting) {
		t.Fatalf("connecting root not deferred: %v", err)
	}
}

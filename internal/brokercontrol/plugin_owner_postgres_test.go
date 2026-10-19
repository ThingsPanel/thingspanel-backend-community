package brokercontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	bc "project/internal/brokercontract"
)

func TestPluginSocketOwnerPersistsImmutableContext(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_BROKER_DATABASE_DSN is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated broker QA database")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "broker_plugin_it_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal("create private test schema")
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	testDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("configure isolated schema")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open private test schema")
	}
	setup := `
	CREATE TABLE device_configs (id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_type varchar(1) NOT NULL, protocol_type varchar(36));
	CREATE TABLE devices (id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_number varchar(36) NOT NULL,
	 voucher varchar(500) NOT NULL, is_enabled varchar(36) NOT NULL, activate_flag varchar(36) NOT NULL,
	 parent_id varchar(36), device_config_id varchar(36), service_access_id varchar(36), access_way varchar(36));
	CREATE TABLE service_plugins (id varchar(36) PRIMARY KEY, service_identifier varchar(100) NOT NULL, service_type int NOT NULL, service_config jsonb);
	CREATE TABLE service_access (id varchar(36) PRIMARY KEY, service_plugin_id varchar(36) NOT NULL, tenant_id varchar(36) NOT NULL);
	CREATE TABLE device_topic_mappings (id bigint PRIMARY KEY, device_config_id varchar(36) NOT NULL, enabled boolean NOT NULL, direction text, source_topic text, target_topic text, priority integer, data_identifier text);
	CREATE TABLE broker_mapping_snapshots (version varchar(80) PRIMARY KEY, rules jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now());`
	if err := db.Exec(setup).Error; err != nil {
		t.Fatal("create private owner fixture tables")
	}
	if err := db.AutoMigrate(&ClientIDBinding{}, &TransportBinding{}, &pluginContextRow{}); err != nil {
		t.Fatal("create private broker owner tables")
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "016_plugin_context.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal("apply plugin context migration")
	}
	voucherMigration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "011_safe_device_voucher.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(voucherMigration)).Error; err != nil {
		t.Fatal("apply safe voucher migration")
	}
	if err := db.Exec(`INSERT INTO service_plugins(id,service_identifier,service_type,service_config) VALUES ('p1','svc',2,'{"sub_topic_prefix":"service/alarm/"}');
	 INSERT INTO service_access(id,service_plugin_id,tenant_id) VALUES ('sa1','p1','tenant-1');
	 INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag,service_access_id,access_way)
	 VALUES ('d1','tenant-1','number-1','{}','enabled','active','sa1','B');
	 INSERT INTO device_configs(id,tenant_id,device_type,protocol_type) VALUES ('cfg-proto','tenant-1','3','MODBUS_RTU');
	 INSERT INTO service_plugins(id,service_identifier,service_type,service_config) VALUES ('p2','MODBUS_RTU',1,'{"sub_topic_prefix":"plugin/modbus"}');
	 INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag,device_config_id,access_way)
	 VALUES ('d2','tenant-1','number-2','{"username":"proto-user","password":"pw"}','enabled','active','cfg-proto','A');
	 INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag,service_access_id,device_config_id,access_way)
	 VALUES ('d3','tenant-1','number-3','{}','enabled','active','sa1','cfg-proto','A')`).Error; err != nil {
		t.Fatal("seed plugin service target")
	}
	repo := NewGormRepository(db)
	owner, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: strings.Repeat("t", 40), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{{
		Username: "legacy-plugin", Password: "secret-for-fixture", BrokerCell: "community", BrokerInstance: "node-1",
		AllowedDeviceIDs: []string{"d1"}, AllowedTenantIDs: []string{"tenant-1"}, PluginSocket: true, AllowAnyClientID: true,
		Principal:     bc.Principal{TenantID: "tenant-1", DeviceID: "svc-1", Kind: bc.PrincipalService},
		Policy:        bc.Policy{Publish: []string{"devices/telemetry"}, MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress},
		PolicyVersion: "plugin-policy-v1", MappingVersion: MappingVersion,
	}}})
	if err != nil {
		t.Fatal("construct plugin owner")
	}
	ctx := context.Background()
	grant, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "auth", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1",
		ConnectionNonce: "nonce-1", WireClientID: "old-sdk-random-client", Username: "legacy-plugin", Password: "secret-for-fixture", Protocol: 4})
	if err != nil || !grant.Allow {
		t.Fatalf("plugin auth not granted: allow=%t err=%v", grant.Allow, err)
	}
	if len(grant.PluginTargets) != 1 || grant.PluginTargets[0].DeviceID != "d1" || grant.PluginTargets[0].TopicPrefix != "service/alarm/" || len(grant.PluginContextID) != 64 {
		t.Fatalf("grant omitted owner-resolved context: targets=%#v context=%q", grant.PluginTargets, grant.PluginContextID)
	}
	if !containsExact(grant.Policy.Publish, "service/alarm/devices/telemetry") || !containsExact(grant.Policy.Subscribe, "service/alarm/devices/command/number-1/+") {
		t.Fatalf("legacy prefix ACL missing: %#v", grant.Policy)
	}
	if err := db.Create(&TransportBinding{TenantID: "tenant-1", DeviceID: "svc-normal", BrokerCell: "community", RouteEpoch: 1, Phase: "active", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal("seed normal service route")
	}
	legacyOwner, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: strings.Repeat("t", 40), BrokerCell: "community", BrokerInstance: "node-1"}}, Services: []ServiceCredential{{
		Username: "backend-service", Password: "backend-password", BrokerCell: "community", BrokerInstance: "node-1", AllowedClientIDs: []string{"fixed-client"},
		AllowedDeviceIDs: []string{"d1"}, AllowedTenantIDs: []string{"tenant-1"}, Principal: bc.Principal{TenantID: "tenant-1", DeviceID: "svc-normal", Kind: bc.PrincipalService},
		Policy: bc.Policy{Publish: []string{"devices/command/number-1/+"}, MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, PolicyVersion: "legacy-policy", MappingVersion: MappingVersion,
	}}})
	if err != nil {
		t.Fatal("construct ordinary backend service owner")
	}
	backendGrant, err := legacyOwner.Authenticate(ctx, bc.AuthRequest{RequestID: "backend-auth", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "backend-nonce", WireClientID: "fixed-client", Username: "backend-service", Password: "backend-password", Protocol: 4})
	if err != nil || !backendGrant.Allow || backendGrant.PluginContextID != "" || len(backendGrant.PluginTargets) != 0 || !containsExact(backendGrant.Policy.Publish, "service/alarm/devices/command/number-1/+") {
		t.Fatalf("ordinary backend service prefix alias mismatch: %#v err=%v", backendGrant, err)
	}
	deviceGrant, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "device-auth", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1",
		ConnectionNonce: "device-nonce", WireClientID: "protocol-plugin-child", Username: "proto-user", Password: "pw", Protocol: 4})
	if err != nil || !deviceGrant.Allow || deviceGrant.Principal.Kind != bc.PrincipalGateway || len(deviceGrant.PluginTargets) != 1 ||
		deviceGrant.PluginTargets[0].Kind != bc.PrincipalGateway || deviceGrant.PluginTargets[0].TopicPrefix != "plugin/modbus/" ||
		!containsExact(deviceGrant.Policy.Publish, "devices/telemetry") || !containsExact(deviceGrant.Policy.Publish, "plugin/modbus/gateway/telemetry") {
		t.Fatalf("protocol-owned type3 voucher grant mismatch: %#v err=%v", deviceGrant, err)
	}
	// A device carrying both a service-access relation and a protocol-plugin
	// configuration must resolve to exactly one target, and that target must be
	// the service-access one: prefix and device kind come from the service
	// relation, never from the protocol branch (matches brokerplugin.Resolve).
	if err := db.Create(&TransportBinding{TenantID: "tenant-1", DeviceID: "d3", BrokerCell: "community", RouteEpoch: 9, Phase: "active", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal("seed dual-relation route")
	}
	dual, err := owner.resolvePluginTargets(ctx, []string{"d3"}, []string{"tenant-1"}, "community")
	if err != nil || len(dual) != 1 || dual[0].TopicPrefix != "service/alarm/" || dual[0].Kind != bc.PrincipalDevice || dual[0].RouteEpoch != 9 {
		t.Fatalf("dual-relation device did not prefer service access: %#v err=%v", dual, err)
	}
	stored, err := ReadPluginContext(ctx, db, grant.PluginContextID)
	if err != nil || stored.ConnectionID != grant.ConnectionID || stored.Targets[0].RouteEpoch != grant.RouteEpoch {
		t.Fatalf("context read mismatch: %#v err=%v", stored, err)
	}
	// The stable same-connection ID can extend expiry but cannot overwrite scope.
	changed := stored
	changed.Targets = append([]bc.PluginTarget(nil), stored.Targets...)
	changed.ExpiresAt = stored.ExpiresAt.Add(5 * time.Second)
	if err := repo.StorePluginContext(ctx, changed); err != nil {
		t.Fatal("store idempotent context fixture")
	}
	still, err := ReadPluginContext(ctx, db, grant.PluginContextID)
	if err != nil || still.Targets[0].TopicPrefix != "service/alarm/" || !still.ExpiresAt.After(stored.ExpiresAt) {
		t.Fatalf("context scope changed on conflict: %#v %v", still, err)
	}
	changed.Targets[0].TopicPrefix = "service/other/"
	if err := repo.StorePluginContext(ctx, changed); err == nil {
		t.Fatal("same ID accepted a different immutable target scope")
	}
	grant2, e := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "auth2", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1",
		ConnectionNonce: "nonce-2", WireClientID: "another-random-id", Username: "legacy-plugin", Password: "secret-for-fixture", Protocol: 5})
	if e != nil || !grant2.Allow || grant2.PluginContextID == grant.PluginContextID {
		t.Fatalf("new connection did not get distinct context: %s %s %v", grant.PluginContextID, grant2.PluginContextID, e)
	}
	var rowCount int64
	if err := db.Model(&pluginContextRow{}).Count(&rowCount).Error; err != nil || rowCount != 3 {
		t.Fatalf("context row count=%d err=%v", rowCount, err)
	}
}

package brokercontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	bc "project/internal/brokercontract"
	"project/internal/brokermapper"
)

func TestPostgresVoucherJSONBSemanticsAndOwnerTransactions(t *testing.T) {
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
	schema := "broker_owner_it_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal("create isolated test schema")
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	testDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("configure isolated test schema")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated schema connection")
	}
	if err := db.Exec(`CREATE TABLE device_configs (id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_type varchar(1) NOT NULL);
		CREATE TABLE devices (
		id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_number varchar(36) NOT NULL,
		voucher varchar(500) NOT NULL, is_enabled varchar(36) NOT NULL, activate_flag varchar(36) NOT NULL,
		parent_id varchar(36) NULL, device_config_id varchar(36) NULL);
		CREATE TABLE device_topic_mappings (id bigint PRIMARY KEY, device_config_id varchar(36) NOT NULL, direction text NOT NULL,
		source_topic text NOT NULL, target_topic text NOT NULL, priority integer NOT NULL, data_identifier text NULL, enabled boolean NOT NULL);
		CREATE TABLE broker_mapping_snapshots (version varchar(80) PRIMARY KEY, rules jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatal("create isolated devices fixture")
	}
	if err := db.AutoMigrate(&ClientIDBinding{}, &TransportBinding{}); err != nil {
		t.Fatal("create owner fixture tables")
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "011_safe_device_voucher.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(body)).Error; err != nil {
		t.Fatal("apply safe voucher fixture migration")
	}
	if err := db.Exec(`INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag) VALUES
		('d1','tenant-1','num-1',' {"password":"p","username":"u"} ','enabled','active')`).Error; err != nil {
		t.Fatal("insert credential fixture")
	}
	repo := &GormRepository{DB: db}
	ctx := context.Background()
	matches, err := repo.FindCredentialMatches(ctx, `{"username":"u","password":"p"}`)
	if err != nil || len(matches) != 1 {
		t.Fatalf("jsonb semantic match count=%d err=%v", len(matches), err)
	}
	miss, err := repo.FindCredentialMatches(ctx, `{"username":"u","password":"wrong"}`)
	if err != nil || len(miss) != 0 {
		t.Fatalf("password mismatch count=%d err=%v", len(miss), err)
	}
	// Real legacy/default vouchers must not poison another device's auth scan.
	badVouchers := []string{"", "legacy-token", "{", "[]", "null", `{"username":"wrong","username":"u","password":"p"}`, `{"username":"u","password":"wrong","password":"p"}`, `{"username":"u","password":1e1000000}`}
	for i, voucher := range badVouchers {
		if err := db.Exec("INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag) VALUES(?,?,?,?,'enabled','active')", fmt.Sprintf("bad-%d", i), "tenant-1", fmt.Sprintf("bad-num-%d", i), voucher).Error; err != nil {
			t.Fatal("seed malformed voucher")
		}
	}
	stillValid, e := repo.FindCredentialMatches(ctx, `{"username":"u","password":"p"}`)
	if e != nil || len(stillValid) != 1 || stillValid[0].ID != "d1" {
		t.Fatalf("invalid voucher row poisoned valid authentication: count=%d error=%v", len(stillValid), e)
	}
	var invalid int64
	if err := db.Raw("SELECT count(*) FROM devices WHERE broker_safe_voucher_v1(voucher) IS NULL").Scan(&invalid).Error; err != nil || invalid != int64(len(badVouchers)) {
		t.Fatalf("safe voucher invalid count=%d err=%v", invalid, err)
	}
	if err := db.Exec(`INSERT INTO device_configs(id,tenant_id,device_type) VALUES ('cfg-map','tenant-1','1'),('cfg-gateway','tenant-1','2'),('cfg-child','tenant-1','3'),('cfg-bad','tenant-1','1'),('cfg-foreign','tenant-2','1');
		INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag,device_config_id) VALUES
		('d-user-only','tenant-1','num-user-only','{"username":"u-only"}','enabled','active',NULL),
		('d-map','tenant-1','num-map','{"username":"mapped","password":"p"}','enabled','active','cfg-map'),
		('d-gateway','tenant-1','num-gateway','{"username":"gateway","password":"p"}','enabled','active','cfg-gateway'),
		('d-child','tenant-1','num-child','{"username":"child","password":"p"}','enabled','active','cfg-child');
		INSERT INTO device_topic_mappings(id,device_config_id,direction,source_topic,target_topic,priority,enabled) VALUES
		(1,'cfg-map','up','custom/{device_number}/{message_id}','devices/event/{message_id}',10,true),
		(2,'cfg-map','down','custom/{device_number}/control/{message_id}','devices/command/{device_number}/{message_id}',20,true),
		(3,'cfg-foreign','up','foreign/{device_number}/{message_id}','devices/event/{message_id}',10,true),
		(4,'cfg-bad','up','bad/{device_number}/{message_id}','$tp/state/v1/forbidden',10,true)`).Error; err != nil {
		t.Fatal("insert mapping auth fixtures")
	}
	targets, err := repo.ResolveServiceTargets(ctx, []string{"d-map", "d-user-only", "missing-id"})
	if err != nil || len(targets) != 2 || targets[0].TenantID != "tenant-1" || targets[1].TenantID != "tenant-1" || targets[0].DeviceNumberCount != 1 || targets[1].DeviceNumberCount != 1 {
		t.Fatalf("batch service target resolution mismatch: %#v %v", targets, err)
	}
	extra, err := repo.FindCredentialMatches(ctx, `{"username":"u","password":"p","tenant":"tenant-1"}`)
	if err != nil || len(extra) != 0 {
		t.Fatalf("extra JSON property count=%d err=%v", len(extra), err)
	}
	owner, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: strings.Repeat("t", 40), BrokerCell: "community", BrokerInstance: "node-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "r", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce", WireClientID: "wire-a", Username: "u", Password: "p", Protocol: 4})
	if err != nil || !grant.Allow {
		t.Fatalf("real PostgreSQL device auth failed: allow=%v err=%v", grant.Allow, err)
	}
	userOnly, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "r-user-only", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce-user", WireClientID: "wire-user", Username: "u-only", Protocol: 4})
	if err != nil || !userOnly.Allow {
		t.Fatalf("username-only voucher denied: allow=%v err=%v", userOnly.Allow, err)
	}
	mapped, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "r-map", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce-map", WireClientID: "wire-map", Username: "mapped", Password: "p", Protocol: 4})
	if err != nil || !mapped.Allow || mapped.MappingVersion == "standard-v1" || !containsExact(mapped.Policy.Publish, "custom/num-map/+") || !containsExact(mapped.Policy.Subscribe, "custom/num-map/control/+") {
		t.Fatalf("mapping grant did not freeze scoped filters: %#v err=%v", mapped, err)
	}
	mapperRepo := repo.Mapper
	if mapperRepo == nil {
		mapperRepo = brokermapper.NewRepository(db)
	}
	loaded, err := mapperRepo.Load(ctx, mapped.MappingVersion)
	if err != nil || loaded.Version != mapped.MappingVersion || len(loaded.Rules()) != 2 {
		t.Fatalf("load immutable snapshot: %#v %v", loaded, err)
	}
	if _, err := mapperRepo.CompileForDeviceConfig(ctx, "tenant-1", "cfg-bad"); !errors.Is(err, brokermapper.ErrUnsupportedMapping) {
		t.Fatalf("unsupported mapping row was skipped: %v", err)
	}
	if err := db.Exec(`UPDATE device_topic_mappings SET source_topic='custom-v2/{device_number}/{message_id}' WHERE id=1`).Error; err != nil {
		t.Fatal("change live mapping fixture")
	}
	updated, err := mapperRepo.CompileForDeviceConfig(ctx, "tenant-1", "cfg-map")
	if err != nil || updated.Version == mapped.MappingVersion {
		t.Fatalf("mapping update did not create a new version: %v", err)
	}
	loaded, err = mapperRepo.Load(ctx, mapped.MappingVersion)
	if err != nil || loaded.Rules()[0].SourceTopic != "custom/{device_number}/{message_id}" {
		t.Fatalf("old snapshot was mutated: %#v %v", loaded, err)
	}
	wrongTenant, err := mapperRepo.CompileForDeviceConfig(ctx, "tenant-2", "cfg-map")
	if err != nil || wrongTenant.Version != "standard-v1" {
		t.Fatalf("cross-tenant rules leaked: %v %v", wrongTenant, err)
	}
	gateway, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "r-gateway", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce-gw", WireClientID: "wire-gw", Username: "gateway", Password: "p", Protocol: 4})
	if err != nil || !gateway.Allow || gateway.Principal.Kind != bc.PrincipalGateway {
		t.Fatalf("empty gateway profile not granted: %#v %v", gateway, err)
	}
	child, err := owner.Authenticate(ctx, bc.AuthRequest{RequestID: "r-child", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce-child", WireClientID: "wire-child", Username: "child", Password: "p", Protocol: 4})
	if err != nil || child.Allow {
		t.Fatalf("gateway child authenticated directly: %#v %v", child, err)
	}
	if err := db.Exec(`INSERT INTO devices(id,tenant_id,device_number,voucher,is_enabled,activate_flag) VALUES ('d2','tenant-1','num-2','{"username":"u","password":"p"}','enabled','active')`).Error; err != nil {
		t.Fatal("insert semantic duplicate")
	}
	grant, err = owner.Authenticate(ctx, bc.AuthRequest{RequestID: "r2", Provider: CommunityProvider, BrokerCell: "community", BrokerInstance: "node-1", ConnectionNonce: "nonce2", WireClientID: "wire-b", Username: "u", Password: "p", Protocol: 4})
	if err != nil || grant.Allow {
		t.Fatalf("ambiguous JSONB credential was not denied: allow=%v err=%v", grant.Allow, err)
	}

	var success atomic.Int32
	var wg sync.WaitGroup
	for _, id := range []string{"device-a", "device-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			ok, e := repo.ClaimClientID(ctx, "community", "contended-wire", "tenant-1", id)
			if e != nil {
				t.Errorf("claim error: %v", e)
			}
			if ok {
				success.Add(1)
			}
		}(id)
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("unique ClientID owner count=%d, want 1", success.Load())
	}
	var routes atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := repo.ResolveActiveBinding(ctx, "tenant-route", "device-route", "community")
			if e != nil {
				t.Errorf("route resolve error: %v", e)
				return
			}
			if b.RouteEpoch == 1 && b.Phase == "active" {
				routes.Add(1)
			}
		}()
	}
	wg.Wait()
	if routes.Load() != 8 {
		t.Fatalf("route binders completed=%d, want 8", routes.Load())
	}
	if err := db.Model(&TransportBinding{}).Where("tenant_id=? AND device_id=?", "tenant-route", "device-route").Update("phase", "connecting").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResolveActiveBinding(ctx, "tenant-route", "device-route", "community"); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("connecting route renewed: %v", err)
	}
}

func withSearchPath(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return fmt.Sprintf("%s search_path=%s", strings.TrimSpace(dsn), schema), nil
}

package brokercontrol

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"testing"
)

func TestPostgresUUIDConfigAndTextMappingScope(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_BROKER_DATABASE_DSN is not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open test database")
	}
	pool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := "broker_mapping_uuid_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	isolatedDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("isolate test schema")
	}
	// The fixture changes physical column types below. Do not retain a prepared
	// statement's UUID parameter types from the preceding schema layout.
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: isolatedDSN, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated schema")
	}
	isolatedPool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer isolatedPool.Close()
	if err := db.Exec(`CREATE TABLE device_configs(id uuid PRIMARY KEY,tenant_id varchar(36) NOT NULL);
 CREATE TABLE device_topic_mappings(id bigint PRIMARY KEY,device_config_id varchar(36) NOT NULL,direction text NOT NULL,source_topic text NOT NULL,target_topic text NOT NULL,priority integer NOT NULL,data_identifier text,enabled boolean NOT NULL);
 CREATE TABLE broker_mapping_snapshots(version varchar(80) PRIMARY KEY,rules jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now());`).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	ctx := context.Background()
	repo := NewGormRepository(db)
	if err := db.Exec("INSERT INTO device_configs VALUES (?::uuid,?)", id, "tenant-a").Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.CompileMappingSnapshot(ctx, "tenant-a", id)
	if err != nil || snapshot.Version != "standard-v1" {
		t.Fatalf("empty mapping: %v %v", snapshot, err)
	}
	if err := db.Exec(`INSERT INTO device_topic_mappings VALUES
 (1,?,'up','sensor/{device_number}/{message_id}','devices/event/{message_id}',10,NULL,TRUE),
 (2,'legacy-not-a-uuid','up','other/{device_number}/{message_id}','devices/event/{message_id}',10,NULL,TRUE)`, id).Error; err != nil {
		t.Fatal(err)
	}
	enabled, err := repo.HasEnabledTopicMappings(ctx, "tenant-a", id)
	if err != nil || !enabled {
		t.Fatalf("UUID/text mapping lookup: %v %v", enabled, err)
	}
	snapshot, err = repo.CompileMappingSnapshot(ctx, "tenant-a", id)
	if err != nil || snapshot.Version == "standard-v1" || len(snapshot.Rules()) != 1 {
		t.Fatalf("UUID/text snapshot: %v %v", snapshot, err)
	}
	enabled, err = repo.HasEnabledTopicMappings(ctx, "tenant-b", id)
	if err != nil || enabled {
		t.Fatalf("cross-tenant mapping lookup: %v %v", enabled, err)
	}
	snapshot, err = repo.CompileMappingSnapshot(ctx, "tenant-b", id)
	if err != nil || snapshot.Version != "standard-v1" || len(snapshot.Rules()) != 0 {
		t.Fatalf("cross-tenant snapshot: %v %v", snapshot, err)
	}
	// Deployed community schemas also use the inverse types: textual config IDs
	// and UUID mapping foreign keys. Neither side may depend on an implicit cast.
	if err := db.Exec(`DELETE FROM device_topic_mappings WHERE id=2;
		ALTER TABLE device_configs ALTER COLUMN id TYPE varchar(36) USING id::text;
		ALTER TABLE device_topic_mappings ALTER COLUMN device_config_id TYPE uuid USING device_config_id::uuid;`).Error; err != nil {
		t.Fatal(err)
	}
	enabled, err = repo.HasEnabledTopicMappings(ctx, "tenant-a", id)
	if err != nil || !enabled {
		t.Fatalf("text/UUID mapping lookup: %v %v", enabled, err)
	}
	snapshot, err = repo.CompileMappingSnapshot(ctx, "tenant-a", id)
	if err != nil || snapshot.Version == "standard-v1" || len(snapshot.Rules()) != 1 {
		t.Fatalf("text/UUID snapshot: %v %v", snapshot, err)
	}
	enabled, err = repo.HasEnabledTopicMappings(ctx, "tenant-b", id)
	if err != nil || enabled {
		t.Fatalf("text/UUID cross-tenant mapping: %v %v", enabled, err)
	}
}

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

func TestConnectionTopicsPostgresScopeAndUnsupported(t *testing.T) {
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

	if err := db.Exec(`CREATE TABLE device_configs(id text PRIMARY KEY,tenant_id text,device_type text,protocol_type text);
 CREATE TABLE devices(id text PRIMARY KEY,tenant_id text,device_number text,is_enabled text,activate_flag text,parent_id text,device_config_id text,service_access_id text,access_way text,protocol text);
 CREATE TABLE device_topic_mappings(id int,device_config_id text,enabled bool);
 INSERT INTO device_configs VALUES('config','tenant','1','MQTT');
 INSERT INTO devices VALUES('d','tenant','qa-number','enabled','active',NULL,'config',NULL,'A','MQTT');`).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewGormRepository(db)
	ctx := context.Background()
	out, err := repo.ReadConnectionTopics(ctx, "tenant", "d")
	if err != nil || !out.Available || len(out.Items) == 0 {
		t.Fatalf("direct: %+v %v", out, err)
	}
	out, err = repo.ReadConnectionTopics(ctx, "other", "d")
	if err != nil || out.Available || len(out.Items) != 0 {
		t.Fatalf("tenant leak: %+v %v", out, err)
	}
	if err := db.Exec(`INSERT INTO device_topic_mappings VALUES(1,'config',TRUE)`).Error; err != nil {
		t.Fatal(err)
	}
	out, err = repo.ReadConnectionTopics(ctx, "tenant", "d")
	if err != nil || out.Available || out.Reason == nil || *out.Reason != "custom_mapping" {
		t.Fatalf("custom: %+v %v", out, err)
	}
	if err := db.Exec(`DELETE FROM device_topic_mappings;UPDATE devices SET service_access_id='plugin' WHERE id='d'`).Error; err != nil {
		t.Fatal(err)
	}
	out, err = repo.ReadConnectionTopics(ctx, "tenant", "d")
	if err != nil || out.Available || *out.Reason != "unsupported_access" {
		t.Fatalf("plugin: %+v %v", out, err)
	}
	if err := db.Exec(`UPDATE devices SET service_access_id=NULL,parent_id='gateway' WHERE id='d'`).Error; err != nil {
		t.Fatal(err)
	}
	out, err = repo.ReadConnectionTopics(ctx, "tenant", "d")
	if err != nil || out.Available {
		t.Fatalf("child: %+v %v", out, err)
	}
}

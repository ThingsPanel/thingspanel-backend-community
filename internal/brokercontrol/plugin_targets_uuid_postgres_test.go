package brokercontrol

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Deployed community schemas use varchar device_configs.id and uuid device_topic_mappings.device_config_id.
// Resolving protocol-plugin targets must not rely on an implicit uuid = varchar comparison: PostgreSQL
// rejects it, the owner then reports the targets as unavailable, and every protocol plugin that already has
// registered devices is refused at connect time ("not Authorized").
func TestPostgresPluginTargetsDoNotDependOnImplicitUUIDCast(t *testing.T) {
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
	schema := "broker_plugin_uuid_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	isolatedDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("isolate test schema")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: isolatedDSN, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated schema")
	}
	isolatedPool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer isolatedPool.Close()
	if err := db.Exec(`CREATE TABLE device_configs(id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, protocol_type varchar(36), device_type varchar(9));
		CREATE TABLE device_topic_mappings(id bigint PRIMARY KEY, device_config_id uuid NOT NULL, enabled boolean NOT NULL);
		CREATE TABLE service_plugins(id varchar(36) PRIMARY KEY, service_identifier varchar(100), service_type int, service_config text);
		CREATE TABLE service_access(id varchar(36) PRIMARY KEY, tenant_id varchar(36), service_plugin_id varchar(36));
		CREATE TABLE devices(id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, device_number varchar(36), device_config_id varchar(36),
			service_access_id varchar(36), access_way varchar(1), is_enabled varchar(10), activate_flag varchar(10), parent_id varchar(36));`).Error; err != nil {
		t.Fatal(err)
	}
	config, device := uuid.NewString(), uuid.NewString()
	if err := db.Exec(`INSERT INTO device_configs VALUES (?, 'tenant-a', 'HTTP', '1');
		INSERT INTO service_plugins VALUES ('sp-1', 'HTTP', 1, '{"sub_topic_prefix":"plugin/http/"}');
		INSERT INTO devices VALUES (?, 'tenant-a', 'dev-1', ?, NULL, 'A', 'enabled', 'active', NULL)`, config, device, config).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewGormRepository(db)
	targets, err := repo.ResolvePluginTargets(context.Background(), []string{device})
	if err != nil || len(targets) != 1 || targets[0].DeviceID != device || targets[0].TopicPrefix != "plugin/http/" {
		t.Fatalf("plugin target resolution on the deployed uuid/varchar schema: %v %+v", err, targets)
	}
	// A custom topic mapping on the device's config still removes it from the plugin scope.
	if err := db.Exec(`INSERT INTO device_topic_mappings VALUES (1, ?::uuid, TRUE)`, config).Error; err != nil {
		t.Fatal(err)
	}
	targets, err = repo.ResolvePluginTargets(context.Background(), []string{device})
	if err != nil || len(targets) != 0 {
		t.Fatalf("a mapped config must not resolve as a plain plugin target: %v %+v", err, targets)
	}
}

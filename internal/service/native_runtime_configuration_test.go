package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	bc "project/internal/brokercontract"
	"project/internal/brokercontrol"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/processor"
	"strings"
	"testing"
	"time"
)

func configurationFixture(generation int64, topic string) *NativeRuntimeConfiguration {
	value := map[string]any{"model": map[string]any{"modelId": "11111111-1111-4111-8111-111111111111", "snapshotId": "22222222-2222-4222-8222-222222222222", "publishedRevision": 3}, "deviceKind": "standalone", "access": map[string]any{"mode": "direct", "protocol": "mqtt", "connectionDirection": "device_to_platform"}, "topicMappings": []any{map[string]any{"name": "temperature", "direction": "up", "sourceTopic": topic, "targetTopic": "devices/telemetry", "priority": 100, "enabled": true}}, "processingScripts": []any{map[string]any{"name": "temperature transform", "phase": "telemetry_up", "language": "lua", "entrypoint": "encodeInp", "content": "function encodeInp(msg, topic) local json=require('json');local d=json.decode(msg);d.temperature=d.temperature+1;return json.encode(d) end", "enabled": true}}}
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return &NativeRuntimeConfiguration{Generation: generation, Digest: hex.EncodeToString(sum[:]), Configuration: raw}
}
func TestNativeRuntimeConfigurationValidation(t *testing.T) {
	good := configurationFixture(1, "qa/up")
	if _, err := validateNativeRuntimeConfiguration(good); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"digest", "trailing", "foreign", "gateway", "unknown", "mapping"} {
		t.Run(mode, func(t *testing.T) {
			in := *good
			var value map[string]any
			_ = json.Unmarshal(in.Configuration, &value)
			switch mode {
			case "digest":
				in.Digest = "bad"
			case "trailing":
				in.Configuration = append(in.Configuration, []byte(" {}")...)
			case "foreign":
				value["access"].(map[string]any)["providerId"] = "foreign"
			case "gateway":
				value["deviceKind"] = "gateway"
			case "unknown":
				value["access"].(map[string]any)["protocol"] = "unknown"
			case "mapping":
				value["topicMappings"].([]any)[0].(map[string]any)["targetTopic"] = "$SYS/private"
			}
			if mode != "digest" && mode != "trailing" {
				in.Configuration, _ = json.Marshal(value)
				sum := sha256.Sum256(in.Configuration)
				in.Digest = hex.EncodeToString(sum[:])
			}
			if _, err := validateNativeRuntimeConfiguration(&in); !errors.Is(err, ErrNativeRegistrationInvalid) {
				t.Fatalf("accepted %s: %v", mode, err)
			}
		})
	}
}
func TestNativeRuntimeConfigurationPostgresProjectionAndLeaseFence(t *testing.T) {
	db := automationPG(t)
	ctx := context.Background()
	if err := db.Exec("CREATE TABLE users (tenant_id varchar(36)); INSERT INTO users VALUES ('tenant-a')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&dal.DeviceIDReservation{}, &model.DeviceConfig{}, &model.DeviceTopicMapping{}, &model.DataScript{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"004_mapping_snapshots.sql", "011_safe_device_voucher.sql", "018_native_runtime_registrations.sql", "019_native_runtime_credential_lifecycle.sql", "020_native_credential_leases.sql", "021_native_runtime_lifecycle.sql", "022_native_runtime_configuration.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(strings.ReplaceAll(string(raw), "public.", "")).Error; err != nil {
			t.Fatal(err)
		}
	}
	tenant := nativeTenantIdentity("tenant-a")
	in := NativeRuntimeRegistration{DeviceID: uuid.NewString(), CredentialID: uuid.NewString(), EventID: uuid.NewString(), Change: "activation", DeviceNumber: "configuration-test", Voucher: `{"username":"configuration-secret"}`, RuntimeConfiguration: configurationFixture(1, "qa/v1")}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", in); err != nil {
		t.Fatal(err)
	}
	initialBinding := NativeRuntimeLifecycle{DeviceID: in.DeviceID, EventID: uuid.NewString(), Kind: "template_binding", Generation: 1, RuntimeConfiguration: in.RuntimeConfiguration}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", initialBinding); err != nil {
		t.Fatal("registration then binding exact receipt", err)
	}
	var first model.Device
	if err := db.First(&first, "id=?", in.DeviceID).Error; err != nil {
		t.Fatal(err)
	}
	if first.DeviceConfigID == nil {
		t.Fatal("configuration absent")
	}
	var projected model.DeviceConfig
	if err := db.First(&projected, "id=?", *first.DeviceConfigID).Error; err != nil {
		t.Fatal(err)
	}
	if projected.NativeThingModelID == nil || *projected.NativeThingModelID != "11111111-1111-4111-8111-111111111111" || projected.NativeThingModelSnapshotID == nil || *projected.NativeThingModelSnapshotID != "22222222-2222-4222-8222-222222222222" || projected.NativeThingModelRevision == nil || *projected.NativeThingModelRevision != 3 {
		t.Fatal("fixed native model binding missing")
	}
	var scripts []model.DataScript
	db.Where("device_config_id=?", *first.DeviceConfigID).Find(&scripts)
	if len(scripts) != 1 || scripts[0].ScriptType != "A" {
		t.Fatal("script phase not projected")
	}
	transformed, err := processor.NewLuaExecutor().ExecuteDecode(ctx, *scripts[0].Content, []byte(`{"temperature":22.25}`))
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]float64
	if json.Unmarshal([]byte(transformed), &values) != nil || values["temperature"] != 23.25 {
		t.Fatal("persisted Lua did not execute", transformed)
	}
	for _, sql := range []string{
		"UPDATE device_configs SET name='tampered' WHERE id=?",
		"DELETE FROM device_configs WHERE id=?",
		"UPDATE data_scripts SET content='tampered' WHERE device_config_id=?",
		"DELETE FROM data_scripts WHERE device_config_id=?",
		"UPDATE device_topic_mappings SET source_topic='tampered' WHERE device_config_id=?",
		"DELETE FROM device_topic_mappings WHERE device_config_id=?",
		"INSERT INTO data_scripts(id,name,device_config_id,enable_flag,script_type) VALUES('illegal','illegal',?,'Y','A')",
		"INSERT INTO device_topic_mappings(device_config_id,name,direction,source_topic,target_topic,priority,enabled,created_at,updated_at) VALUES(?,'illegal','up','qa/illegal','devices/telemetry',100,true,now(),now())",
	} {
		if err := db.Exec(sql, *first.DeviceConfigID).Error; err == nil || !strings.Contains(err.Error(), "native runtime configuration is immutable") {
			t.Fatalf("managed legacy mutation accepted: %s", sql)
		}
	}
	ordinary := model.DeviceConfig{ID: uuid.NewString(), Name: "ordinary", DeviceType: "1", TenantID: "tenant-a", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&ordinary).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ordinary).Update("name", "ordinary edited").Error; err != nil {
		t.Fatal("ordinary update denied", err)
	}
	moving := model.DataScript{ID: uuid.NewString(), Name: "ordinary script", DeviceConfigID: ordinary.ID, EnableFlag: "Y", ScriptType: "A"}
	if err := db.Create(&moving).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&moving).Update("device_config_id", *first.DeviceConfigID).Error; err == nil || !strings.Contains(err.Error(), "native runtime configuration is immutable") {
		t.Fatal("moved ordinary row into managed config")
	}
	if err := db.Model(&scripts[0]).Update("device_config_id", ordinary.ID).Error; err == nil || !strings.Contains(err.Error(), "native runtime configuration is immutable") {
		t.Fatal("moved managed row out")
	}
	if err := db.Delete(&moving).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&ordinary).Error; err != nil {
		t.Fatal(err)
	}
	// A legacy INSERT begun before managed registration must wait for the
	// projection transaction and then recheck immutability, not slip through.
	creating := db.Begin()
	if creating.Error != nil {
		t.Fatal(creating.Error)
	}
	concurrentID := uuid.NewString()
	another := model.DeviceConfig{ID: concurrentID, Name: "managed concurrent", DeviceType: "1", TenantID: "tenant-a", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := creating.Create(&another).Error; err != nil {
		creating.Rollback()
		t.Fatal(err)
	}
	attempted := make(chan error, 1)
	go func() {
		attempted <- db.Exec("INSERT INTO data_scripts(id,name,device_config_id,enable_flag,script_type) VALUES(?, 'racing',?,'Y','A')", uuid.NewString(), concurrentID).Error
	}()
	select {
	case err := <-attempted:
		creating.Rollback()
		t.Fatalf("legacy write did not wait for projection: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := creating.Exec("INSERT INTO native_runtime_managed_configurations(device_config_id,device_id,generation,content_digest) VALUES(?,?,99,?)", concurrentID, in.DeviceID, in.RuntimeConfiguration.Digest).Error; err != nil {
		creating.Rollback()
		t.Fatal(err)
	}
	if err := creating.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-attempted:
		if err == nil || !strings.Contains(err.Error(), "native runtime configuration is immutable") {
			t.Fatal("concurrent legacy INSERT bypassed managed guard")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("configuration lock not released")
	}
	repo := brokercontrol.NewGormRepository(db)
	snap, err := repo.CompileMappingSnapshot(ctx, "tenant-a", *first.DeviceConfigID)
	if err != nil {
		t.Fatal(err)
	}
	req := bc.AuthRequest{BrokerCell: "community", BrokerInstance: "test", Username: "configuration-secret"}
	grant := bc.Grant{ConnectionID: "old-config", MappingVersion: snap.Version, ExpiresAt: time.Now().Add(time.Minute), Principal: bc.Principal{Kind: bc.PrincipalDevice, TenantID: "tenant-a", DeviceID: in.DeviceID}}
	if err := repo.RecordCredentialLease(ctx, req, grant); err != nil {
		t.Fatal(err)
	}
	update := NativeRuntimeLifecycle{DeviceID: in.DeviceID, EventID: uuid.NewString(), Kind: "template_binding", Generation: 2, RuntimeConfiguration: configurationFixture(2, "qa/v2")}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", update); err == nil || errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("must be pending: %v", err)
	}
	if err := repo.RecordCredentialLease(ctx, req, grant); err == nil {
		t.Fatal("pending update admitted old grant")
	}
	var during model.Device
	db.First(&during, "id=?", in.DeviceID)
	if *during.DeviceConfigID != *first.DeviceConfigID {
		t.Fatal("switched while old lease alive")
	}
	if err := db.Exec("UPDATE native_runtime_credential_leases SET expires_at=now()-interval '1 second'").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", update); err != nil {
			t.Fatal(err)
		}
	}
	var after model.Device
	db.First(&after, "id=?", in.DeviceID)
	if after.DeviceConfigID == nil || *after.DeviceConfigID == *first.DeviceConfigID {
		t.Fatal("immutable projection not advanced")
	}
	if err := repo.RecordCredentialLease(ctx, req, grant); err == nil {
		t.Fatal("pre-switch authentication admitted stale mapping")
	}
	next, err := repo.CompileMappingSnapshot(ctx, "tenant-a", *after.DeviceConfigID)
	if err != nil {
		t.Fatal(err)
	}
	grant.MappingVersion = next.Version
	grant.ConnectionID = "new-config"
	if err := repo.RecordCredentialLease(ctx, req, grant); err != nil {
		t.Fatal("new configuration cannot reconnect", err)
	}
	for _, mode := range []string{"tenant", "deployment", "stale", "digest"} {
		bad := update
		scope, dep := tenant, "dep"
		switch mode {
		case "tenant":
			scope = nativeTenantIdentity("other")
		case "deployment":
			dep = "other"
		case "stale":
			bad.EventID = uuid.NewString()
			bad.Generation = 1
			bad.RuntimeConfiguration = configurationFixture(1, "qa/v1")
		case "digest":
			bad.RuntimeConfiguration = configurationFixture(2, "qa/changed")
		}
		if err := ApplyNativeRuntimeLifecycle(ctx, db, scope, dep, bad); !errors.Is(err, ErrNativeRegistrationConflict) {
			t.Fatalf("%s not rejected: %v", mode, err)
		}
	}
	// Both old and new generations remain immutable after the pointer advances.
	for _, id := range []string{*first.DeviceConfigID, *after.DeviceConfigID} {
		if err := db.Exec("UPDATE device_configs SET name='late tamper' WHERE id=?", id).Error; err == nil {
			t.Fatal("historical managed projection editable")
		}
	}
	// Old credential event retry is never a configuration rollback.
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", in); err != nil {
		t.Fatal(err)
	}
	var final model.Device
	db.First(&final, "id=?", in.DeviceID)
	if *final.DeviceConfigID != *after.DeviceConfigID {
		t.Fatal("credential replay rolled configuration back")
	}
}

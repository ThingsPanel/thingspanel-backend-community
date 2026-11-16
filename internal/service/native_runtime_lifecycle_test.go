package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/url"
	"os"
	"path/filepath"
	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	"strings"
	"testing"
	"time"
)

func TestNativeRuntimeLifecycleOwnershipReplayAndLeaseFence(t *testing.T) {
	db := automationPG(t)
	ctx := context.Background()
	if err := db.Exec("CREATE TABLE users (tenant_id varchar(36)); INSERT INTO users VALUES ('tenant-a')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&dal.DeviceIDReservation{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"018_native_runtime_registrations.sql", "019_native_runtime_credential_lifecycle.sql", "020_native_credential_leases.sql", "021_native_runtime_lifecycle.sql", "022_native_runtime_configuration.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatal(err)
		}
	}
	tenant := nativeTenantIdentity("tenant-a")
	reg := NativeRuntimeRegistration{DeviceID: uuid.NewString(), CredentialID: uuid.NewString(), EventID: uuid.NewString(), Change: "activation", DeviceNumber: "before", Voucher: `{"username":"test-native"}`}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", reg); err != nil {
		t.Fatal(err)
	}
	confirmedAt, err := ConfirmNativeRuntimeActivation(ctx, db, tenant, "dep", reg)
	if err != nil || confirmedAt == nil {
		t.Fatalf("activation fact unavailable: %v", err)
	}
	for _, mode := range []string{"tenant", "event", "credential", "change", "replacement"} {
		altered := reg
		scope := tenant
		switch mode {
		case "tenant":
			scope = nativeTenantIdentity("other")
		case "event":
			altered.EventID = uuid.NewString()
		case "credential":
			altered.CredentialID = uuid.NewString()
		case "change":
			altered.Change = "rotation"
			altered.ReplacementCredentialID = reg.CredentialID
		case "replacement":
			altered.ReplacementCredentialID = uuid.NewString()
		}
		if _, err := ConfirmNativeRuntimeActivation(ctx, db, scope, "dep", altered); err == nil {
			t.Fatalf("unconfirmed activation fact accepted: %s", mode)
		}
	}
	// Both persisted owner credential and immutable event evidence must agree.
	for _, mutation := range []struct {
		sql         string
		args        []any
		restore     string
		restoreArgs []any
	}{
		{"UPDATE devices SET voucher=? WHERE id=?", []any{"tampered", reg.DeviceID}, "UPDATE devices SET voucher=? WHERE id=?", []any{reg.Voucher, reg.DeviceID}},
		{"UPDATE native_runtime_credential_events SET request_sha256=? WHERE event_id=?", []any{"tampered", reg.EventID}, "UPDATE native_runtime_credential_events SET request_sha256=? WHERE event_id=?", []any{nativeCredentialRequestDigest(reg), reg.EventID}},
	} {
		if err := db.Exec(mutation.sql, mutation.args...).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := ConfirmNativeRuntimeActivation(ctx, db, tenant, "dep", reg); err == nil {
			t.Fatal("drifted activation evidence accepted")
		}
		if err := db.Exec(mutation.restore, mutation.restoreArgs...).Error; err != nil {
			t.Fatal(err)
		}
	}
	op := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: 2, Kind: "device_renumber", DeviceNumber: "after"}
	for _, identity := range [][2]string{{nativeTenantIdentity("other"), "dep"}, {tenant, "other"}} {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, identity[0], identity[1], op); !errors.Is(err, ErrNativeRegistrationConflict) {
			t.Fatalf("ownership: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", op); err != nil {
			t.Fatal(err)
		}
	}
	changed := op
	changed.DeviceNumber = "tampered"
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", changed); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("digest fence: %v", err)
	}
	changed = op
	changed.EventID = uuid.NewString()
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", changed); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("generation fence: %v", err)
	}
	var owner nativeRuntimeRegistration
	db.First(&owner, "device_id=?", reg.DeviceID)
	if err := db.Exec(`INSERT INTO native_runtime_credential_leases(connection_id,device_id,tenant_id,broker_cell,broker_instance,voucher_sha256,expires_at) VALUES (?,?,?,?,?,?,?)`, "old", reg.DeviceID, "tenant-a", "cell", "broker", owner.VoucherSHA256, time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	disable := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: 3, Kind: "administrative_state", State: "disabled"}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, nativeTenantIdentity("other"), "dep", disable); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("foreign disable: %v", err)
	}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", disable); err == nil {
		t.Fatal("disable succeeded with active broker lease")
	}
	var stopped model.Device
	if err := db.First(&stopped, "id=?", reg.DeviceID).Error; err != nil || stopped.IsEnabled != "disabled" || stopped.Voucher != reg.Voucher {
		t.Fatalf("disable projection/credential: %v", err)
	}
	enable := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: 4, Kind: "administrative_state", State: "enabled"}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", enable); err == nil {
		t.Fatalf("enable overtook pending disable: %v", err)
	}
	if err := db.Exec("UPDATE native_runtime_credential_leases SET revoked_ack_at=now() WHERE connection_id='old'").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", disable); err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", enable); err != nil {
		t.Fatal(err)
	}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", disable); err != nil {
		t.Fatal(err)
	} // Historic replay cannot disable the new generation.
	if err := db.First(&stopped, "id=?", reg.DeviceID).Error; err != nil || stopped.IsEnabled != "enabled" || stopped.Voucher != reg.Voucher {
		t.Fatalf("enable/replay changed credential or state: %v", err)
	}
	op.Generation = enable.Generation
	if err := db.Exec("UPDATE native_runtime_credential_leases SET revoked_ack_at=NULL WHERE connection_id='old'").Error; err != nil {
		t.Fatal(err)
	}
	// Native deletion carries the current resource version (not version+1).
	del := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: op.Generation, Kind: "device_deletion"}
	staleDelete := del
	staleDelete.Generation = op.Generation - 1
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", staleDelete); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("stale deletion accepted: %v", err)
	}
	staleRename := op
	staleRename.EventID = uuid.NewString()
	staleRename.Generation = op.Generation - 1
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", staleRename); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("stale renumber accepted: %v", err)
	}
	if err := db.Exec("CREATE TABLE runtime_history_cascade(device_id text REFERENCES devices(id) ON DELETE CASCADE, value int); CREATE TABLE runtime_history_restrict(device_id text REFERENCES devices(id) ON DELETE RESTRICT, value int)").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"runtime_history_cascade", "runtime_history_restrict"} {
		if err := db.Exec("INSERT INTO "+table+"(device_id,value) VALUES (?,?)", reg.DeviceID, 42).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", del); err == nil {
		t.Fatal("unrevoked lease acknowledged deletion")
	}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", reg); !errors.Is(err, ErrNativeRegistrationConflict) {
		t.Fatalf("deleting projection revived: %v", err)
	}
	var count int64
	db.Model(&model.Device{}).Where("id=?", reg.DeviceID).Count(&count)
	if count != 1 {
		t.Fatal("deleted before lease confirmation")
	}
	if err := db.Exec("UPDATE native_runtime_credential_leases SET revoked_ack_at=now() WHERE connection_id='old'").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", del); err != nil {
			t.Fatal(err)
		}
	}
	db.Model(&model.Device{}).Where("id=?", reg.DeviceID).Count(&count)
	if count != 1 {
		t.Fatal("runtime tombstone lost")
	}
	var device model.Device
	if err := db.First(&device, "id=?", reg.DeviceID).Error; err != nil || device.IsEnabled != "disabled" {
		t.Fatalf("runtime tombstone active: %v", err)
	}
	for _, table := range []string{"runtime_history_cascade", "runtime_history_restrict"} {
		var value int
		if err := db.Table(table).Select("value").Where("device_id=?", reg.DeviceID).Scan(&value).Error; err != nil || value != 42 {
			t.Fatalf("history lost: %s %v", table, err)
		}
	}
	var reservation dal.DeviceIDReservation
	db.First(&reservation, "id=?", reg.DeviceID)
	if reservation.State != "active" || reservation.DeletedAt != nil {
		t.Fatal("retained runtime row lost its active identity reservation")
	}
}

// Use a private database, since startup verifies the actual public schema.
func TestNativeDeletionRetainsRestartSafeIdentity(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_BROKER_DATABASE_DSN to an isolated PostgreSQL test database")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open test PostgreSQL")
	}
	adminSQL, _ := admin.DB()
	t.Cleanup(func() { adminSQL.Close() })
	name := "qa_native_delete_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Error(err)
		}
	})
	privateDSN := dsn
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal(e)
		}
		u.Path = "/" + name
		q := u.Query()
		q.Set("search_path", "public")
		u.RawQuery = q.Encode()
		privateDSN = u.String()
	} else {
		privateDSN += " dbname=" + name + " search_path=public"
	}
	db, err := gorm.Open(postgres.Open(privateDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open private database")
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceStatusHistory{}); err != nil {
		t.Fatal(err)
	}
	if err := initialize.ApplyDeviceIdentitySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE users(tenant_id text); INSERT INTO users VALUES ('tenant-a')").Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"018_native_runtime_registrations.sql", "019_native_runtime_credential_lifecycle.sql", "020_native_credential_leases.sql", "021_native_runtime_lifecycle.sql", "022_native_runtime_configuration.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	tenant := nativeTenantIdentity("tenant-a")
	reg := NativeRuntimeRegistration{DeviceID: uuid.NewString(), CredentialID: uuid.NewString(), EventID: uuid.NewString(), Change: "activation", DeviceNumber: "restart-probe", Voucher: `{"username":"restart-probe"}`}
	if err := RegisterNativeRuntime(ctx, db, tenant, "dep", reg); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE retained_history(device_id varchar(36) REFERENCES devices(id) ON DELETE RESTRICT,value int)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO retained_history VALUES (?,42)", reg.DeviceID).Error; err != nil {
		t.Fatal(err)
	}
	var owner nativeRuntimeRegistration
	db.First(&owner, "device_id=?", reg.DeviceID)
	if err := db.Exec(`INSERT INTO native_runtime_credential_leases(connection_id,device_id,tenant_id,broker_cell,broker_instance,voucher_sha256,expires_at) VALUES ('restart-lease',?,'tenant-a','cell','broker',?,now()+interval '1 hour')`, reg.DeviceID, owner.VoucherSHA256).Error; err != nil {
		t.Fatal(err)
	}
	// Deleting a currently online device clears the projection without inventing
	// a disconnect event; the real broker projector ignores disabled devices.
	lastOffline := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.Model(&model.Device{}).Where("id=?", reg.DeviceID).Updates(map[string]any{"is_online": 1, "activate_flag": "active", "last_offline_time": lastOffline}).Error; err != nil {
		t.Fatal(err)
	}
	deletion := NativeRuntimeLifecycle{DeviceID: reg.DeviceID, EventID: uuid.NewString(), Generation: 1, Kind: "device_deletion"}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", deletion); err == nil {
		t.Fatal("active lease did not fence delete")
	}
	if err := db.Exec("UPDATE native_runtime_credential_leases SET revoked_ack_at=now()").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyNativeRuntimeLifecycle(ctx, db, tenant, "dep", deletion); err != nil {
		t.Fatal(err)
	}
	// Check the product result before any test-only mutation.
	var retained dal.DeviceIDReservation
	if err := db.First(&retained, "id=?", reg.DeviceID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.State != "active" || retained.DeletedAt != nil {
		t.Fatal("native deletion corrupted retained-row identity")
	}
	if err := initialize.ApplyDeviceIdentitySchema(db); err != nil {
		t.Fatalf("unmodified deletion result prevents startup: %v", err)
	}
	// Reproduce the old corruption and prove startup still rejects it.
	if err := db.Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", reg.DeviceID).Error; err != nil {
		t.Fatal(err)
	}
	if err := initialize.ApplyDeviceIdentitySchema(db); err == nil {
		t.Fatal("startup accepted retained row with tombstoned identity")
	}
	if err := db.Exec("UPDATE device_id_reservations SET state='active',deleted_at=NULL WHERE id=?", reg.DeviceID).Error; err != nil {
		t.Fatal(err)
	}
	// Exact application startup schema verification, including ledger and trigger.
	for i := 0; i < 2; i++ {
		if err := initialize.ApplyDeviceIdentitySchema(db); err != nil {
			t.Fatalf("restart after native deletion: %v", err)
		}
	}
	var device model.Device
	db.First(&device, "id=?", reg.DeviceID)
	if device.IsOnline != 0 || device.LastOfflineTime == nil || !device.LastOfflineTime.Equal(lastOffline) {
		t.Fatal("deleted runtime retained online state or fabricated a disconnect timestamp")
	}
	var onlineCount int64
	// Same predicate used by the legacy board's tenant online count.
	if err := db.Model(&model.Device{}).Where("tenant_id=? AND activate_flag='active' AND is_online=1", "tenant-a").Count(&onlineCount).Error; err != nil || onlineCount != 0 {
		t.Fatalf("legacy online statistic includes deleted projection: count=%d err=%v", onlineCount, err)
	}
	var statusEvents int64
	if err := db.Model(&model.DeviceStatusHistory{}).Where("device_id=?", reg.DeviceID).Count(&statusEvents).Error; err != nil || statusEvents != 0 {
		t.Fatalf("administrative removal emitted transport status history: count=%d err=%v", statusEvents, err)
	}
	if device.IsEnabled != "disabled" || device.Voucher == reg.Voucher {
		t.Fatal("old credential or device still enabled")
	}
	db.First(&owner, "device_id=?", reg.DeviceID)
	if owner.Active || !owner.Deleting {
		t.Fatal("deleted owner retained access")
	}
	if _, err := ConfirmNativeRuntimeActivation(ctx, db, tenant, "dep", reg); err == nil {
		t.Fatal("deleted identity activation confirmed")
	}
	for _, scope := range []string{tenant, nativeTenantIdentity("other")} {
		reused := reg
		reused.EventID, reused.CredentialID = uuid.NewString(), uuid.NewString()
		if err := RegisterNativeRuntime(ctx, db, scope, "dep", reused); err == nil {
			t.Fatal("deleted identity was reused")
		}
	}
	var count int64
	db.Table("retained_history").Where("device_id=? AND value=42", reg.DeviceID).Count(&count)
	if count != 1 {
		t.Fatal("history lost")
	}
	var reservation dal.DeviceIDReservation
	db.First(&reservation, "id=?", reg.DeviceID)
	if reservation.State != "active" || reservation.DeletedAt != nil {
		t.Fatal("physical row identity inconsistent")
	}
}

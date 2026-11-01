package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

type deleteCommitReplyLossPool struct {
	*sql.DB
	commits atomic.Int32
}

func (p *deleteCommitReplyLossPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &deleteCommitReplyLossTx{Tx: tx, pool: p}, nil
}

type deleteCommitReplyLossTx struct {
	*sql.Tx
	pool *deleteCommitReplyLossPool
}

func (tx *deleteCommitReplyLossTx) Commit() error {
	tx.pool.commits.Add(1)
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return errors.New("injected lost commit response")
}

func TestDeviceDeleteTenantAndRollbackPostgres(t *testing.T) {
	redisAddr := os.Getenv("TEST_DEVICE_DELETE_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("isolated Redis address not configured")
	}
	db := brokerEvidenceTestDB(t)
	if err := db.AutoMigrate(&model.DeviceTriggerCondition{}, &model.TelemetryCurrentData{}, &model.TelemetryData{}, &model.TelemetrySetLog{}, &model.AttributeData{}, &model.AttributeSetLog{}, &model.EventData{}, &model.CommandSetLog{}); err != nil {
		t.Fatal(err)
	}
	priorDB, priorRedis, priorQ := global.DB, global.REDIS, *query.Q
	query.SetDefault(db)
	global.DB = db
	global.REDIS = redis.NewClient(&redis.Options{Addr: redisAddr})
	client := global.REDIS
	t.Cleanup(func() { _ = client.Close(); global.DB, global.REDIS, *query.Q = priorDB, priorRedis, priorQ })
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	deviceID, voucher := "t03-"+suffix, "voucher-"+suffix
	unknownID, unknownVoucher := "t03u-"+suffix, "voucher-u-"+suffix
	t.Cleanup(func() { _ = client.Del(context.Background(), deviceID, voucher, unknownID, unknownVoucher).Err() })
	device := model.Device{ID: deviceID, TenantID: "tenant-a", DeviceNumber: "num-" + suffix, IsEnabled: "enabled", ActivateFlag: "active", Voucher: voucher}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{deviceID, voucher} {
		if err := client.Set(ctx, key, "retained", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	for _, row := range []any{
		&model.TelemetryCurrentData{DeviceID: deviceID, Key: "temp", T: now},
		&model.TelemetryData{DeviceID: deviceID, Key: "temp", T: now.UnixMilli()},
		&model.TelemetrySetLog{ID: "tel-log", DeviceID: deviceID, CreatedAt: now},
		&model.AttributeData{ID: "attr", DeviceID: deviceID, Key: "power", T: now},
		&model.AttributeSetLog{ID: "attr-log", DeviceID: deviceID, CreatedAt: now},
		&model.EventData{ID: "event", DeviceID: deviceID, Identify: "event", T: now},
		&model.CommandSetLog{ID: "command-log", DeviceID: deviceID, CreatedAt: now},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}
	tables := []string{"devices", "telemetry_current_datas", "telemetry_datas", "telemetry_set_logs", "attribute_datas", "attribute_set_logs", "event_datas", "command_set_logs"}
	assertRows := func(want int64) {
		t.Helper()
		for _, table := range tables {
			field := "device_id"
			if table == "devices" {
				field = "id"
			}
			var count int64
			if err := db.Table(table).Where(field+" = ?", deviceID).Count(&count).Error; err != nil || count != want {
				t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
			}
		}
	}
	assertCache := func(want int64) {
		t.Helper()
		n, err := client.Exists(ctx, deviceID, voucher).Result()
		if err != nil || n != want {
			t.Fatalf("cache keys=%d want=%d err=%v", n, want, err)
		}
	}
	var code *errcode.Error
	if err := (&Device{}).DeleteDevice(deviceID, &utils.UserClaims{ID: "b", TenantID: "tenant-b"}); !errors.As(err, &code) || code.Code != errcode.CodeNotFound {
		t.Fatalf("cross-tenant deletion: %v", err)
	}
	assertRows(1)
	assertCache(2)
	if err := db.Exec(`CREATE FUNCTION reject_event_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected delete failure'; END $$; CREATE TRIGGER reject_event_delete BEFORE DELETE ON event_datas FOR EACH ROW EXECUTE FUNCTION reject_event_delete();`).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&Device{}).DeleteDevice(deviceID, &utils.UserClaims{ID: "a", TenantID: "tenant-a"}); err == nil {
		t.Fatal("injected failure was accepted")
	}
	assertRows(1)
	assertCache(2)
	if err := db.Exec(`DROP TRIGGER reject_event_delete ON event_datas`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE SEQUENCE suppress_hit; CREATE FUNCTION suppress_device_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('suppress_hit'); RETURN NULL; END $$; CREATE TRIGGER suppress_device_delete BEFORE DELETE ON devices FOR EACH ROW EXECUTE FUNCTION suppress_device_delete();`).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&Device{}).DeleteDevice(deviceID, &utils.UserClaims{ID: "a", TenantID: "tenant-a"}); !errors.As(err, &code) || code.Code != errcode.CodeNotFound {
		t.Fatalf("zero-row deletion: %v", err)
	}
	assertRows(1)
	assertCache(2)
	var triggerHit bool
	if err := db.Raw(`SELECT is_called FROM suppress_hit`).Scan(&triggerHit).Error; err != nil || !triggerHit {
		t.Fatalf("zero-row trigger not reached: called=%v err=%v", triggerHit, err)
	}
	if err := db.Exec(`DROP TRIGGER suppress_device_delete ON devices`).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&Device{}).DeleteDevice(deviceID, &utils.UserClaims{ID: "a", TenantID: "tenant-a"}); err != nil {
		t.Fatalf("authorized delete: %v", err)
	}
	assertRows(0)
	assertCache(0)

	// The database commits, then the driver returns an error as if the response
	// were lost. This is an injection, not a real network partition or recovery.
	unknownDevice := model.Device{ID: unknownID, TenantID: "tenant-a", DeviceNumber: "num-u-" + suffix, IsEnabled: "enabled", ActivateFlag: "active", Voucher: unknownVoucher}
	if err := db.Create(&unknownDevice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TelemetryData{DeviceID: unknownID, Key: "temp", T: now.UnixMilli()}).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{unknownID, unknownVoucher} {
		if err := client.Set(ctx, key, "retained", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool := &deleteCommitReplyLossPool{DB: sqlDB}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	global.DB = wrapped
	deleteErr := (&Device{}).DeleteDevice(unknownID, &utils.UserClaims{ID: "a", TenantID: "tenant-a"})
	global.DB = db
	if !errors.As(deleteErr, &code) || code.Code != errcode.CodeDBError || !strings.Contains(code.CustomMsg, "unknown") {
		t.Fatalf("commit outcome error: %v", deleteErr)
	}
	if pool.commits.Load() != 1 {
		t.Fatalf("commit attempts=%d", pool.commits.Load())
	}
	var count int64
	if err := db.Model(&model.Device{}).Where("id=?", unknownID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("committed device count=%d err=%v", count, err)
	}
	if n, err := client.Exists(ctx, unknownID, unknownVoucher).Result(); err != nil || n != 2 {
		t.Fatalf("unknown commit cleared cache keys=%d err=%v", n, err)
	}
}

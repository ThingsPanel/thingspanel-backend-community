package service

import (
	"errors"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
	"strings"
	"testing"
	"time"
)

func TestDevicePermanentIdentityPostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	// These cases exercise production reservation creation; the fixture
	// callback must not manufacture a reservation on the service's behalf.
	if err := db.Callback().Create().Remove("fixture-device-reservation"); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Group{}, &model.RGroupDevice{}); err != nil {
		t.Fatal(err)
	}
	claims := &utils.UserClaims{ID: "identity-owner", TenantID: "tenant-a"}
	next := func() string { return "identity-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24] }
	create := func(id string, claims *utils.UserClaims) error {
		_, err := (&Device{}).CreateDevice(model.CreateDeviceReq{ID: &id}, claims)
		return err
	}
	state := func(id string) (string, int64) {
		t.Helper()
		var r dal.DeviceIDReservation
		if err := db.Where("id=?", id).Take(&r).Error; err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := db.Model(&model.Device{}).Where("id=?", id).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return r.State, n
	}
	t.Run("custom_new_id_works_deleted_id_never_reused", func(t *testing.T) {
		id := next()
		if err := create(id, claims); err != nil {
			t.Fatal(err)
		}
		if status, n := state(id); status != "active" || n != 1 {
			t.Fatalf("creation not atomic: %s %d", status, n)
		}
		if err := (&Device{}).DeleteDevice(id, claims); err != nil {
			t.Fatal(err)
		}
		if status, n := state(id); status != "tombstoned" || n != 0 {
			t.Fatalf("delete not atomic: %s %d", status, n)
		}
		for _, owner := range []*utils.UserClaims{claims, {ID: "other", TenantID: "tenant-b"}} {
			err := create(id, owner)
			var code *errcode.Error
			if !errors.As(err, &code) || code.Code != errcode.CodeOpDenied || !strings.Contains(code.CustomMsg, "permanently reserved") {
				t.Fatalf("deleted ID reuse not explicit permanent conflict: %v", err)
			}
		}
	})
	t.Run("concurrent_same_id_has_one_owner", func(t *testing.T) {
		id := next()
		done := make(chan error, 2)
		go func() { done <- create(id, claims) }()
		go func() { done <- create(id, &utils.UserClaims{ID: "other", TenantID: "tenant-b"}) }()
		successes := 0
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err == nil {
					successes++
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ID reservation contention did not finish")
			}
		}
		if successes != 1 {
			t.Fatalf("same ID creation successes=%d", successes)
		}
		var reservation dal.DeviceIDReservation
		var device model.Device
		if err := db.Where("id=?", id).Take(&reservation).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("id=?", id).Take(&device).Error; err != nil {
			t.Fatal(err)
		}
		if reservation.State != "active" || reservation.TenantID != device.TenantID {
			t.Fatal("reservation and device ownership diverged")
		}
	})
	t.Run("batch_conflict_rolls_back_prior_reservations", func(t *testing.T) {
		oldID := next()
		if err := create(oldID, claims); err != nil {
			t.Fatal(err)
		}
		if err := (&Device{}).DeleteDevice(oldID, claims); err != nil {
			t.Fatal(err)
		}
		newID := "00000000-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
		makeDevice := func(id string) *model.Device {
			return &model.Device{ID: id, TenantID: claims.TenantID, Voucher: id, DeviceNumber: id, IsEnabled: "enabled", ActivateFlag: "active"}
		}
		if err := dal.CreateDeviceBatch([]*model.Device{makeDevice(oldID), makeDevice(newID)}); !errors.Is(err, dal.ErrDeviceIDReserved) {
			t.Fatalf("batch conflict=%v", err)
		}
		for _, table := range []string{"devices", "device_id_reservations"} {
			var n int64
			if err := db.Table(table).Where("id=?", newID).Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("partial batch %s count=%d err=%v", table, n, err)
			}
		}
	})
	t.Run("create_commit_reply_loss_preserves_atomic_identity", func(t *testing.T) {
		id := next()
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &parentCommitLossPool{DB: pool}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		global.DB = wrapped
		err = create(id, claims)
		global.DB = db
		var code *errcode.Error
		if !errors.As(err, &code) || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 1 {
			t.Fatalf("commit unknown=%v commits=%d", err, loss.commits.Load())
		}
		if status, n := state(id); status != "active" || n != 1 {
			t.Fatalf("unknown creation lost identity: %s %d", status, n)
		}
	})
	t.Run("delete_commit_reply_loss_preserves_tombstone", func(t *testing.T) {
		id := next()
		if err := create(id, claims); err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &deleteCommitReplyLossPool{DB: pool}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		global.DB = wrapped
		err = (&Device{}).DeleteDevice(id, claims)
		global.DB = db
		var code *errcode.Error
		if !errors.As(err, &code) || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 1 {
			t.Fatalf("delete commit unknown=%v commits=%d", err, loss.commits.Load())
		}
		if status, n := state(id); status != "tombstoned" || n != 0 {
			t.Fatalf("unknown deletion lost tombstone: %s %d", status, n)
		}
		if err := create(id, claims); err == nil {
			t.Fatal("unknown delete allowed identity reuse")
		}
	})
	t.Run("missing_reservation_fails_delete_without_partial_data_loss", func(t *testing.T) {
		id := next()
		if err := create(id, claims); err != nil {
			t.Fatal(err)
		}
		tenant := claims.TenantID
		number := float64(12)
		if err := db.Create(&model.TelemetryCurrentData{DeviceID: id, TenantID: &tenant, Key: "retained", T: time.Now().UTC(), NumberV: &number}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("id=?", id).Delete(&dal.DeviceIDReservation{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := (&Device{}).DeleteDevice(id, claims); err == nil {
			t.Fatal("missing identity silently synthesized")
		}
		var telemetry int64
		if err := db.Model(&model.TelemetryCurrentData{}).Where("device_id=?", id).Count(&telemetry).Error; err != nil || telemetry != 1 {
			t.Fatalf("failed tombstone lost earlier-deleted telemetry: %d %v", telemetry, err)
		}
		var n int64
		if err := db.Model(&model.Device{}).Where("id=?", id).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("identity failure partially deleted device: %d %v", n, err)
		}
	})
	t.Run("reverse_order_batch_creation_serializes_ids", func(t *testing.T) {
		a, b := next(), next()
		makeDevice := func(id string) *model.Device {
			return &model.Device{ID: id, TenantID: claims.TenantID, Voucher: id, DeviceNumber: id, IsEnabled: "enabled", ActivateFlag: "active"}
		}
		done := make(chan error, 2)
		go func() { done <- dal.CreateDeviceBatch([]*model.Device{makeDevice(a), makeDevice(b)}) }()
		go func() { done <- dal.CreateDeviceBatch([]*model.Device{makeDevice(b), makeDevice(a)}) }()
		successes := 0
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err == nil {
					successes++
				} else if !errors.Is(err, dal.ErrDeviceIDReserved) {
					t.Fatalf("batch reservation error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("reverse batch ID locks deadlocked")
			}
		}
		if successes != 1 {
			t.Fatalf("reverse batch successes=%d", successes)
		}
		for _, table := range []string{"devices", "device_id_reservations"} {
			var n int64
			if err := db.Table(table).Where("id IN ?", []string{a, b}).Count(&n).Error; err != nil || n != 2 {
				t.Fatalf("batch identity table=%s count=%d err=%v", table, n, err)
			}
		}
	})
	t.Run("creation_rollback_reply_loss_does_not_leave_reservation", func(t *testing.T) {
		id := next()
		if err := db.Exec(`CREATE FUNCTION reject_identity_create() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='` + id + `' THEN RAISE EXCEPTION 'injected identity failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_identity BEFORE INSERT ON devices FOR EACH ROW EXECUTE FUNCTION reject_identity_create();`).Error; err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		loss := &parentCommitLossPool{DB: pool, rollbackLoss: true}
		wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
		if err != nil {
			t.Fatal(err)
		}
		global.DB = wrapped
		err = create(id, claims)
		global.DB = db
		if dropErr := db.Exec(`DROP TRIGGER reject_identity ON devices; DROP FUNCTION reject_identity_create();`).Error; dropErr != nil {
			t.Fatal(dropErr)
		}
		var code *errcode.Error
		if !errors.As(err, &code) || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 0 {
			t.Fatalf("rollback unknown=%v commits=%d", err, loss.commits.Load())
		}
		for _, table := range []string{"devices", "device_id_reservations"} {
			var n int64
			if err := db.Table(table).Where("id=?", id).Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("rollback left %s count=%d err=%v", table, n, err)
			}
		}
	})

}

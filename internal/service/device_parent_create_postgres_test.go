package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

type parentCommitLossPool struct {
	*sql.DB
	commits      atomic.Int32
	rollbackLoss bool
}

func (p *parentCommitLossPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &parentCommitLossTx{Tx: tx, pool: p}, nil
}

type parentCommitLossTx struct {
	*sql.Tx
	pool *parentCommitLossPool
}

func (tx *parentCommitLossTx) Commit() error {
	tx.pool.commits.Add(1)
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return errors.New("injected lost commit reply")
}
func (tx *parentCommitLossTx) Rollback() error {
	err := tx.Tx.Rollback()
	if err != nil {
		return err
	}
	if tx.pool.rollbackLoss {
		return errors.New("injected lost rollback reply")
	}
	return nil
}

func TestCreateDeviceParentFencePostgres(t *testing.T) {
	dsn := os.Getenv("PARENT_CREATE_TEST_DSN")
	if dsn == "" {
		t.Skip("PARENT_CREATE_TEST_DSN not supplied")
	}
	fixtureURL, parseErr := url.Parse(dsn)
	if parseErr != nil || (fixtureURL.Scheme != "postgres" && fixtureURL.Scheme != "postgresql") {
		t.Fatal("parent fixture requires an explicit PostgreSQL URL")
	}
	host := fixtureURL.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("refusing non-loopback parent fixture host")
	}
	// libpq URL query parameters can override the authority; disallow every
	// connection-target override before opening any connection.
	for _, key := range []string{"host", "hostaddr", "port", "dbname", "database", "user", "username", "service", "servicefile"} {
		if _, exists := fixtureURL.Query()[key]; exists {
			t.Fatal("refusing parent fixture connection-target override")
		}
	}
	if fixtureURL.User == nil || fixtureURL.User.Username() != "parent_create_test" || fixtureURL.Path != "/parent_create_test" {
		t.Fatal("parent fixture requires its dedicated database and user")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open fixture")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminPool.Close() })
	var database, user string
	if err := admin.Raw("SELECT current_database(), current_user").Row().Scan(&database, &user); err != nil || database != "parent_create_test" || user != "parent_create_test" {
		t.Fatal("fixture identity mismatch")
	}
	schema := "parent_create_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; err != nil {
			t.Error("schema cleanup failed")
		}
	})
	u, _ := url.Parse(dsn)
	opts := u.Query()
	opts.Set("options", "-c search_path="+schema)
	u.RawQuery = opts.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Device{}, &model.Group{}, &model.RGroupDevice{}); err != nil {
		t.Fatal(err)
	}
	oldQ, oldDevice, oldDB := *query.Q, query.Device, global.DB
	global.DB = db
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	t.Cleanup(func() { *query.Q = oldQ; query.Device = oldDevice; global.DB = oldDB })
	for _, d := range []model.Device{{ID: "parent-a", TenantID: "tenant-a", Voucher: "parent-a", DeviceNumber: "parent-a", IsEnabled: "enabled", ActivateFlag: "active"}, {ID: "parent-del", TenantID: "tenant-a", Voucher: "parent-del", DeviceNumber: "parent-del", IsEnabled: "enabled", ActivateFlag: "active"}, {ID: "parent-reverse", TenantID: "tenant-a", Voucher: "parent-reverse", DeviceNumber: "parent-reverse", IsEnabled: "enabled", ActivateFlag: "active"}, {ID: "parent-b", TenantID: "tenant-b", Voucher: "parent-b", DeviceNumber: "parent-b", IsEnabled: "enabled", ActivateFlag: "active"}} {
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	root := "0"
	if err := db.Create(&model.Group{ID: "root-a", ParentID: &root, Name: "root", TenantID: "tenant-a"}).Error; err != nil {
		t.Fatal(err)
	}
	claims := &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}
	name := "child"
	create := func(id, parent string) error {
		device, e := (&Device{}).CreateDevice(model.CreateDeviceReq{ID: &id, Name: &name, ParentID: &parent}, claims)
		if e != nil && !reflect.DeepEqual(device, model.Device{}) {
			return errors.New("error response returned nonzero device")
		}
		return e
	}
	assertCode := func(err error, want int) {
		t.Helper()
		var e *errcode.Error
		if !errors.As(err, &e) || e.Code != want {
			t.Fatalf("error=%v want code=%d", err, want)
		}
	}
	assertCode(create("child-bad", "parent-b"), errcode.CodeNoPermission)
	assertCode(create("child-miss", "missing"), errcode.CodeNoPermission)
	assertCode(create("child-empty", ""), errcode.CodeParamError)
	assertCode(create("child-self", "child-self"), errcode.CodeParamError)
	if _, err := (&Device{}).CreateDevice(model.CreateDeviceReq{Name: &name}, nil); err == nil {
		t.Fatal("missing identity accepted")
	}
	if err := create("child-good", "parent-a"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION fixture_sensitive_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='child-leak' THEN RAISE EXCEPTION 'fixture-sensitive-secret'; END IF; RETURN NEW; END $$; CREATE TRIGGER fixture_sensitive BEFORE INSERT ON devices FOR EACH ROW EXECUTE FUNCTION fixture_sensitive_failure()`).Error; err != nil {
		t.Fatal(err)
	}
	failure := create("child-leak", "parent-a")
	var failureCode *errcode.Error
	if !errors.As(failure, &failureCode) || failureCode.Code != errcode.CodeDBError || failureCode.Data != nil {
		t.Fatalf("sensitive database error was exposed: %v", failure)
	}
	encodedFailure, err := json.Marshal(failureCode)
	if err != nil || strings.Contains(string(encodedFailure), "fixture-sensitive-secret") || strings.Contains(failureCode.CustomMsg, "fixture-sensitive-secret") {
		t.Fatalf("sensitive failure content exposed: %s err=%v", encodedFailure, err)
	}
	if err := db.Exec(`DROP TRIGGER fixture_sensitive ON devices`).Error; err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Table("r_group_device").Where("device_id=? AND group_id=?", "child-good", "root-a").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("group count=%d err=%v", n, err)
	}

	waitLocks := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			var n int64
			if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid <> pg_backend_pid()`).Scan(&n).Error; err != nil {
				t.Fatal(err)
			}
			if n >= want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("expected real PostgreSQL lock wait")
	}
	// Equivalent delete fixture, not the separate T03 service implementation.
	deleteTx := db.Begin()
	if deleteTx.Error != nil {
		t.Fatal(deleteTx.Error)
	}
	defer deleteTx.Rollback()
	if err := deleteTx.Exec(`SELECT id FROM devices WHERE id=? FOR UPDATE`, "parent-del").Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- create("child-after-delete", "parent-del") }()
	waitLocks(1)
	if err := deleteTx.Exec(`DELETE FROM devices WHERE id=?`, "parent-del").Error; err != nil {
		t.Fatal(err)
	}
	if err := deleteTx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	assertCode(<-done, errcode.CodeNoPermission)
	// Block the real create inside INSERT after its parent row lock has been acquired.
	if err := db.Exec(`CREATE FUNCTION fixture_pause_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='child-paused' THEN PERFORM pg_advisory_xact_lock(77123456); END IF; RETURN NEW; END $$; CREATE TRIGGER fixture_pause BEFORE INSERT ON devices FOR EACH ROW EXECUTE FUNCTION fixture_pause_insert()`).Error; err != nil {
		t.Fatal(err)
	}
	gate := db.Begin()
	defer gate.Rollback()
	if err := gate.Exec(`SELECT pg_advisory_xact_lock(77123456)`).Error; err != nil {
		t.Fatal(err)
	}
	go func() { done <- create("child-paused", "parent-reverse") }()
	waitLocks(1)
	deleteDone := make(chan int64, 1)
	go func() {
		tx := db.Begin()
		defer tx.Rollback()
		if err := tx.Exec(`SELECT id FROM devices WHERE id=? FOR UPDATE`, "parent-reverse").Error; err != nil {
			deleteDone <- -1
			return
		}
		var count int64
		if err := tx.Table("devices").Where("parent_id=? AND id=?", "parent-reverse", "child-paused").Count(&count).Error; err != nil {
			deleteDone <- -1
			return
		}
		deleteDone <- count
	}()
	waitLocks(2)
	if err := gate.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if count := <-deleteDone; count != 1 {
		t.Fatalf("committed child count=%d", count)
	}

	loss := &parentCommitLossPool{DB: pool}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	global.DB = wrapped
	err = create("child-unknown", "parent-a")
	global.DB = db
	var code *errcode.Error
	if !errors.As(err, &code) || code.Code != errcode.CodeDBError || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 1 {
		t.Fatalf("commit unknown error=%v commits=%d", err, loss.commits.Load())
	}
	if err := db.Table("devices").Where("id=?", "child-unknown").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("unknown commit persisted=%d err=%v", n, err)
	}
	if err := db.Table("r_group_device").Where("device_id=? AND group_id=?", "child-unknown", "root-a").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("unknown commit group=%d err=%v", n, err)
	}
	loss.rollbackLoss = true
	global.DB = wrapped
	err = create("child-rollback", "missing")
	global.DB = db
	if !errors.As(err, &code) || code.Code != errcode.CodeDBError || !strings.Contains(code.CustomMsg, "unknown") || loss.commits.Load() != 1 {
		t.Fatalf("rollback unknown error=%v commits=%d", err, loss.commits.Load())
	}
}

// Exercises the actual T03 DeleteDevice service and ordinary CreateDevice.
// Existing-child binding and reparenting are covered by device_parent_bind_postgres_test.go.
func TestCreateDeviceAgainstRealDeletePostgres(t *testing.T) {
	db := setupSceneDeleteBarrierDB(t)
	useSceneDeleteBarrierDB(t, db)
	if err := db.AutoMigrate(&model.Group{}, &model.RGroupDevice{}); err != nil {
		t.Fatal("migrate isolated group fixture")
	}
	claims := &utils.UserClaims{ID: "parent-owner", TenantID: "tenant-a"}
	createFirst := "parent-create-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	deleteFirst := "parent-delete-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	sceneDeleteFixtureDevice(t, db, claims.TenantID, createFirst)
	sceneDeleteFixtureDevice(t, db, claims.TenantID, deleteFirst)
	name := "child"
	create := func(parent string, done chan<- error) {
		id := "child-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
		_, err := (&Device{}).CreateDevice(model.CreateDeviceReq{ID: &id, ParentID: &parent, Name: &name}, claims)
		done <- err
	}
	created := make(chan error, 1)
	deleted := make(chan error, 1)
	gate := installSceneDeleteAdvisoryGate(t, db, "devices", "BEFORE INSERT", "parent_insert_gate", "block_parent_insert", nextSceneDeleteAdvisoryKey())
	go create(createFirst, created)
	blocker := waitForSceneDeleteGate(t, db, "devices")
	go func() { deleted <- (&Device{}).DeleteDevice(createFirst, claims) }()
	waitForSceneDeviceLockWait(t, db, blocker)
	gate.release(t)
	if err := <-created; err != nil {
		t.Fatalf("create before delete: %v", err)
	}
	assertSceneErrorCode(t, <-deleted, 200063)
	gate = installSceneDeleteAdvisoryGate(t, db, "devices", "BEFORE DELETE", "parent_delete_gate", "block_parent_delete", nextSceneDeleteAdvisoryKey())
	go func() { deleted <- (&Device{}).DeleteDevice(deleteFirst, claims) }()
	blocker = waitForSceneDeleteGate(t, db, "devices")
	go create(deleteFirst, created)
	waitForSceneDeviceLockWait(t, db, blocker)
	gate.release(t)
	if err := <-deleted; err != nil {
		t.Fatalf("delete before create: %v", err)
	}
	assertSceneErrorCode(t, <-created, errcode.CodeNoPermission)
	var count int64
	if err := db.Model(&model.Device{}).Where("parent_id = ?", deleteFirst).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("child after delete count=%d err=%v", count, err)
	}
}

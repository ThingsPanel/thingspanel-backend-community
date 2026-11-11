package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

type apiKeyReplyLossPool struct {
	*sql.DB
	commitCount  atomic.Int32
	execCount    atomic.Int32
	loseRollback bool
}

func (p *apiKeyReplyLossPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &apiKeyReplyLossTx{Tx: tx, pool: p}, nil
}

type apiKeyReplyLossTx struct {
	*sql.Tx
	pool *apiKeyReplyLossPool
}

func (tx *apiKeyReplyLossTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	tx.pool.execCount.Add(1)
	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx *apiKeyReplyLossTx) Commit() error {
	tx.pool.commitCount.Add(1)
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return errors.New("synthetic lost commit reply")
}
func (tx *apiKeyReplyLossTx) Rollback() error {
	if err := tx.Tx.Rollback(); err != nil {
		return err
	}
	if tx.pool.loseRollback {
		return errors.New("synthetic sensitive rollback reply")
	}
	return nil
}

func TestAPIKeyWriteScopePostgres(t *testing.T) {
	const fixture = "postgres://apikey_write_test:fixture-only-password@127.0.0.1:25673/apikey_write_test?sslmode=disable"
	dsn := os.Getenv("APIKEY_WRITE_TEST_DSN")
	if dsn == "" {
		t.Skip("APIKEY_WRITE_TEST_DSN not supplied")
	}
	if dsn != fixture {
		t.Fatal("refusing non-fixture DSN")
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
	if err := admin.Raw("SELECT current_database(), current_user").Row().Scan(&database, &user); err != nil || database != "apikey_write_test" || user != "apikey_write_test" {
		t.Fatal("fixture identity mismatch")
	}
	schema := "apikey_write_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; err != nil {
			t.Error("fixture cleanup failed")
		}
	})
	u, _ := url.Parse(dsn)
	args := u.Query()
	args.Set("options", "-c search_path="+schema)
	u.RawQuery = args.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open fixture schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec(`CREATE TABLE open_api_keys (id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, api_key varchar(200) NOT NULL UNIQUE, status int2 NULL, name varchar(200) NOT NULL, created_at timestamptz, updated_at timestamptz, created_id varchar(50))`).Error; err != nil {
		t.Fatal("create fixture table")
	}
	oldDB, oldRedis := global.DB, global.REDIS
	global.DB, global.REDIS = db, nil
	t.Cleanup(func() { global.DB, global.REDIS = oldDB, oldRedis })
	for _, v := range []struct{ id, tenant, key string }{{"key-a", "tenant-a", "fixture-secret-a"}, {"key-b", "tenant-b", "fixture-secret-b"}, {"key-lock", "tenant-a", "fixture-secret-lock"}, {"key-zero", "tenant-a", "fixture-secret-zero"}, {"key-delete-zero", "tenant-a", "fixture-secret-delete-zero"}, {"key-unknown", "tenant-a", "fixture-secret-unknown"}, {"key-delete-unknown", "tenant-a", "fixture-secret-delete-unknown"}} {
		if err := db.Exec(`INSERT INTO open_api_keys(id,tenant_id,api_key,status,name,created_id,updated_at) VALUES (?,?,?,1,'original','creator',?)`, v.id, v.tenant, v.key, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)).Error; err != nil {
			t.Fatal("seed fixture key")
		}
	}
	owner := &utils.UserClaims{ID: "owner", TenantID: "tenant-a", Authority: "TENANT_ADMIN"}
	adminClaims := &utils.UserClaims{ID: "root", Authority: "SYS_ADMIN"}
	userClaims := &utils.UserClaims{ID: "member", TenantID: "tenant-a", Authority: "TENANT_USER"}
	zero := int16(0)
	name := "changed"
	svc := &OpenAPIKey{}
	assertCode := func(err error, want int) {
		t.Helper()
		var code *errcode.Error
		if !errors.As(err, &code) || code.Code != want || code.Data != nil {
			t.Fatalf("unexpected error=%v want=%d", err, want)
		}
	}
	req := func(id string) *model.UpdateOpenAPIKeyReq { return &model.UpdateOpenAPIKeyReq{ID: id, Status: &zero} }
	// A nil DB proves direct service callers receive the parameter error before
	// reaching the database; ordinary empty updates remain permitted.
	global.DB = nil
	for _, invalid := range []int16{-1, 2} {
		assertCode(svc.UpdateOpenAPIKey(context.Background(), &model.UpdateOpenAPIKeyReq{ID: "key-a", Status: &invalid}, owner), errcode.CodeParamError)
	}
	global.DB = db
	for _, tc := range []struct {
		id     string
		claims *utils.UserClaims
	}{{"key-b", owner}, {"missing", owner}, {"key-a", nil}, {"key-a", userClaims}, {"key-a", &utils.UserClaims{ID: "owner", Authority: "TENANT_ADMIN"}}, {"key-a", &utils.UserClaims{TenantID: "tenant-a", Authority: "TENANT_ADMIN"}}} {
		assertCode(svc.UpdateOpenAPIKey(context.Background(), req(tc.id), tc.claims), errcode.CodeNoPermission)
		assertCode(svc.DeleteOpenAPIKey(context.Background(), tc.id, tc.claims), errcode.CodeNoPermission)
	}
	if err := svc.UpdateOpenAPIKey(context.Background(), &model.UpdateOpenAPIKeyReq{ID: "key-a", Status: &zero, Name: &name}, owner); err != nil {
		t.Fatal(err)
	}
	var stored model.OpenAPIKey
	if err := db.Where("id=?", "key-a").First(&stored).Error; err != nil || stored.Status == nil || *stored.Status != 0 || stored.Name != name {
		t.Fatalf("status/name update err=%v", err)
	}
	before := *stored.UpdatedAt
	if err := svc.UpdateOpenAPIKey(context.Background(), req("key-a"), owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id=?", "key-a").First(&stored).Error; err != nil || !stored.UpdatedAt.Equal(before) {
		t.Fatal("repeat revoke changed timestamp or failed")
	}
	if _, err := dal.LookupActiveOpenAPIKey(context.Background(), db, "fixture-secret-a"); err == nil {
		t.Fatal("revoked key authenticated")
	}
	if err := svc.DeleteOpenAPIKey(context.Background(), "key-b", adminClaims); err != nil {
		t.Fatal(err)
	}
	assertCode(svc.DeleteOpenAPIKey(context.Background(), "key-b", adminClaims), errcode.CodeNoPermission)
	if err := db.Exec(`CREATE FUNCTION fixture_zero_key_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='key-zero' THEN RETURN NULL; END IF; RETURN NEW; END $$; CREATE TRIGGER fixture_zero BEFORE UPDATE ON open_api_keys FOR EACH ROW EXECUTE FUNCTION fixture_zero_key_update()`).Error; err != nil {
		t.Fatal(err)
	}
	assertCode(svc.UpdateOpenAPIKey(context.Background(), req("key-zero"), owner), errcode.CodeNoPermission)
	if _, err := dal.LookupActiveOpenAPIKey(context.Background(), db, "fixture-secret-zero"); err != nil {
		t.Fatal("zero row update unexpectedly changed key")
	}
	if err := db.Exec(`CREATE FUNCTION fixture_zero_key_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='key-delete-zero' THEN RETURN NULL; END IF; RETURN OLD; END $$; CREATE TRIGGER fixture_zero_delete BEFORE DELETE ON open_api_keys FOR EACH ROW EXECUTE FUNCTION fixture_zero_key_delete()`).Error; err != nil {
		t.Fatal(err)
	}
	assertCode(svc.DeleteOpenAPIKey(context.Background(), "key-delete-zero", owner), errcode.CodeNoPermission)
	if _, err := dal.LookupActiveOpenAPIKey(context.Background(), db, "fixture-secret-delete-zero"); err != nil {
		t.Fatal("zero row delete unexpectedly removed key")
	}
	lockTx := db.Begin()
	if lockTx.Error != nil {
		t.Fatal(lockTx.Error)
	}
	defer lockTx.Rollback()
	if err := lockTx.Exec(`SELECT id FROM open_api_keys WHERE id='key-lock' FOR UPDATE`).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.UpdateOpenAPIKey(context.Background(), req("key-lock"), owner) }()
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%open_api_keys%' AND pid<>pg_backend_pid()`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("write did not wait on actual row lock")
	}
	if err := lockTx.Exec(`UPDATE open_api_keys SET tenant_id='tenant-b' WHERE id='key-lock'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := lockTx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	assertCode(<-done, errcode.CodeNoPermission)
	var current int16
	if err := db.Raw(`SELECT status FROM open_api_keys WHERE id='key-lock'`).Scan(&current).Error; err != nil || current != 1 {
		t.Fatal("tenant change permitted stale write")
	}
	// A canceled waiter must return while the other transaction still owns the row lock.
	for _, operation := range []string{"update", "delete"} {
		t.Run("cancel_waiting_"+operation, func(t *testing.T) {
			id := "key-cancel-" + operation
			const secret = "fixture-sensitive-cancel-marker"
			if err := db.Exec(`INSERT INTO open_api_keys(id,tenant_id,api_key,status,name,created_id) VALUES (?, 'tenant-a', ?, 1, 'original', 'creator')`, id, secret+operation).Error; err != nil {
				t.Fatal("seed cancellation fixture")
			}
			holder := db.Begin()
			if holder.Error != nil {
				t.Fatal("begin holder")
			}
			defer holder.Rollback()
			if err := holder.Exec(`SELECT id FROM open_api_keys WHERE id=? FOR UPDATE`, id).Error; err != nil {
				t.Fatal("lock fixture")
			}
			var logs bytes.Buffer
			previousOutput := logrus.StandardLogger().Out
			logrus.SetOutput(&logs)
			defer logrus.SetOutput(previousOutput)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if operation == "update" {
					result <- svc.UpdateOpenAPIKey(ctx, req(id), owner)
				} else {
					result <- svc.DeleteOpenAPIKey(ctx, id, owner)
				}
			}()
			until := time.Now().Add(5 * time.Second)
			waiting := false
			for time.Now().Before(until) {
				var n int64
				if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%open_api_keys%' AND pid<>pg_backend_pid()`).Scan(&n).Error; err != nil {
					t.Fatal("inspect lock wait")
				}
				if n > 0 {
					waiting = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("request did not enter row lock wait")
			}
			cancel()
			select {
			case err := <-result:
				var failure *errcode.Error
				if !errors.As(err, &failure) || failure.Code != errcode.CodeDBError || failure.Data != nil || failure.CustomMsg != "api key change not executed" {
					t.Fatalf("unexpected cancellation result: %v", err)
				}
				if strings.Contains(err.Error(), secret) || strings.Contains(logs.String(), secret) {
					t.Fatal("cancellation leaked sensitive value")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled request did not return while lock was held")
			}
			var status int16
			if err := db.Raw(`SELECT status FROM open_api_keys WHERE id=?`, id).Row().Scan(&status); err != nil || status != 1 {
				t.Fatal("canceled write changed key")
			}
			if err := holder.Rollback().Error; err != nil {
				t.Fatal("release holder")
			}
			if err := db.Raw(`SELECT status FROM open_api_keys WHERE id=?`, id).Row().Scan(&status); err != nil || status != 1 {
				t.Fatal("key changed after release")
			}
		})
	}
	loss := &apiKeyReplyLossPool{DB: pool}
	wrapped, err := gorm.Open(postgres.New(postgres.Config{Conn: loss}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	global.DB = wrapped
	err = svc.UpdateOpenAPIKey(context.Background(), req("key-unknown"), owner)
	global.DB = db
	var failure *errcode.Error
	if !errors.As(err, &failure) || failure.Code != errcode.CodeDBError || failure.Data != nil || !strings.Contains(failure.CustomMsg, "unknown") || loss.commitCount.Load() != 1 {
		t.Fatalf("commit unknown error=%v", err)
	}
	if _, err := dal.LookupActiveOpenAPIKey(context.Background(), db, "fixture-secret-unknown"); err == nil {
		t.Fatal("real commit did not revoke")
	}
	global.DB = wrapped
	err = svc.DeleteOpenAPIKey(context.Background(), "key-delete-unknown", owner)
	global.DB = db
	if !errors.As(err, &failure) || failure.Code != errcode.CodeDBError || failure.Data != nil || !strings.Contains(failure.CustomMsg, "unknown") || loss.commitCount.Load() != 2 {
		t.Fatalf("delete commit unknown error=%v", err)
	}
	if _, err := dal.LookupActiveOpenAPIKey(context.Background(), db, "fixture-secret-delete-unknown"); err == nil {
		t.Fatal("real delete commit did not remove key")
	}
	loss.loseRollback = true
	var rollbackLogs bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&rollbackLogs)
	defer logrus.SetOutput(oldOutput)
	beforeExec := loss.execCount.Load()
	global.DB = wrapped
	err = svc.DeleteOpenAPIKey(context.Background(), "missing", owner)
	global.DB = db
	assertCode(err, errcode.CodeNoPermission)
	if loss.execCount.Load() != beforeExec || loss.commitCount.Load() != 2 || !strings.Contains(rollbackLogs.String(), "cleanup unconfirmed before write") || strings.Contains(rollbackLogs.String(), "synthetic sensitive") {
		t.Fatal("pre-write rollback failure classification or redaction incorrect")
	}
	global.DB = wrapped
	err = svc.UpdateOpenAPIKey(context.Background(), req("key-zero"), owner)
	global.DB = db
	if !errors.As(err, &failure) || failure.Code != errcode.CodeDBError || failure.Data != nil || !strings.Contains(failure.CustomMsg, "unknown") || loss.execCount.Load() != beforeExec+1 || loss.commitCount.Load() != 2 {
		t.Fatalf("post-write rollback must be unknown: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	global.DB = wrapped
	err = svc.DeleteOpenAPIKey(ctx, "key-zero", owner)
	global.DB = db
	if !errors.As(err, &failure) || failure.Code != errcode.CodeDBError || failure.CustomMsg != "api key change not executed" || loss.execCount.Load() != beforeExec+1 || loss.commitCount.Load() != 2 {
		t.Fatalf("canceled pre-write operation: %v", err)
	}
}

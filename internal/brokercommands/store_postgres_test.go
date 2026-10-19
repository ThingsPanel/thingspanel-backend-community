package brokercommands

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func postgresStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TP_BROKER_COMMANDS_TEST_DSN")
	if dsn == "" {
		data, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("PostgreSQL QA DSN unavailable")
		}
		dsn = strings.TrimSpace(string(data))
	}
	if dsn == "" {
		t.Skip("PostgreSQL QA DSN is empty")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open PostgreSQL QA database: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	schema := "brokercommands_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatalf("create private schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(commandDSNWithSchema(dsn, schema)), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open private schema: %v", err)
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbSQL.SetMaxOpenConns(12)
	t.Cleanup(func() {
		_ = dbSQL.Close()
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminSQL.Close()
	})
	if err := db.Exec(`CREATE TABLE devices (
		id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL,
		is_enabled varchar(24) NOT NULL, parent_id varchar(36)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE broker_transport_bindings (
		tenant_id varchar(36) NOT NULL, device_id varchar(36) NOT NULL,
		broker_cell varchar(128) NOT NULL, route_epoch bigint NOT NULL CHECK(route_epoch > 0),
		phase varchar(24) NOT NULL CHECK(phase IN ('active','connecting','draining')),
		updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,device_id)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE command_set_logs (id uuid PRIMARY KEY, device_id varchar(36) NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "006_commands.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(strings.ReplaceAll(string(migration), "public.", "")).Error; err != nil {
		t.Fatalf("apply private command migration: %v", err)
	}
	return New(db), db
}

func commandDSNWithSchema(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil {
			q := u.Query()
			q.Set("search_path", schema)
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	return dsn + " search_path=" + schema
}

func seedDevices(t *testing.T, db *gorm.DB, tenant, root, target string) {
	t.Helper()
	for _, id := range []string{root, target} {
		parent := any(nil)
		if id != root {
			parent = root
		}
		if err := db.Exec("INSERT INTO devices(id,tenant_id,is_enabled,parent_id) VALUES (?,?,?,?)", id, tenant, "enabled", parent).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES (?,?,?,?,?)", tenant, root, "cell-a", 7, "active").Error; err != nil {
		t.Fatal(err)
	}
}

func newRequest(tenant, caller, root, target, key string) Request {
	return Request{TenantID: tenant, CallerID: caller, Intent: "control", IdempotencyKey: key, DeviceID: target, TargetDeviceID: root,
		CanonicalRequest: []byte(`{"device_id":"` + target + `","identify":"switch","params":{"on":true}}`),
		Payload:          []byte(`{"message_id":"placeholder"}`), ExpiresAt: time.Now().Add(time.Hour)}
}

func persistLog(tx *gorm.DB, row Row) error {
	return tx.Exec("INSERT INTO command_set_logs(id,device_id) VALUES (?,?)", row.ID, row.TargetDeviceID).Error
}

func TestPostgresConcurrentSubmitIdempotencyAndConflict(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target, caller := uuid.NewString(), uuid.NewString(), uuid.NewString(), "user-a"
	seedDevices(t, db, tenant, root, target)
	req := newRequest(tenant, caller, root, target, "same-key")
	const n = 100
	rows := make([]Row, n)
	dups := make([]bool, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rows[i], dups[i], errs[i] = store.Submit(context.Background(), req, persistLog)
		}(i)
	}
	wg.Wait()
	var id string
	dupCount := 0
	for i := range rows {
		if errs[i] != nil {
			t.Fatalf("submit %d: %v", i, errs[i])
		}
		if id == "" {
			id = rows[i].ID
		}
		if rows[i].ID != id {
			t.Fatalf("got multiple IDs %q and %q", id, rows[i].ID)
		}
		if dups[i] {
			dupCount++
		}
	}
	if dupCount != n-1 {
		t.Fatalf("duplicate results=%d, want %d", dupCount, n-1)
	}
	var count int64
	db.Model(&Row{}).Count(&count)
	if count != 1 {
		t.Fatalf("stored rows=%d, want 1", count)
	}
	db.Table("command_set_logs").Count(&count)
	if count != 1 {
		t.Fatalf("persist callbacks=%d, want 1", count)
	}
	req.CanonicalRequest = []byte(`{"device_id":"` + target + `","identify":"switch","params":{"on":false}}`)
	if _, _, err := store.Submit(context.Background(), req, persistLog); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed canonical request error=%v", err)
	}
}

func TestPostgresSubmitPersistFailureRollsBack(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	_, _, err := store.Submit(context.Background(), newRequest(tenant, "user-a", root, target, "rollback"), func(tx *gorm.DB, row Row) error {
		if err := persistLog(tx, row); err != nil {
			return err
		}
		return errors.New("injected persistence failure")
	})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	var count int64
	db.Model(&Row{}).Count(&count)
	if count != 0 {
		t.Fatalf("command rows after rollback=%d", count)
	}
	db.Table("command_set_logs").Count(&count)
	if count != 0 {
		t.Fatalf("log rows after rollback=%d", count)
	}
}

func TestPostgresLogicalDeviceMustDescendFromTargetRoot(t *testing.T) {
	store, db := postgresStore(t)
	tenant, otherTenant, root, middle, leaf, otherRoot := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, middle)
	if err := db.Exec("INSERT INTO devices(id,tenant_id,is_enabled,parent_id) VALUES (?,?,?,?)", leaf, tenant, "enabled", middle).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO devices(id,tenant_id,is_enabled,parent_id) VALUES (?,?,?,NULL)", otherRoot, otherTenant, "enabled").Error; err != nil {
		t.Fatal(err)
	}
	req := newRequest(tenant, "user-a", root, leaf, "multilevel")
	req.CanonicalRequest = []byte(`{"device_id":"` + leaf + `","identify":"switch","params":{"on":true}}`)
	if _, _, err := store.Submit(context.Background(), req, persistLog); err != nil {
		t.Fatalf("valid multi-level logical-to-root route: %v", err)
	}

	reverse := newRequest(tenant, "user-a", leaf, root, "reverse")
	if _, _, err := store.Submit(context.Background(), reverse, persistLog); !errors.Is(err, ErrRejected) && !errors.Is(err, ErrFenced) {
		t.Fatalf("reverse root-to-child request=%v, want rejection", err)
	}

	crossTenant := newRequest(tenant, "user-a", otherRoot, leaf, "cross-tenant")
	if _, _, err := store.Submit(context.Background(), crossTenant, persistLog); !errors.Is(err, ErrRejected) && !errors.Is(err, ErrFenced) {
		t.Fatalf("cross-tenant root request=%v, want rejection", err)
	}
}

func TestPostgresClaimCompetesExpiryFenceAndEarlyAck(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	ctx := context.Background()
	row, _, err := store.Submit(ctx, newRequest(tenant, "user-a", root, target, "claim"), persistLog)
	if err != nil {
		t.Fatal(err)
	}
	var claimed atomicCount
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if got != nil {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("successful claims=%d, want 1", claimed.Load())
	}
	if err := store.Acknowledge(ctx, db, tenant, root, row.ID, true); err != nil {
		t.Fatalf("early ACK: %v", err)
	}
	if err := store.FinishPublish(ctx, row.ID, true, ""); !errors.Is(err, ErrState) {
		t.Fatalf("finish after early ACK=%v, want ErrState", err)
	}
	got, err := store.GetScoped(ctx, tenant, row.ID)
	if err != nil || got.State != StateDeviceReportedSuccess {
		t.Fatalf("early ACK terminal state=%q err=%v", got.State, err)
	}
	if _, err := store.GetScoped(ctx, uuid.NewString(), row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong tenant read=%v", err)
	}
	if err := store.Acknowledge(ctx, db, tenant, uuid.NewString(), row.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong device ACK=%v", err)
	}
}

func TestGetScopedLegacyNonUUIDFallsBackWithoutMaskingDatabaseErrors(t *testing.T) {
	store, db := postgresStore(t)
	ctx := context.Background()
	// Legacy message IDs are commonly short strings. A non-UUID must be a clean
	// not-found result so the caller can perform its separately scoped fallback.
	if _, err := store.GetScoped(ctx, "tenant-a", "legacy-command-17"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy non-UUID lookup = %v, want ErrNotFound", err)
	}

	// A UUID still executes the durable query. A real schema/query failure must
	// remain distinguishable from not-found and therefore fail closed.
	if err := db.Exec("DROP TABLE broker_command_requests").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScoped(ctx, "tenant-a", uuid.NewString()); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("UUID lookup against unavailable table = %v, want database error", err)
	}
}

func TestPostgresFindIdempotentScopesCallerAndIgnoresAcceptedExpiry(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	ctx := context.Background()
	req := newRequest(tenant, "caller-a", root, target, "scoped-retry")
	row, _, err := store.Submit(ctx, req, persistLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Row{}).Where("id = ?", row.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	got, found, err := store.FindIdempotent(ctx, tenant, "caller-a", req.Intent, req.IdempotencyKey, req.CanonicalRequest)
	if err != nil || !found || got.ID != row.ID {
		t.Fatalf("expired retry found=%v id=%q err=%v, want original id %q", found, got.ID, err, row.ID)
	}
	if _, found, err := store.FindIdempotent(ctx, tenant, "caller-b", req.Intent, req.IdempotencyKey, req.CanonicalRequest); err != nil || found {
		t.Fatalf("other caller lookup found=%v err=%v", found, err)
	}
	if _, found, err := store.FindIdempotent(ctx, uuid.NewString(), "caller-a", req.Intent, req.IdempotencyKey, req.CanonicalRequest); err != nil || found {
		t.Fatalf("other tenant lookup found=%v err=%v", found, err)
	}
	if _, _, err := store.FindIdempotent(ctx, tenant, "caller-a", req.Intent, req.IdempotencyKey, []byte(`{"changed":true}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed canonical request=%v, want ErrConflict", err)
	}
}

func TestPostgresExpiryFencingAndStaleDispatchNeverRequeues(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	ctx := context.Background()
	expiring := newRequest(tenant, "user-a", root, target, "expires")
	expiring.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	row, _, err := store.Submit(ctx, expiring, persistLog)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	retry, duplicate, err := store.Submit(ctx, expiring, persistLog)
	if err != nil || !duplicate || retry.ID != row.ID {
		t.Fatalf("expired idempotent retry row=%q duplicate=%v err=%v", retry.ID, duplicate, err)
	}
	got, err := store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
	if err != nil || got != nil {
		t.Fatalf("expired claim=%v err=%v", got, err)
	}
	stored, err := store.GetScoped(ctx, tenant, row.ID)
	if err != nil || stored.State != StateExpired {
		t.Fatalf("expiry state=%q err=%v", stored.State, err)
	}

	fencedReq := newRequest(tenant, "user-a", root, target, "fenced")
	fenced, _, err := store.Submit(ctx, fencedReq, persistLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch=8 WHERE tenant_id=? AND device_id=?", tenant, root).Error; err != nil {
		t.Fatal(err)
	}
	got, err = store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { t.Fatal("fenced command reached guard"); return nil })
	if err != nil || got != nil {
		t.Fatalf("fenced claim=%v err=%v", got, err)
	}
	stored, err = store.GetScoped(ctx, tenant, fenced.ID)
	if err != nil || stored.State != StateFenced {
		t.Fatalf("fenced state=%q err=%v", stored.State, err)
	}

	if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch=7 WHERE tenant_id=? AND device_id=?", tenant, root).Error; err != nil {
		t.Fatal(err)
	}
	crashed, _, err := store.Submit(ctx, newRequest(tenant, "user-a", root, target, "crashed"), persistLog)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
	if err != nil || claim == nil || claim.ID != crashed.ID {
		t.Fatalf("claim crashed row=%v err=%v", claim, err)
	}
	if err := db.Model(&Row{}).Where("id=?", crashed.ID).Update("updated_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := store.MarkStaleDispatches(ctx, 30*time.Second); err != nil || count != 1 {
		t.Fatalf("mark stale count=%d err=%v", count, err)
	}
	stored, err = store.GetScoped(ctx, tenant, crashed.ID)
	if err != nil || stored.State != StateUnknown {
		t.Fatalf("crashed state=%q err=%v", stored.State, err)
	}
	claim, err = store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
	if err != nil || claim != nil {
		t.Fatalf("unknown row reclaimed=%v err=%v", claim, err)
	}

	acknowledged, _, err := store.Submit(ctx, newRequest(tenant, "user-a", root, target, "broker-ack-no-device-report"), persistLog)
	if err != nil {
		t.Fatal(err)
	}
	claim, err = store.Claim(ctx, func(context.Context, *gorm.DB, Row) error { return nil })
	if err != nil || claim == nil || claim.ID != acknowledged.ID {
		t.Fatalf("claim broker-ack candidate=%v err=%v", claim, err)
	}
	if err := store.FinishPublish(ctx, acknowledged.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	stored, err = store.GetScoped(ctx, tenant, acknowledged.ID)
	if err != nil || stored.State != "broker_acknowledged" {
		t.Fatalf("successful MQTT publish state=%q err=%v", stored.State, err)
	}
	if err := db.Model(&Row{}).Where("id=?", acknowledged.ID).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := store.MarkStaleDispatches(ctx, 30*time.Second); err != nil || count != 1 {
		t.Fatalf("mark broker-ack stale count=%d err=%v", count, err)
	}
	stored, err = store.GetScoped(ctx, tenant, acknowledged.ID)
	if err != nil || stored.State != StateUnknown {
		t.Fatalf("broker-ack without device report state=%q err=%v", stored.State, err)
	}
}

type atomicCount struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCount) Add(v int) { c.mu.Lock(); c.n += v; c.mu.Unlock() }
func (c *atomicCount) Load() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

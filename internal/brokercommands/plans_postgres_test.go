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

func plansPostgresStore(t *testing.T) (*Store, *gorm.DB) {
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
	schema := "brokerplan_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatalf("create private schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(plansDSNWithSchema(dsn, schema)), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbSQL.SetMaxOpenConns(16)
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
	for _, name := range []string{"006_commands.sql", "013_command_plans.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(strings.ReplaceAll(string(migration), "public.", "")).Error; err != nil {
			t.Fatalf("apply private %s: %v", name, err)
		}
	}
	return New(db), db
}

func plansDSNWithSchema(dsn, schema string) string {
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

func planFixture(t *testing.T, db *gorm.DB) (tenant, caller, root, device string) {
	t.Helper()
	tenant, caller, root, device = uuid.NewString(), "user-"+uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := db.Exec("INSERT INTO devices(id,tenant_id,is_enabled,parent_id) VALUES (?,?,?,NULL),(?,?,?,?)", root, tenant, "enabled", device, tenant, "enabled", root).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES (?,?,?,?,?)", tenant, root, "cell-a", 7, "active").Error; err != nil {
		t.Fatal(err)
	}
	return
}

func planRequest(tenant, caller, root, device string, expires time.Time) Request {
	return Request{TenantID: tenant, CallerID: caller, Intent: "device.command", DeviceID: device, TargetDeviceID: root,
		CanonicalRequest: []byte(`{"device_id":"` + device + `","identify":"relay","params":{"on":true}}`),
		Payload:          []byte(`{"type":"command","data":{"on":true}}`), ExpiresAt: expires}
}

func createPlan(t *testing.T, s *Store, req Request, guard PlanGuard) Plan {
	t.Helper()
	p, err := s.CreatePlan(context.Background(), req, make([]byte, 32), guard)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return p
}

func planGuardOK(context.Context, *gorm.DB, Plan) error { return nil }

func TestPostgresPlanPreviewIsNotCommandAndAuthorizationIsExactAndIdempotent(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	p := createPlan(t, store, planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), planGuardOK)
	var commands, logs int64
	db.Model(&Row{}).Count(&commands)
	db.Table("command_set_logs").Count(&logs)
	if commands != 0 || logs != 0 {
		t.Fatalf("preview created side effects: commands=%d logs=%d", commands, logs)
	}
	if _, err := store.GetPlan(context.Background(), uuid.NewString(), caller, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant plan lookup=%v, want not found", err)
	}
	if _, err := store.GetPlan(context.Background(), tenant, "other-user", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-caller plan lookup=%v, want not found", err)
	}
	badHash := append([]byte(nil), p.PlanSHA256...)
	badHash[0] ^= 0xff
	if _, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, badHash, planGuardOK); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong hash authorization=%v, want conflict", err)
	}
	if _, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, func(context.Context, *gorm.DB, Plan) error { return ErrRejected }); !errors.Is(err, ErrRejected) {
		t.Fatalf("unauthorized guard result=%v, want rejected", err)
	}
	first, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, planGuardOK)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, planGuardOK)
	if err != nil || second != first {
		t.Fatalf("idempotent authorization=(%q,%v), first=%q", second, err, first)
	}
}

func TestPostgresPlanConcurrentSameKeyReturnsOneCommand(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	p := createPlan(t, store, planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), planGuardOK)
	auth, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, planGuardOK)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	type result struct {
		row Row
		dup bool
		err error
	}
	results := make([]result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i].row, results[i].dup, results[i].err = store.SubmitPlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, auth, "same-key", planGuardOK, persistLog)
		}(i)
	}
	close(start)
	wg.Wait()
	var id string
	duplicates := 0
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("same-key submit %d: %v", i, result.err)
		}
		if id == "" {
			id = result.row.ID
		}
		if result.row.ID != id {
			t.Fatalf("different command IDs %q and %q", id, result.row.ID)
		}
		if result.dup {
			duplicates++
		}
	}
	var commands, logs int64
	db.Model(&Row{}).Count(&commands)
	db.Table("command_set_logs").Count(&logs)
	if duplicates != n-1 || commands != 1 || logs != 1 {
		t.Fatalf("duplicates=%d commands=%d logs=%d, want %d/1/1", duplicates, commands, logs, n-1)
	}
}

func TestPostgresPlanConcurrentSubmitConsumesAtMostOnce(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	p := createPlan(t, store, planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), planGuardOK)
	auth, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, planGuardOK)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		row Row
		dup bool
		err error
	}
	results := make([]result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			key := "same-key"
			if i == 1 {
				key = "different-key"
			}
			results[i].row, results[i].dup, results[i].err = store.SubmitPlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, auth, key, planGuardOK, persistLog)
		}(i)
	}
	close(start)
	wg.Wait()
	var id string
	for _, result := range results {
		if result.err == nil {
			if id == "" {
				id = result.row.ID
			}
			if result.row.ID != id {
				t.Fatalf("multiple commands %s and %s", id, result.row.ID)
			}
		} else if !errors.Is(result.err, ErrRejected) {
			t.Fatalf("submit result error=%v, want success or rejected", result.err)
		}
	}
	if id == "" {
		t.Fatal("both submissions failed")
	}
	var commands, logs int64
	db.Model(&Row{}).Count(&commands)
	db.Table("command_set_logs").Count(&logs)
	var stored Plan
	db.Where("id=?", p.ID).Take(&stored)
	if commands != 1 || logs != 1 || stored.CommandID == nil || *stored.CommandID != id {
		t.Fatalf("command=%d log=%d plan.command_id=%v, want exactly one linked command", commands, logs, stored.CommandID)
	}
}

func TestPostgresPlanPersistFailureDoesNotConsumeAuthorization(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	p := createPlan(t, store, planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), planGuardOK)
	auth, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, planGuardOK)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SubmitPlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, auth, "guard-reject", func(context.Context, *gorm.DB, Plan) error { return ErrRejected }, persistLog); !errors.Is(err, ErrRejected) {
		t.Fatalf("submit guard rejection=%v, want rejected", err)
	}
	_, _, err = store.SubmitPlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, auth, "rollback", planGuardOK, func(tx *gorm.DB, row Row) error {
		if err := persistLog(tx, row); err != nil {
			return err
		}
		return errors.New("injected persistence error")
	})
	if err == nil {
		t.Fatal("expected persist failure")
	}
	var commands, logs int64
	db.Model(&Row{}).Count(&commands)
	db.Table("command_set_logs").Count(&logs)
	var after Plan
	db.Where("id=?", p.ID).Take(&after)
	if commands != 0 || logs != 0 || after.CommandID != nil || after.AuthorizationID == nil || *after.AuthorizationID != auth {
		t.Fatalf("rollback state commands=%d logs=%d command_id=%v authorization=%v", commands, logs, after.CommandID, after.AuthorizationID)
	}
	if _, _, err := store.SubmitPlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, auth, "retry", planGuardOK, persistLog); err != nil {
		t.Fatalf("retry after rolled back persistence: %v", err)
	}
}

func TestPostgresPlanGuardRouteFenceExpiryAndCancellation(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	if _, err := store.CreatePlan(context.Background(), planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), make([]byte, 32), func(context.Context, *gorm.DB, Plan) error { return ErrRejected }); !errors.Is(err, ErrRejected) {
		t.Fatalf("guard rejection=%v", err)
	}
	var count int64
	db.Model(&Plan{}).Count(&count)
	if count != 0 {
		t.Fatalf("guard rejection left %d plans", count)
	}
	if _, err := store.CreatePlan(context.Background(), planRequest(tenant, caller, root, device, time.Now().Add(-time.Second)), make([]byte, 32), planGuardOK); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired preview=%v, want invalid", err)
	}
	p := createPlan(t, store, planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), planGuardOK)
	if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch=8 WHERE tenant_id=? AND device_id=?", tenant, root).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthorizePlan(context.Background(), tenant, caller, p.ID, p.PlanSHA256, planGuardOK); !errors.Is(err, ErrFenced) {
		t.Fatalf("authorization after epoch change=%v, want fenced", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CreatePlan(ctx, planRequest(tenant, caller, root, device, time.Now().Add(time.Minute)), make([]byte, 32), planGuardOK); err == nil {
		t.Fatal("canceled context created a plan")
	}
	if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch=7 WHERE tenant_id=? AND device_id=?", tenant, root).Error; err != nil {
		t.Fatal(err)
	}
	var plan Plan
	db.Where("id=?", p.ID).Take(&plan)
	if plan.AuthorizationID != nil || plan.CommandID != nil {
		t.Fatalf("fenced plan mutated authorization=%v command=%v", plan.AuthorizationID, plan.CommandID)
	}
}

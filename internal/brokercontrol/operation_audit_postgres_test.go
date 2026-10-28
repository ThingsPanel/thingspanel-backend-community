package brokercontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokerledger"
)

func TestMigrationOperationAuditPostgresAtomicActions(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_BROKER_DATABASE_DSN"))
	if dsn == "" {
		if raw, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn"); err == nil {
			dsn = strings.TrimSpace(string(raw))
		}
	}
	if dsn == "" {
		t.Skip("isolated broker QA PostgreSQL DSN is unavailable")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated broker QA database")
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal("get QA connection pool")
	}
	adminSQL.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = adminSQL.Close() })
	var dbName string
	if err := admin.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil || dbName != "brokerqa" {
		t.Fatal("refusing operation audit test outside brokerqa")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("generate private schema name")
	}
	schema := "brokerqa_operation_audit_" + hex.EncodeToString(random[:])
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create private operation audit schema")
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error })
	testDSN, err := migrationTestSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("configure private operation audit search path")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open private operation audit schema")
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal("get private schema pool")
	}
	dbSQL.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = dbSQL.Close() })
	var currentDB, currentSchema string
	if err := db.Raw("SELECT current_database(), current_schema()").Row().Scan(&currentDB, &currentSchema); err != nil || currentDB != "brokerqa" || currentSchema != schema {
		t.Fatal("private operation audit schema isolation failed")
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal("list broker SQL migrations")
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("broker SQL migrations unavailable")
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("read broker SQL migration")
		}
		ddl := strings.ReplaceAll(string(body), "public.", `"`+schema+`".`)
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("apply private broker migration %s: %v", filepath.Base(path), err)
		}
	}

	ctx := context.Background()
	store := NewMigrationStore(db)
	const tenant, device, source, target = "audit-tenant", "audit-device", "audit-cell-a", "audit-cell-b"
	for _, cell := range []string{source, target} {
		for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
			if _, err := store.RegisterMember(ctx, cell, role, role+"-1", "boot-1"); err != nil {
				t.Fatalf("register private member %s/%s: %v", cell, role, err)
			}
		}
	}
	if err := db.Create(&TransportBinding{TenantID: tenant, DeviceID: device, BrokerCell: source, RouteEpoch: 1, Phase: "active", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal("seed private route binding")
	}
	_, sourceDigest, err := store.Members(ctx, source)
	if err != nil {
		t.Fatal("read private source roster")
	}
	changeID := uuid.NewString()
	freezePlan := strings.Repeat("a", 64)
	if _, err := store.Freeze(ctx, FreezeRouteRequest{ChangeID: changeID, TenantID: tenant, DeviceID: device, ExpectedCell: source, TargetCell: target, ExpectedEpoch: 1, Actor: "fixture-operator", MembersSHA256: sourceDigest, PlanSHA256: freezePlan}); err != nil {
		t.Fatal("freeze private source route")
	}
	if err := ackRoster(store, ctx, tenant, device, changeID, source, strings.Repeat("b", 64)); err != nil {
		t.Fatal("acknowledge private source fence")
	}

	planPrepare, planRebase, planActivate := strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)
	if _, err := store.WithOperationAudit("", planPrepare, "prepare"); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("empty operator actor must be rejected")
	}
	if _, err := store.WithOperationAudit("operator", "bad", "prepare"); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("malformed operator plan hash must be rejected")
	}
	if _, err := store.WithOperationAudit("operator", planPrepare, "freeze"); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("unsupported operator audit operation must be rejected")
	}
	var nilStore *MigrationStore
	if _, err := nilStore.WithOperationAudit("operator", planPrepare, "prepare"); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("nil migration store must be rejected")
	}
	prepareStore, err := store.WithOperationAudit("operator-qa", planPrepare, "prepare")
	if err != nil || prepareStore == store || store.audit != nil || prepareStore.audit == nil {
		t.Fatal("operation audit must return an isolated store copy")
	}
	activateStore, err := store.WithOperationAudit("operator-qa", planActivate, "activate")
	if err != nil {
		t.Fatal("create activate audit store")
	}
	rebaseStore, err := store.WithOperationAudit("operator-qa", planRebase, "rebase")
	if err != nil {
		t.Fatal("create rebase audit store")
	}
	if err := activateStore.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("4", 64)); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("operation-mismatched prepare must be rejected")
	}
	if err := rebaseStore.Activate(ctx, tenant, device, changeID); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("operation-mismatched activate must be rejected")
	}
	if err := rebaseStore.AcknowledgeFence(ctx, tenant, device, changeID, "broker", "broker-1", "boot-1", strings.Repeat("5", 64)); !errors.Is(err, ErrMigrationInvalid) {
		t.Fatal("operator audit context must not relabel a fence ACK")
	}
	var operatorEvents int64
	if err := db.Table("broker_route_change_events").Where("change_id=?::uuid AND stage='operator_apply'", changeID).Count(&operatorEvents).Error; err != nil || operatorEvents != 0 {
		t.Fatal("mismatched operations must not emit operator audit events")
	}

	if err := db.Exec(`CREATE FUNCTION test_fail_operator_apply() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.stage='operator_apply' THEN RAISE EXCEPTION 'injected operator audit failure'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal("create private operator audit failure trigger")
	}
	if err := db.Exec(`CREATE TRIGGER test_fail_operator_apply BEFORE INSERT ON broker_route_change_events FOR EACH ROW EXECUTE FUNCTION test_fail_operator_apply()`).Error; err != nil {
		t.Fatal("install private operator audit failure trigger")
	}
	if err := prepareStore.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("4", 64)); err == nil {
		t.Fatal("operator audit insertion failure must fail PrepareTarget")
	}
	if err := db.Exec("DROP TRIGGER test_fail_operator_apply ON broker_route_change_events").Error; err != nil {
		t.Fatal("remove private operator audit failure trigger")
	}
	if err := db.Exec("DROP FUNCTION test_fail_operator_apply()").Error; err != nil {
		t.Fatal("remove private operator audit failure function")
	}
	var route RouteChange
	var binding TransportBinding
	if err := db.Where("id=?::uuid", changeID).Take(&route).Error; err != nil {
		t.Fatal("read route after failed prepare")
	}
	if err := db.Where("tenant_id=? AND device_id=?", tenant, device).Take(&binding).Error; err != nil {
		t.Fatal("read binding after failed prepare")
	}
	var connectingEvents, failedAuditEvents int64
	if err := db.Table("broker_route_change_events").Where("change_id=?::uuid AND stage='connecting'", changeID).Count(&connectingEvents).Error; err != nil {
		t.Fatal("count rolled-back connecting events")
	}
	if err := db.Table("broker_route_change_events").Where("change_id=?::uuid AND stage='operator_apply'", changeID).Count(&failedAuditEvents).Error; err != nil {
		t.Fatal("count rolled-back operator audit events")
	}
	if route.Phase != "frozen" || binding.Phase != "draining" || connectingEvents != 0 || failedAuditEvents != 0 {
		t.Fatal("failed operator audit did not roll back route phase and earlier event")
	}
	if store.audit != nil {
		t.Fatal("original store was contaminated by the audited copy")
	}
	if err := prepareStore.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("4", 64)); err != nil {
		t.Fatal("audited target preparation failed after removing injected fault")
	}
	assertOperationAudit(t, db, changeID, 1, "prepare", "operator-qa", planPrepare)

	if _, err := store.RegisterMember(ctx, source, "adapter", "adapter-2", "boot-2"); err != nil {
		t.Fatal("register changed source generation for rebase")
	}
	_, rebasedDigest, err := store.Members(ctx, source)
	if err != nil {
		t.Fatal("read changed source roster")
	}
	if err := rebaseStore.RebaseBarrier(ctx, tenant, device, changeID, sourceDigest, rebasedDigest, strings.Repeat("6", 64)); err != nil {
		t.Fatal("audited barrier rebase failed")
	}
	assertOperationAudit(t, db, changeID, 2, "rebase", "operator-qa", planRebase)
	if err := ackRoster(store, ctx, tenant, device, changeID, source, strings.Repeat("7", 64)); err != nil {
		t.Fatal("acknowledge rebased source fence")
	}
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,?,?,?,2,1,true,now(),now())`, tenant, device, target, "broker-b", "boot-b", "connection-b").Error; err != nil {
		t.Fatal("seed connection observation for audit activate test")
	}
	_, duplicate, err := brokerledger.New(db).Accept(ctx, brokerledger.Message{
		TenantID: tenant, DeviceID: device, Stream: "event", SourceEventID: "operation-audit-pending",
		BrokerCell: target, RouteEpoch: 2, Payload: []byte(`{"value":1}`), Metadata: map[string]any{"source": "operation-audit-test"},
	})
	if err != nil || duplicate {
		t.Fatal("create pending new-epoch receipt through ledger store")
	}
	if err := activateStore.Activate(ctx, tenant, device, changeID); err != nil {
		t.Fatal("audited target activation failed")
	}
	assertOperationAudit(t, db, changeID, 3, "activate", "operator-qa", planActivate)
	if err := db.Where("tenant_id=? AND device_id=?", tenant, device).Take(&binding).Error; err != nil || binding.BrokerCell != target || binding.RouteEpoch != 2 || binding.Phase != "active" {
		t.Fatal("audited activation did not commit the expected route state")
	}
}

func assertOperationAudit(t *testing.T, db *gorm.DB, changeID string, sequence int, operation, actor, planSHA256 string) {
	t.Helper()
	var total int64
	if err := db.Table("broker_route_change_events").Where("change_id=?::uuid AND stage='operator_apply'", changeID).Count(&total).Error; err != nil || total != int64(sequence) {
		t.Fatalf("operator audit event count=%d, want %d", total, sequence)
	}
	var evidence string
	if err := db.Raw(`SELECT evidence::text FROM broker_route_change_events WHERE change_id=?::uuid AND stage='operator_apply' ORDER BY sequence DESC LIMIT 1`, changeID).Scan(&evidence).Error; err != nil {
		t.Fatal("read operator audit evidence")
	}
	var got struct {
		Operation  string `json:"operation"`
		Actor      string `json:"actor"`
		PlanSHA256 string `json:"plan_sha256"`
	}
	if err := json.Unmarshal([]byte(evidence), &got); err != nil || got.Operation != operation || got.Actor != actor || got.PlanSHA256 != planSHA256 {
		t.Fatal("operator audit evidence fields do not match approved operation")
	}
}

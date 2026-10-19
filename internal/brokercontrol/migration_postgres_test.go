package brokercontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This is a PostgreSQL state-machine test, not proof that runtime processes
// register or acknowledge the barrier. It runs all broker migrations in a
// private schema so concurrent QA suites cannot share state.
func TestMigrationPostgresStateMachineAndRollback(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		if raw, e := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn"); e == nil {
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
	if err := admin.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatal("inspect QA database")
	}
	if dbName != "brokerqa" {
		t.Fatal("refusing migration test outside brokerqa")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "brokerqa_route_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE SCHEMA \"" + schema + "\"").Error; err != nil {
		t.Fatal("create private test schema")
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA \"" + schema + "\" CASCADE").Error })
	testDSN, err := migrationTestSearchPath(dsn, schema)
	if err != nil {
		t.Fatal("configure private schema connection")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open private schema connection")
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal("get private schema pool")
	}
	dbSQL.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = dbSQL.Close() })
	paths, err := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal("list broker migrations")
	}
	sort.Strings(paths)
	if len(paths) < 12 || !strings.HasSuffix(paths[11], "012_route_changes.sql") {
		t.Fatalf("expected ordered migrations through 012_route_changes.sql, found %d", len(paths))
	}
	for _, path := range paths {
		body, e := os.ReadFile(path)
		if e != nil {
			t.Fatal("read broker migration")
		}
		ddl := strings.ReplaceAll(string(body), "public.", `"`+schema+`".`)
		if e := db.Exec(ddl).Error; e != nil {
			t.Fatalf("apply isolated migration %s: %v", filepath.Base(path), e)
		}
	}
	ctx := context.Background()
	store := NewMigrationStore(db)
	for _, cell := range []string{"cell-a", "cell-b"} {
		for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
			if _, err := store.RegisterMember(ctx, cell, role, role+"-1", "boot-1"); err != nil {
				t.Fatalf("register %s/%s: %v", cell, role, err)
			}
		}
	}
	if err := db.Create(&TransportBinding{TenantID: "tenant-rollback", DeviceID: "device-rollback", BrokerCell: "cell-a", RouteEpoch: 1, Phase: "active", UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("seed rollback binding")
	}
	rollbackReceipt := uuid.NewString()
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state) VALUES(?::uuid,'tenant-rollback','device-rollback','event','cell-a',1,decode('01','hex'),'{}',decode(repeat('aa',32),'hex'),'pending')`, rollbackReceipt).Error; err != nil {
		t.Fatal("seed rollback receipt")
	}
	_, rollbackDigest, err := store.Members(ctx, "cell-a")
	if err != nil {
		t.Fatal("read rollback source members")
	}
	if err := db.Exec(`CREATE FUNCTION test_fail_route_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected route event failure'; END $$;
		CREATE TRIGGER test_fail_route_event BEFORE INSERT ON broker_route_change_events FOR EACH ROW EXECUTE FUNCTION test_fail_route_event()`).Error; err != nil {
		t.Fatal("install transaction failure trigger")
	}
	_, injectedErr := NewMigrationStore(db).Freeze(ctx, FreezeRouteRequest{ChangeID: uuid.NewString(), TenantID: "tenant-rollback", DeviceID: "device-rollback", ExpectedCell: "cell-a", TargetCell: "cell-b", Actor: "operator", ExpectedEpoch: 1, MembersSHA256: rollbackDigest, PlanSHA256: strings.Repeat("a", 64)})
	if injectedErr == nil {
		t.Fatal("injected route event failure did not fail Freeze")
	}
	if err := db.Exec("DROP TRIGGER test_fail_route_event ON broker_route_change_events; DROP FUNCTION test_fail_route_event()").Error; err != nil {
		t.Fatal("remove transaction failure trigger")
	}
	var rollbackBinding TransportBinding
	if err := db.Where("tenant_id=? AND device_id=?", "tenant-rollback", "device-rollback").Take(&rollbackBinding).Error; err != nil {
		t.Fatal("read rolled-back binding")
	}
	var rollbackCount int64
	if err := db.Model(&RouteChange{}).Where("tenant_id=? AND device_id=?", "tenant-rollback", "device-rollback").Count(&rollbackCount).Error; err != nil {
		t.Fatal("read rolled-back change")
	}
	var receiptState string
	if err := db.Raw("SELECT state FROM broker_ingress_receipts WHERE id=?::uuid", rollbackReceipt).Scan(&receiptState).Error; err != nil {
		t.Fatal("read rolled-back receipt")
	}
	if rollbackBinding.Phase != "active" || rollbackCount != 0 || receiptState != "pending" {
		t.Fatalf("Freeze error was not atomic: binding=%+v changes=%d receipt=%q", rollbackBinding, rollbackCount, receiptState)
	}
	// Opposing A->B and B->A freezes on one tenant serialize on the tenant
	// admission advisory lock. The reversed cell directions must not deadlock.
	for _, pair := range []struct{ device, cell string }{{"ab-device", "cell-a"}, {"ba-device", "cell-b"}} {
		if err := db.Create(&TransportBinding{TenantID: "tenant-opposed", DeviceID: pair.device, BrokerCell: pair.cell, RouteEpoch: 1, Phase: "active", UpdatedAt: time.Now()}).Error; err != nil {
			t.Fatal("seed opposing route binding")
		}
	}
	_, digestA, err := store.Members(ctx, "cell-a")
	if err != nil {
		t.Fatal("read cell-a digest")
	}
	_, digestB, err := store.Members(ctx, "cell-b")
	if err != nil {
		t.Fatal("read cell-b digest")
	}
	opposeCtx, opposeCancel := context.WithTimeout(ctx, 5*time.Second)
	defer opposeCancel()
	opposeReqs := []FreezeRouteRequest{
		{ChangeID: uuid.NewString(), TenantID: "tenant-opposed", DeviceID: "ab-device", ExpectedCell: "cell-a", TargetCell: "cell-b", Actor: "operator", ExpectedEpoch: 1, MembersSHA256: digestA, PlanSHA256: strings.Repeat("b", 64)},
		{ChangeID: uuid.NewString(), TenantID: "tenant-opposed", DeviceID: "ba-device", ExpectedCell: "cell-b", TargetCell: "cell-a", Actor: "operator", ExpectedEpoch: 1, MembersSHA256: digestB, PlanSHA256: strings.Repeat("c", 64)},
	}
	var opposeWG sync.WaitGroup
	opposeResults := make(chan error, len(opposeReqs))
	for _, req := range opposeReqs {
		opposeWG.Add(1)
		go func(req FreezeRouteRequest) {
			defer opposeWG.Done()
			_, e := NewMigrationStore(db).Freeze(opposeCtx, req)
			opposeResults <- e
		}(req)
	}
	opposeWG.Wait()
	close(opposeResults)
	for e := range opposeResults {
		if e != nil {
			t.Fatalf("opposing Freeze deadlock/error: %v", e)
		}
	}
	var opposingAcks sync.WaitGroup
	for _, req := range opposeReqs {
		opposingAcks.Add(1)
		go func(req FreezeRouteRequest) {
			defer opposingAcks.Done()
			if e := ackRoster(NewMigrationStore(db), opposeCtx, req.TenantID, req.DeviceID, req.ChangeID, req.ExpectedCell, strings.Repeat("d", 64)); e != nil {
				t.Errorf("opposing freeze ACK failed: %v", e)
			}
		}(req)
	}
	opposingAcks.Wait()
	if err := db.Create(&TransportBinding{TenantID: "tenant-1", DeviceID: "device-1", BrokerCell: "cell-a", RouteEpoch: 4, Phase: "active", UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("seed active binding")
	}
	// Freeze must retire accepted work without deleting it or implying that an
	// uncertain publication was never sent.
	retiredReceipt := uuid.NewString()
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state)
		VALUES(?::uuid,'tenant-1','device-1','event','cell-a',4,decode('01','hex'),'{}',decode(repeat('aa',32),'hex'),'pending')`, retiredReceipt).Error; err != nil {
		t.Fatal("seed source receipt")
	}
	for i, state := range []string{"pending", "dispatching"} {
		if err := db.Exec(`INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload,state)
			VALUES(?::uuid,?::uuid,'tenant-1','device-1',?, '{}',?)`, uuid.NewString(), retiredReceipt, fmt.Sprintf("effect-%d", i), state).Error; err != nil {
			t.Fatal("seed source effects")
		}
	}
	for i, state := range []string{"accepted", "dispatching", "broker_acknowledged"} {
		if err := db.Exec(`INSERT INTO broker_command_requests(id,tenant_id,caller_id,intent,idempotency_key,device_id,target_device_id,route_cell,route_epoch,canonical_request,canonical_sha256,payload,expires_at,state)
			VALUES(?::uuid,'tenant-1','caller','intent',?, 'device-1','device-1','cell-a',4,'{}',decode(repeat('bb',32),'hex'),'{}',now()+interval '1 minute',?)`, uuid.NewString(), fmt.Sprintf("key-%d", i), state).Error; err != nil {
			t.Fatal("seed source command")
		}
	}

	_, sourceDigest, err := store.Members(ctx, "cell-a")
	if err != nil {
		t.Fatal("read source roster")
	}
	plan := strings.Repeat("1", 64)
	request := func(id string) FreezeRouteRequest {
		return FreezeRouteRequest{ChangeID: id, TenantID: "tenant-1", DeviceID: "device-1", ExpectedCell: "cell-a", TargetCell: "cell-b", Actor: "operator", ExpectedEpoch: 4, MembersSHA256: sourceDigest, PlanSHA256: plan}
	}
	ids := []string{uuid.NewString(), uuid.NewString()}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, e := store.Freeze(ctx, request(id)); results <- e }(id)
	}
	wg.Wait()
	close(results)
	twins := 0
	for e := range results {
		if e == nil {
			twins++
		} else if !errors.Is(e, ErrBindingConflict) {
			t.Fatalf("concurrent Freeze error: %v", e)
		}
	}
	if twins != 1 {
		t.Fatalf("concurrent Freeze winners=%d, want exactly one", twins)
	}
	var change RouteChange
	if err := db.Where("tenant_id=? AND device_id=?", "tenant-1", "device-1").Take(&change).Error; err != nil {
		t.Fatal("read winning route change")
	}
	// An identical request is idempotent; a reused ID with changed intent conflicts.
	if _, err := store.Freeze(ctx, request(change.ID)); err != nil {
		t.Fatalf("identical Freeze retry: %v", err)
	}
	conflict := request(change.ID)
	conflict.PlanSHA256 = strings.Repeat("2", 64)
	if _, err := store.Freeze(ctx, conflict); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("changed Freeze retry error=%v", err)
	}
	if err := store.AcknowledgeFence(ctx, "other-tenant", "device-1", change.ID, "broker", "broker-1", "boot-1", strings.Repeat("a", 64)); err == nil {
		t.Fatal("cross-tenant ACK unexpectedly succeeded")
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", change.ID, strings.Repeat("b", 64)); !errors.Is(err, ErrBarrierIncomplete) {
		t.Fatalf("Prepare without ACK error=%v", err)
	}
	var retiredState string
	if err := db.Raw("SELECT state FROM broker_ingress_receipts WHERE id=?::uuid", retiredReceipt).Scan(&retiredState).Error; err != nil || retiredState != "fenced" {
		t.Fatalf("pending receipt retirement state=%q err=%v", retiredState, err)
	}
	var effectStates []string
	if err := db.Raw("SELECT state FROM broker_ingress_effects WHERE receipt_id=?::uuid ORDER BY effect_kind", retiredReceipt).Scan(&effectStates).Error; err != nil || len(effectStates) != 2 || effectStates[0] != "fenced" || effectStates[1] != "unknown" {
		t.Fatalf("effect retirement states=%v err=%v", effectStates, err)
	}
	var commandStates []string
	if err := db.Raw("SELECT state FROM broker_command_requests WHERE target_device_id='device-1' ORDER BY idempotency_key").Scan(&commandStates).Error; err != nil || len(commandStates) != 3 {
		t.Fatalf("command retirement states=%v err=%v", commandStates, err)
	}
	stateByKey := map[string]string{}
	var retiredCommands []struct {
		IdempotencyKey string `gorm:"column:idempotency_key"`
		State          string `gorm:"column:state"`
	}
	if err := db.Raw("SELECT idempotency_key,state FROM broker_command_requests WHERE target_device_id='device-1'").Scan(&retiredCommands).Error; err != nil {
		t.Fatal("scan retired command states")
	}
	for _, row := range retiredCommands {
		stateByKey[row.IdempotencyKey] = row.State
	}
	if stateByKey["key-0"] != "fenced" || stateByKey["key-1"] != "unknown" || stateByKey["key-2"] != "unknown" {
		t.Fatalf("command states after freeze=%v", stateByKey)
	}
	// A restarted member has a new generation; the old ACK must not carry over.
	if err := store.AcknowledgeFence(ctx, "tenant-1", "device-1", change.ID, "broker", "broker-1", "boot-1", strings.Repeat("a", 64)); err != nil {
		t.Fatal("ACK current broker")
	}
	if _, err := store.RegisterMember(ctx, "cell-a", "adapter", "adapter-1", "boot-2"); err != nil {
		t.Fatal("register restarted adapter")
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", change.ID, strings.Repeat("b", 64)); !errors.Is(err, ErrBarrierIncomplete) {
		t.Fatalf("generation replacement inherited ACK: %v", err)
	}
	_, changedDigest, err := store.Members(ctx, "cell-a")
	if err != nil {
		t.Fatal("read changed roster")
	}
	if err := store.RebaseBarrier(ctx, "tenant-1", "device-1", change.ID, sourceDigest, changedDigest, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("rebase changed roster: %v", err)
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", change.ID, strings.Repeat("b", 64)); !errors.Is(err, ErrBarrierIncomplete) {
		t.Fatalf("rebase retained prior ACKs: %v", err)
	}
	if err := ackRoster(store, ctx, "tenant-1", "device-1", change.ID, "cell-a", strings.Repeat("d", 64)); err != nil {
		t.Fatal("ack rebased source roster")
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", change.ID, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("prepare target: %v", err)
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", change.ID, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("identical prepare retry: %v", err)
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", change.ID, strings.Repeat("e", 64)); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("changed prepare retry: %v", err)
	}
	if err := store.Activate(ctx, "tenant-1", "device-1", change.ID); !errors.Is(err, ErrTargetUnproven) {
		t.Fatalf("AUTH-only target activated: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,?,?,?,5,1,true,now(),now())`, "tenant-1", "device-1", "cell-b", "broker-1", "boot-1", "conn-1").Error; err != nil {
		t.Fatal("seed actual connection observation")
	}
	if err := store.Activate(ctx, "tenant-1", "device-1", change.ID); !errors.Is(err, ErrTargetUnproven) {
		t.Fatalf("connection without ingress activated: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state) VALUES(?::uuid,? ,?,'event','cell-b',5,decode('01','hex'),'{}',decode(repeat('ab',32),'hex'),'pending')`, uuid.NewString(), "tenant-1", "device-1").Error; err != nil {
		t.Fatal("seed trusted connecting-epoch ingress")
	}
	if err := store.Activate(ctx, "tenant-1", "device-1", change.ID); err != nil {
		t.Fatalf("activate with both proofs: %v", err)
	}
	var binding TransportBinding
	if err := db.Where("tenant_id=? AND device_id=?", "tenant-1", "device-1").Take(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.Phase != "active" || binding.BrokerCell != "cell-b" || binding.RouteEpoch != 5 {
		t.Fatalf("activated binding=%+v", binding)
	}
	if err := store.Activate(ctx, "tenant-1", "device-1", change.ID); err != nil {
		t.Fatalf("idempotent Activate: %v", err)
	}

	// Failed connecting rollback is a new migration from epoch 5, never a restore
	// of epoch 4. The prior connecting record is superseded, then the new target
	// must pass the same source fence and target proof requirements.
	_, targetDigest, err := store.Members(ctx, "cell-b")
	if err != nil {
		t.Fatal(err)
	}
	rollbackID := uuid.NewString()
	rollback := FreezeRouteRequest{ChangeID: rollbackID, TenantID: "tenant-1", DeviceID: "device-1", ExpectedCell: "cell-b", TargetCell: "cell-a", Actor: "operator", ExpectedEpoch: 5, MembersSHA256: targetDigest, PlanSHA256: strings.Repeat("f", 64)}
	if _, err := store.Freeze(ctx, rollback); err != nil {
		t.Fatalf("freeze rollback from active new epoch: %v", err)
	}
	if err := ackRoster(store, ctx, "tenant-1", "device-1", rollbackID, "cell-b", strings.Repeat("9", 64)); err != nil {
		t.Fatal("ack rollback source")
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", rollbackID, strings.Repeat("8", 64)); err != nil {
		t.Fatalf("prepare rollback target: %v", err)
	}
	var final TransportBinding
	if err := db.Where("tenant_id=? AND device_id=?", "tenant-1", "device-1").Take(&final).Error; err != nil {
		t.Fatal(err)
	}
	if final.BrokerCell != "cell-a" || final.RouteEpoch != 6 || final.Phase != "connecting" {
		t.Fatalf("rollback did not advance epoch: %+v", final)
	}
	// Model a target boot that never becomes ready. Recovery freezes that
	// connecting epoch, supersedes the old change, and advances again.
	_, digestA, err = store.Members(ctx, "cell-a")
	if err != nil {
		t.Fatal("read cell-a roster for failed target")
	}
	failedTargetID := uuid.NewString()
	failedTarget := FreezeRouteRequest{ChangeID: failedTargetID, TenantID: "tenant-1", DeviceID: "device-1", ExpectedCell: "cell-a", TargetCell: "cell-b", Actor: "operator", ExpectedEpoch: 6, MembersSHA256: digestA, PlanSHA256: strings.Repeat("3", 64)}
	if _, err := store.Freeze(ctx, failedTarget); err != nil {
		t.Fatalf("freeze for failed target: %v", err)
	}
	if err := ackRoster(store, ctx, "tenant-1", "device-1", failedTargetID, "cell-a", strings.Repeat("4", 64)); err != nil {
		t.Fatal("ack failed-target source")
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", failedTargetID, strings.Repeat("5", 64)); err != nil {
		t.Fatalf("prepare failed target: %v", err)
	}
	_, digestB, err = store.Members(ctx, "cell-b")
	if err != nil {
		t.Fatal("read cell-b roster for failed-target recovery")
	}
	recoveryID := uuid.NewString()
	recovery := FreezeRouteRequest{ChangeID: recoveryID, TenantID: "tenant-1", DeviceID: "device-1", ExpectedCell: "cell-b", TargetCell: "cell-a", Actor: "operator", ExpectedEpoch: 7, MembersSHA256: digestB, PlanSHA256: strings.Repeat("6", 64)}
	if _, err := store.Freeze(ctx, recovery); err != nil {
		t.Fatalf("freeze failed-connecting route: %v", err)
	}
	var superseded RouteChange
	if err := db.Where("id=?::uuid", failedTargetID).Take(&superseded).Error; err != nil || superseded.Phase != "superseded" {
		t.Fatalf("failed connecting phase=%q err=%v", superseded.Phase, err)
	}
	if err := ackRoster(store, ctx, "tenant-1", "device-1", recoveryID, "cell-b", strings.Repeat("7", 64)); err != nil {
		t.Fatal("ack recovery source")
	}
	if err := store.PrepareTarget(ctx, "tenant-1", "device-1", recoveryID, strings.Repeat("8", 64)); err != nil {
		t.Fatalf("prepare recovery target: %v", err)
	}
	if err := db.Where("tenant_id=? AND device_id=?", "tenant-1", "device-1").Take(&final).Error; err != nil {
		t.Fatal(err)
	}
	if final.BrokerCell != "cell-a" || final.RouteEpoch != 8 || final.Phase != "connecting" {
		t.Fatalf("failed-target recovery did not advance epoch: %+v", final)
	}
	var otherTenant int64
	if err := db.Model(&RouteChange{}).Where("tenant_id=?", "other-tenant").Count(&otherTenant).Error; err != nil || otherTenant != 0 {
		t.Fatalf("tenant scope leaked rows=%d err=%v", otherTenant, err)
	}
}

func ackRoster(store *MigrationStore, ctx context.Context, tenant, device, id, cell, evidence string) error {
	members, _, err := store.Members(ctx, cell)
	if err != nil {
		return err
	}
	for _, m := range members {
		if err := store.AcknowledgeFence(ctx, tenant, device, id, m.Role, m.InstanceID, m.BootID, evidence); err != nil {
			return fmt.Errorf("%s/%s: %w", m.Role, m.InstanceID, err)
		}
	}
	return nil
}

func migrationTestSearchPath(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			return "", e
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return fmt.Sprintf("%s search_path=%s", strings.TrimSpace(dsn), schema), nil
}

package brokerledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"project/internal/brokercontrol"
)

func retiredStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	store, db := postgresStore(t, Limits{})
	for _, name := range []string{"005_ingress_effects.sql", "006_commands.sql", "008_quarantine.sql", "012_route_changes.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := db.Exec(strings.ReplaceAll(string(body), "public.", "")).Error; err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return store, db
}

func TestPostgresAuditedRetirementBackpressureAndQuarantine(t *testing.T) {
	store, db := retiredStore(t)
	ctx := context.Background()
	// Unknown inactive routes preserve the original backpressure result.
	insertBinding(t, db, "tenant-unknown", "device-inactive", "cell-old", 1, "draining")
	unknown := testMessage("tenant-unknown", "device-inactive", "unknown-route")
	unknown.BrokerCell = "cell-old"
	if _, _, err := store.Accept(ctx, unknown); !errors.Is(err, ErrBindingInactive) {
		t.Fatalf("unknown inactive route err=%v", err)
	}
	// A higher current epoch alone is not evidence that this old cell was
	// retired by SQL012; unexplained fences remain fail-closed backpressure.
	insertBinding(t, db, "tenant-unknown", "device-fenced", "cell-current", 2, "active")
	fenced := testMessage("tenant-unknown", "device-fenced", "unproven-old-cell")
	fenced.BrokerCell = "cell-old"
	fenced.RouteEpoch = 1
	if _, _, err := store.Accept(ctx, fenced); !errors.Is(err, ErrFenced) {
		t.Fatalf("unproven old cell err=%v", err)
	}

	const tenant, device, source, target = "tenant-retired", "device-retired", "cell-source", "cell-target"
	insertBinding(t, db, tenant, device, source, 10, "active")
	routes := brokercontrol.NewMigrationStore(db)
	for _, cell := range []string{source, target} {
		for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
			if _, err := routes.RegisterMember(ctx, cell, role, role+"-1", "boot-1"); err != nil {
				t.Fatalf("register %s/%s: %v", cell, role, err)
			}
		}
	}
	_, digest, err := routes.Members(ctx, source)
	if err != nil {
		t.Fatal("read source roster")
	}
	changeID := uuid.NewString()
	if _, err := routes.Freeze(ctx, brokercontrol.FreezeRouteRequest{ChangeID: changeID, TenantID: tenant, DeviceID: device, ExpectedCell: source, TargetCell: target, Actor: "qa", ExpectedEpoch: 10, MembersSHA256: digest, PlanSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatalf("freeze source route: %v", err)
	}
	old := testMessage(tenant, device, "retained-old-route")
	old.BrokerCell = source
	old.RouteEpoch = 10
	if _, _, err := store.Accept(ctx, old); !errors.Is(err, ErrRouteRetired) {
		t.Fatalf("audited retired source err=%v", err)
	}
	rejection := Rejection{TenantID: tenant, DeviceID: device, BrokerCell: source, RouteEpoch: 10, Topic: "devices/event/retained", Body: []byte("opaque retained body"), Reason: "retired_transport_route"}
	qid, duplicate, err := store.RecordRejection(ctx, rejection)
	if err != nil || duplicate || qid == "" {
		t.Fatalf("persist retired rejection id=%q duplicate=%v err=%v", qid, duplicate, err)
	}
	qid2, duplicate, err := store.RecordRejection(ctx, rejection)
	if err != nil || !duplicate || qid2 != qid {
		t.Fatalf("stable retired rejection id=%q want=%q duplicate=%v err=%v", qid2, qid, duplicate, err)
	}
	var occurrences int64
	if err := db.Raw("SELECT occurrences FROM broker_ingress_quarantine WHERE id=?", qid).Scan(&occurrences).Error; err != nil || occurrences != 2 {
		t.Fatalf("deduplicated quarantine occurrences=%d err=%v", occurrences, err)
	}
	// Missing source ACK still blocks route preparation; retirement does not
	// synthesize business receipts or ACK proofs.
	if err := routes.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("b", 64)); !errors.Is(err, brokercontrol.ErrBarrierIncomplete) {
		t.Fatalf("PrepareTarget without source ACK=%v", err)
	}
	for _, m := range mustMembers(t, routes, ctx, source) {
		if err := routes.AcknowledgeFence(ctx, tenant, device, changeID, m.Role, m.InstanceID, m.BootID, strings.Repeat("c", 64)); err != nil {
			t.Fatalf("source ACK: %v", err)
		}
	}
	if err := routes.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("prepare target: %v", err)
	}
	if id, dup, err := store.RecordRejection(ctx, rejection); err != nil || !dup || id != qid {
		t.Fatalf("higher-epoch retired duplicate id=%q want=%q duplicate=%v err=%v", id, qid, dup, err)
	}
	if err := db.Raw("SELECT occurrences FROM broker_ingress_quarantine WHERE id=?", qid).Scan(&occurrences).Error; err != nil || occurrences != 3 {
		t.Fatalf("higher-epoch duplicate occurrences=%d err=%v", occurrences, err)
	}
	// Once binding advances, only the exact recorded source cell+epoch is a
	// terminal retired route. Wrong cell and future epoch remain fenced.
	if _, _, err := store.Accept(ctx, old); !errors.Is(err, ErrRouteRetired) {
		t.Fatalf("advanced binding old source err=%v", err)
	}
	future := testMessage(tenant, device, "future-epoch")
	future.BrokerCell = target
	future.RouteEpoch = 12
	if _, _, err := store.Accept(ctx, future); !errors.Is(err, ErrFenced) {
		t.Fatalf("future epoch err=%v", err)
	}
	wrongCell := old
	wrongCell.BrokerCell = "cell-forged"
	if _, _, err := store.Accept(ctx, wrongCell); !errors.Is(err, ErrFenced) {
		t.Fatalf("wrong cell err=%v", err)
	}
	// A caller cannot turn a current active frame into retirement merely by
	// selecting the reserved reason string.
	insertBinding(t, db, "tenant-forged", "device-forged", "cell-live", 1, "active")
	forged := Rejection{TenantID: "tenant-forged", DeviceID: "device-forged", BrokerCell: "cell-live", RouteEpoch: 1, Topic: "devices/event/x", Body: []byte("bad"), Reason: "retired_transport_route"}
	if _, _, err := store.RecordRejection(ctx, forged); !errors.Is(err, ErrQuarantineFenced) {
		t.Fatalf("forged retirement reason err=%v", err)
	}
	if count, _ := quarantineUsage(t, db, "tenant-forged"); count != 0 {
		t.Fatalf("forged reason wrote %d quarantine records", count)
	}
	// A storage failure remains an error; adapter callers must not ACK it.
	if err := db.Exec(`CREATE FUNCTION fail_retired_quarantine() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected quarantine failure'; END $$;
		CREATE TRIGGER fail_retired_quarantine BEFORE INSERT ON broker_ingress_quarantine FOR EACH ROW EXECUTE FUNCTION fail_retired_quarantine()`).Error; err != nil {
		t.Fatal("install quarantine failure trigger")
	}
	failureCase := rejection
	failureCase.Topic = "devices/event/retained-failure"
	if _, _, err := store.RecordRejection(ctx, failureCase); err == nil {
		t.Fatal("quarantine database failure returned success (would permit ACK)")
	}
	if err := db.Exec("DROP TRIGGER fail_retired_quarantine ON broker_ingress_quarantine; DROP FUNCTION fail_retired_quarantine()").Error; err != nil {
		t.Fatal("remove quarantine failure trigger")
	}
	if count, _ := quarantineUsage(t, db, tenant); count != 1 {
		t.Fatalf("failed quarantine write changed record count to %d", count)
	}
}

func mustMembers(t *testing.T, s *brokercontrol.MigrationStore, ctx context.Context, cell string) []brokercontrol.RouteMember {
	t.Helper()
	members, _, err := s.Members(ctx, cell)
	if err != nil {
		t.Fatalf("read %s members: %v", cell, err)
	}
	return members
}

func TestPostgresRetiredQuarantineRequiresRouteHistoryAndExactSourceScope(t *testing.T) {
	store, db := retiredStore(t)
	ctx := context.Background()
	insertBinding(t, db, "tenant-draining", "device-draining", "cell-a", 4, "draining")
	input := Rejection{TenantID: "tenant-draining", DeviceID: "device-draining", BrokerCell: "cell-a", RouteEpoch: 4, Topic: "devices/event/x", Body: []byte("bad"), Reason: "retired_transport_route"}
	if _, _, err := store.RecordRejection(ctx, input); !errors.Is(err, ErrQuarantineFenced) {
		t.Fatalf("draining without migration history err=%v", err)
	}
	if count, _ := quarantineUsage(t, db, "tenant-draining"); count != 0 {
		t.Fatalf("no-history retired row count=%d", count)
	}
	// Tenant/device/cell/epoch mismatches never inherit another route's proof.
	insertBinding(t, db, "tenant-mismatch", "device-mismatch", "cell-a", 5, "active")
	input.TenantID = "tenant-mismatch"
	input.DeviceID = "device-mismatch"
	input.RouteEpoch = 4
	if _, _, err := store.RecordRejection(ctx, input); !errors.Is(err, ErrQuarantineFenced) {
		t.Fatalf("wrong source epoch err=%v", err)
	}
	input.RouteEpoch = 5
	input.BrokerCell = "cell-other"
	if _, _, err := store.RecordRejection(ctx, input); !errors.Is(err, ErrQuarantineFenced) {
		t.Fatalf("wrong source cell err=%v", err)
	}
	if count, _ := quarantineUsage(t, db, "tenant-mismatch"); count != 0 {
		t.Fatalf("mismatched route wrote %d quarantine records", count)
	}
}

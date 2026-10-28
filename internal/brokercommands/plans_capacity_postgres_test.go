package brokercommands

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func seedPlanRows(t *testing.T, db *gorm.DB, tenant, root string, count int, canonicalBytes int) {
	t.Helper()
	if err := db.Exec(`INSERT INTO broker_command_plans
		(id,tenant_id,caller_id,intent,device_id,target_device_id,route_cell,route_epoch,canonical_request,payload,context_sha256,plan_sha256,expires_at)
		SELECT gen_random_uuid(), ?, 'seed-caller', 'seed', ?, ?, 'cell-a', 7,
		       convert_to(repeat('x', ?), 'UTF8'), decode('', 'hex'), decode(repeat('00',32),'hex'), decode(repeat('11',32),'hex'), now()+interval '1 minute'
		FROM generate_series(1,?)`, tenant, root, root, canonicalBytes, count).Error; err != nil {
		t.Fatalf("seed plan capacity rows: %v", err)
	}
}

func TestPostgresPlanCapacityCountAndTenantIsolation(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	seedPlanRows(t, db, tenant, root, maxPendingPlans, 1)
	req := planRequest(tenant, caller, root, device, time.Now().Add(time.Minute))
	if _, err := store.CreatePlan(context.Background(), req, make([]byte, sha256.Size), planGuardOK); !errors.Is(err, ErrCapacity) {
		t.Fatalf("create at full pending-plan count=%v, want ErrCapacity", err)
	}
	otherTenant, otherCaller, otherRoot, otherDevice := planFixture(t, db)
	other := planRequest(otherTenant, otherCaller, otherRoot, otherDevice, time.Now().Add(time.Minute))
	if _, err := store.CreatePlan(context.Background(), other, make([]byte, sha256.Size), planGuardOK); err != nil {
		t.Fatalf("another tenant's empty budget was affected: %v", err)
	}
}

func TestPostgresPlanCapacityBytesExactBoundary(t *testing.T) {
	store, db := plansPostgresStore(t)
	tenant, caller, root, device := planFixture(t, db)
	req := planRequest(tenant, caller, root, device, time.Now().Add(time.Minute))
	remaining := maxPendingPlanBytes - len(req.CanonicalRequest) - len(req.Payload)
	if remaining <= 0 {
		t.Fatal("request unexpectedly exceeds plan byte budget")
	}
	seedPlanRows(t, db, tenant, root, 1, remaining)
	if _, err := store.CreatePlan(context.Background(), req, make([]byte, sha256.Size), planGuardOK); err != nil {
		t.Fatalf("exact plan-byte budget should be accepted: %v", err)
	}
	req2 := planRequest(tenant, caller, root, device, time.Now().Add(time.Minute))
	if _, err := store.CreatePlan(context.Background(), req2, make([]byte, sha256.Size), planGuardOK); !errors.Is(err, ErrCapacity) {
		t.Fatalf("plan above byte budget=%v, want ErrCapacity", err)
	}
}

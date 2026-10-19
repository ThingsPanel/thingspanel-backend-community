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

// Cross-store proof: a connecting-route receipt is only staged. After the
// migration state machine activates the new epoch, the same receipt can be
// processed atomically by the ledger worker.
func TestPostgresMigrationActivationGatesLedgerProcessing(t *testing.T) {
	ledger, db := postgresStore(t, Limits{})
	for _, name := range []string{"003_state.sql", "005_ingress_effects.sql", "006_commands.sql", "012_route_changes.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := db.Exec(strings.ReplaceAll(string(body), "public.", "")).Error; err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	ctx := context.Background()
	const tenant, device, source, target = "tenant-activate", "device-activate", "cell-a", "cell-b"
	insertBinding(t, db, tenant, device, source, 4, "active")
	routes := brokercontrol.NewMigrationStore(db)
	for _, cell := range []string{source, target} {
		for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
			if _, err := routes.RegisterMember(ctx, cell, role, role+"-1", "boot-1"); err != nil {
				t.Fatalf("register %s/%s: %v", cell, role, err)
			}
		}
	}
	_, membersSHA, err := routes.Members(ctx, source)
	if err != nil {
		t.Fatal("read source member digest")
	}
	changeID := uuid.NewString()
	_, err = routes.Freeze(ctx, brokercontrol.FreezeRouteRequest{ChangeID: changeID, TenantID: tenant, DeviceID: device, ExpectedCell: source, TargetCell: target, Actor: "qa", ExpectedEpoch: 4, MembersSHA256: membersSHA, PlanSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatalf("freeze route: %v", err)
	}
	sourceMembers, _, err := routes.Members(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range sourceMembers {
		if err := routes.AcknowledgeFence(ctx, tenant, device, changeID, member.Role, member.InstanceID, member.BootID, strings.Repeat("b", 64)); err != nil {
			t.Fatalf("ACK %s: %v", member.Role, err)
		}
	}
	if err := routes.PrepareTarget(ctx, tenant, device, changeID, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("prepare target: %v", err)
	}
	message := testMessage(tenant, device, "activation-proof")
	message.BrokerCell = target
	message.RouteEpoch = 5
	id, duplicate, err := ledger.Accept(ctx, message)
	if err != nil || duplicate || id == "" {
		t.Fatalf("accept connecting receipt id=%q duplicate=%v err=%v", id, duplicate, err)
	}
	called := false
	processed, err := ledger.ProcessOne(ctx, func(context.Context, *gorm.DB, Message) error { called = true; return nil })
	if err != nil || processed || called {
		t.Fatalf("connecting receipt ran processor: processed=%v called=%v err=%v", processed, called, err)
	}
	var pending int64
	if err := db.Table("broker_ingress_receipts").Where("id=? AND state='pending'", id).Count(&pending).Error; err != nil || pending != 1 {
		t.Fatalf("connecting receipt pending=%d err=%v", pending, err)
	}
	// The exact new epoch was admitted under the binding lock after Prepare.
	// Receipt timestamps may come from another host clock; they cannot establish
	// or disprove that transaction order. Simulate skew deterministically.
	if err := db.Exec("UPDATE broker_ingress_receipts SET created_at=(SELECT prepared_at-interval '2 minutes' FROM broker_route_changes WHERE id=?) WHERE id=?", changeID, id).Error; err != nil {
		t.Fatal("simulate independent receipt clock")
	}
	if err := routes.Activate(ctx, tenant, device, changeID); !errors.Is(err, brokercontrol.ErrTargetUnproven) {
		t.Fatalf("new-epoch receipt without real connection activated: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,?,?,?,5,1,true,now(),now())`, tenant, device, target, "broker-1", "target-boot", "conn-1").Error; err != nil {
		t.Fatal("seed actual target connection observation")
	}
	if err := routes.Activate(ctx, tenant, device, changeID); err != nil {
		t.Fatalf("activate route with connection+ingress: %v", err)
	}
	if err := db.Exec("CREATE TABLE activation_side_effects(receipt_id uuid PRIMARY KEY)").Error; err != nil {
		t.Fatal("create side-effect fixture")
	}
	var sideEffects int64
	processed, err = ledger.ProcessOne(ctx, func(_ context.Context, tx *gorm.DB, got Message) error {
		called = true
		if got.ID != id || got.RouteEpoch != 5 || got.BrokerCell != target {
			return errors.New("unexpected processed receipt scope")
		}
		return tx.Exec("INSERT INTO activation_side_effects(receipt_id) VALUES(?)", got.ID).Error
	})
	if err != nil || !processed || !called {
		t.Fatalf("activated receipt processing: processed=%v called=%v err=%v", processed, called, err)
	}
	var state string
	if err := db.Raw("SELECT state FROM broker_ingress_receipts WHERE id=?", id).Scan(&state).Error; err != nil || state != stateProcessed {
		t.Fatalf("activated receipt state=%q err=%v", state, err)
	}
	if err := db.Table("activation_side_effects").Where("receipt_id=?", id).Count(&sideEffects).Error; err != nil || sideEffects != 1 {
		t.Fatalf("business side effect count=%d err=%v", sideEffects, err)
	}
}

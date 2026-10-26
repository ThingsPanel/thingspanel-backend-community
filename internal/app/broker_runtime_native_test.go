package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"project/internal/brokercontrol"
)

// This assertion uses the REAL native plugin and running production Adapter,
// effects and dispatcher workers from the E2E fixture. No member/ACK is seeded.
// It proves local source fencing; it is not the full multi-cell gray migration.
func assertNativeRuntimeFence(t *testing.T, ctx context.Context, db *gorm.DB, device mqtt.Client, brokerLog string) {
	t.Helper()
	store := brokercontrol.NewMigrationStore(db)
	members, digest, e := store.Members(ctx, nativeE2ECell)
	if e != nil {
		t.Fatalf("read real runtime members: %v", e)
	}
	if len(members) != 4 {
		t.Fatalf("real runtime members=%d, want broker/adapter/dispatcher/effects", len(members))
	}
	var binding brokercontrol.TransportBinding
	if e := db.Where("tenant_id=? AND device_id=?", nativeE2ETenant, nativeE2EDevice).Take(&binding).Error; e != nil {
		t.Fatal(e)
	}
	changeID := uuid.NewString()
	_, e = store.Freeze(ctx, brokercontrol.FreezeRouteRequest{ChangeID: changeID, TenantID: nativeE2ETenant, DeviceID: nativeE2EDevice, ExpectedCell: nativeE2ECell, ExpectedEpoch: int64(binding.RouteEpoch), TargetCell: "isolated-target-not-prepared", Actor: "native-e2e-operator", MembersSHA256: digest, PlanSHA256: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatalf("freeze real runtime: %v", e)
	}
	waitNativeE2E(t, 12*time.Second, "all four actual runtime fence acknowledgements", func() bool {
		var acknowledged int64
		return db.Table("broker_route_barrier_members").Where("change_id=?::uuid AND acknowledged_at IS NOT NULL", changeID).Count(&acknowledged).Error == nil && acknowledged == 4
	})
	before, _ := os.ReadFile(brokerLog)
	deniedBefore := strings.Count(string(before), "Denied PUBLISH")
	publishNativeE2E(t, ctx, device, "devices/event/native-after-freeze", []byte(`{"source_event_id":"native-after-freeze","method":"blocked","params":{"value":1}}`))
	waitNativeE2E(t, 3*time.Second, "actual native PUBLISH denial after its fence ACK", func() bool {
		body, _ := os.ReadFile(brokerLog)
		return strings.Count(string(body), "Denied PUBLISH") > deniedBefore
	})
	if n := nativeE2EReceiptCount(t, db, nativeE2ETenant, nativeE2EDevice, "native-after-freeze"); n != 0 {
		t.Fatalf("frozen source admitted %d business receipts", n)
	}
}

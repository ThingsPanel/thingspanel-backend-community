package brokerstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"project/internal/model"
)

// activityStore adds the production migrations needed by ProjectCurrent to
// testStore's per-test PostgreSQL schema. The shared QA database is never used
// outside that isolated search_path.
func activityStore(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	for _, name := range []string{"002_ledger.sql", "005_ingress_effects.sql", "006_commands.sql", "009_automation_actions.sql", "010_telemetry_activity.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.ReplaceAll(string(contents), "public.", "")
		if err := s.db.Exec(sql).Error; err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if err := s.db.AutoMigrate(&model.DeviceConfig{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func insertActivityReceipt(t *testing.T, s *Store, tenant, device, cell string, epoch uint64, payload []byte) string {
	t.Helper()
	id := uuid.NewString()
	digest := sha256.Sum256(payload)
	if err := s.db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,processed_at) VALUES(?,?,?,'telemetry_sample',?,?,?,'{}',?,'processed',now())`, id, tenant, device, cell, epoch, payload, digest[:]).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func setActivity(t *testing.T, s *Store, tenant, device, transport, cell string, epoch uint64, observed time.Time) {
	t.Helper()
	r := insertActivityReceipt(t, s, tenant, device, cell, epoch, []byte("telemetry"))
	if err := s.db.Exec(`INSERT INTO broker_telemetry_activity(tenant_id,device_id,transport_device_id,broker_cell,route_epoch,receipt_id,observed_at,next_check_at) VALUES(?,?,?,?,?,?,?,now()) ON CONFLICT(tenant_id,device_id) DO UPDATE SET transport_device_id=excluded.transport_device_id,broker_cell=excluded.broker_cell,route_epoch=excluded.route_epoch,receipt_id=excluded.receipt_id,observed_at=excluded.observed_at,next_check_at=now()`, tenant, device, transport, cell, epoch, r, observed.UTC()).Error; err != nil {
		t.Fatal(err)
	}
}

func configureActivity(t *testing.T, s *Store, id, tenant string, heartbeat, timeout int) {
	t.Helper()
	cfgID := "cfg-" + id
	other, _ := json.Marshal(map[string]int{"heartbeat": heartbeat, "online_timeout": timeout})
	otherText := string(other)
	cfg := model.DeviceConfig{ID: cfgID, Name: "activity test", DeviceType: "1", TenantID: tenant, OtherConfig: &otherText, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&model.Device{}).Where("tenant_id=? AND id=?", tenant, id).Update("device_config_id", cfgID).Error; err != nil {
		t.Fatal(err)
	}
}

func countActivityRows(t *testing.T, s *Store, table, where string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := s.db.Table(table).Where(where, args...).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestProjectCurrentHeartbeatDeadlineAndAtomicEffects(t *testing.T) {
	s := activityStore(t)
	configureActivity(t, s, "da", "ta", 2, 0)
	setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())

	first, err := s.ProjectCurrent(context.Background(), "ta", "da")
	if err != nil || first.Status != "online" || !first.Changed || first.Device.IsOnline != 1 {
		t.Fatalf("fresh activity should project online: %+v err=%v", first, err)
	}
	if got := countActivityRows(t, s, "device_status_history", "tenant_id=? AND device_id=?", "ta", "da"); got != 1 {
		t.Fatalf("history rows=%d want 1", got)
	}
	if got := countActivityRows(t, s, "broker_ingress_receipts", "tenant_id=? AND device_id=? AND stream='state_transition'", "ta", "da"); got != 1 {
		t.Fatalf("transition receipts=%d want 1", got)
	}
	if got := countActivityRows(t, s, "broker_ingress_effects", "tenant_id=? AND device_id=? AND effect_kind='automation'", "ta", "da"); got != 1 {
		t.Fatalf("automation effects=%d want 1", got)
	}

	duplicate, err := s.ProjectCurrent(context.Background(), "ta", "da")
	if err != nil || duplicate.Status != "online" || duplicate.Changed {
		t.Fatalf("duplicate projection: %+v err=%v", duplicate, err)
	}
	if got := countActivityRows(t, s, "device_status_history", "tenant_id=? AND device_id=?", "ta", "da"); got != 1 {
		t.Fatalf("duplicate added status history: %d", got)
	}

	if err := s.db.Exec(`UPDATE broker_telemetry_activity SET observed_at=now()-interval '4 seconds',next_check_at=now() WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
		t.Fatal(err)
	}
	offline, err := s.ProjectCurrent(context.Background(), "ta", "da")
	if err != nil || offline.Status != "offline" || !offline.Changed || offline.Device.IsOnline != 0 {
		t.Fatalf("expired heartbeat should project offline: %+v err=%v", offline, err)
	}
	if got := countActivityRows(t, s, "device_status_history", "tenant_id=? AND device_id=?", "ta", "da"); got != 2 {
		t.Fatalf("online/offline history rows=%d want 2", got)
	}
	if got := countActivityRows(t, s, "broker_ingress_receipts", "tenant_id=? AND device_id=? AND stream='state_transition'", "ta", "da"); got != 2 {
		t.Fatalf("transition receipts=%d want 2", got)
	}
	if got := countActivityRows(t, s, "broker_ingress_effects", "tenant_id=? AND device_id=? AND effect_kind='automation'", "ta", "da"); got != 2 {
		t.Fatalf("automation effects=%d want 2", got)
	}
}

func TestProjectCurrentTimeoutModeAndUnknownActivity(t *testing.T) {
	t.Run("timeout-mode-idle-offline", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 0, 2)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())
		first, err := s.ProjectCurrent(context.Background(), "ta", "da")
		if err != nil || first.Status != "online" || !first.Changed {
			t.Fatalf("fresh timeout-mode activity should project online: %+v err=%v", first, err)
		}
		if err := s.db.Exec(`UPDATE broker_telemetry_activity SET observed_at=now()-interval '4 seconds',next_check_at=now() WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
			t.Fatal(err)
		}
		got, err := s.ProjectCurrent(context.Background(), "ta", "da")
		if err != nil || got.Status != "offline" || !got.Changed || got.Device.IsOnline != 0 {
			t.Fatalf("idle timeout should project offline: %+v err=%v", got, err)
		}
	})
	t.Run("missing-and-future-unknown", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			future bool
		}{{"missing", false}, {"future", true}} {
			t.Run(tc.name, func(t *testing.T) {
				s := activityStore(t)
				configureActivity(t, s, "da", "ta", 30, 0)
				if tc.future {
					setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC().Add(10*time.Second))
				}
				if err := s.db.Model(&model.Device{}).Where("tenant_id=? AND id=?", "ta", "da").Update("is_online", 1).Error; err != nil {
					t.Fatal(err)
				}
				got, err := s.ProjectCurrent(context.Background(), "ta", "da")
				if err != nil || got.Status != "unknown" || got.Changed || got.Device.IsOnline != 1 {
					t.Fatalf("unknown activity changed status: %+v err=%v", got, err)
				}
				if countActivityRows(t, s, "device_status_history", "tenant_id=? AND device_id=?", "ta", "da") != 0 {
					t.Fatal("unknown activity wrote history")
				}
			})
		}
	})
}

func TestProjectCurrentEpochFenceAndGatewaySourcePath(t *testing.T) {
	t.Run("old-epoch-unknown", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 30, 0)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())
		if err := s.db.Exec(`UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
			t.Fatal(err)
		}
		got, err := s.ProjectCurrent(context.Background(), "ta", "da")
		if err != nil || got.Status != "unknown" || got.Changed {
			t.Fatalf("old activity survived epoch fence: %+v err=%v", got, err)
		}
	})
	t.Run("gateway-child-and-reparent", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Exec(`INSERT INTO broker_transport_bindings VALUES('ta','gateway','c',4,'active'); INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online,parent_id) VALUES('child','ta','enabled','active','n-child','{}',0,'gateway')`).Error; err != nil {
			t.Fatal(err)
		}
		configureActivity(t, s, "child", "ta", 30, 0)
		setActivity(t, s, "ta", "child", "gateway", "c", 4, time.Now().UTC())
		owns, err := s.OwnsDevice(context.Background(), "ta", "child")
		if err != nil || !owns {
			t.Fatalf("activity child should be owned: owns=%v err=%v", owns, err)
		}
		got, err := s.ProjectCurrent(context.Background(), "ta", "child")
		if err != nil || got.Status != "online" || !got.Changed {
			t.Fatalf("gateway source path should project child online: %+v err=%v", got, err)
		}
		if err := s.db.Model(&model.Device{}).Where("tenant_id=? AND id=?", "ta", "child").Update("parent_id", "other-gateway").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.ProjectCurrent(context.Background(), "ta", "child"); err == nil {
			t.Fatal("reparented activity unexpectedly passed source-path validation")
		}
		var online int16
		if err := s.db.Raw(`SELECT is_online FROM devices WHERE tenant_id='ta' AND id='child'`).Scan(&online).Error; err != nil || online != 1 {
			t.Fatalf("reparent rejection changed child status: is_online=%d err=%v", online, err)
		}
	})
}

func TestProjectCurrentTransitionFailureRollsBackTogether(t *testing.T) {
	s := activityStore(t)
	configureActivity(t, s, "da", "ta", 30, 0)
	setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())
	if err := s.db.Exec(`CREATE FUNCTION fail_activity_effect() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$; CREATE TRIGGER fail_activity_effect BEFORE INSERT ON broker_ingress_effects FOR EACH ROW EXECUTE FUNCTION fail_activity_effect()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectCurrent(context.Background(), "ta", "da"); err == nil {
		t.Fatal("expected automation-effect insert to fail")
	}
	var online int16
	if err := s.db.Raw(`SELECT is_online FROM devices WHERE tenant_id='ta' AND id='da'`).Scan(&online).Error; err != nil || online != 0 {
		t.Fatalf("device status escaped rolled-back transaction: %d err=%v", online, err)
	}
	for _, table := range []string{"device_status_history", "broker_ingress_receipts", "broker_ingress_effects"} {
		where := "tenant_id='ta' AND device_id='da'"
		if table == "broker_ingress_receipts" {
			where += " AND stream='state_transition'"
		}
		if table == "broker_ingress_effects" {
			where += " AND effect_kind='automation'"
		}
		if got := countActivityRows(t, s, table, where); got != 0 {
			t.Fatalf("rollback left %d rows in %s", got, table)
		}
	}
	if err := s.db.Exec(`DROP TRIGGER fail_activity_effect ON broker_ingress_effects; DROP FUNCTION fail_activity_effect()`).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := s.ProjectCurrent(context.Background(), "ta", "da")
	if err != nil || !recovered.Changed || recovered.Status != "online" {
		t.Fatalf("recovery after rollback failed: %+v err=%v", recovered, err)
	}
}

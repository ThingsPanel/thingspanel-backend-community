package brokerstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/model"
)

func lockedCommandContext(s *Store, tenant, device, root string) ([]byte, error) {
	var fingerprint []byte
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var binding struct {
			TenantID, DeviceID string
			BrokerCell         string
			RouteEpoch         uint64
			Phase              string
		}
		if err := tx.Table("broker_transport_bindings").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND device_id=?", tenant, root).Take(&binding).Error; err != nil {
			return err
		}
		var err error
		fingerprint, err = CommandContext(context.Background(), tx, tenant, device, root)
		return err
	})
	return fingerprint, err
}

func TestCommandContextFreshOnlineFingerprintAndReadOnly(t *testing.T) {
	s := activityStore(t)
	configureActivity(t, s, "da", "ta", 60, 0)
	observed := time.Now().UTC()
	setActivity(t, s, "ta", "da", "da", "c", 1, observed)
	if err := s.db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES('ta','da','c','instance-a','boot-a','connection-a',1,1,true,?,?)`, observed, observed).Error; err != nil {
		t.Fatal(err)
	}
	var nextBefore time.Time
	if err := s.db.Raw(`SELECT next_check_at FROM broker_telemetry_activity WHERE tenant_id='ta' AND device_id='da'`).Scan(&nextBefore).Error; err != nil {
		t.Fatal(err)
	}
	receiptBefore := countActivityRows(t, s, "broker_ingress_receipts", "tenant_id='ta' AND device_id='da'")
	historyBefore := countActivityRows(t, s, "device_status_history", "tenant_id='ta' AND device_id='da'")
	effectsBefore := countActivityRows(t, s, "broker_ingress_effects", "tenant_id='ta' AND device_id='da'")
	first, err := lockedCommandContext(s, "ta", "da", "da")
	if err != nil || len(first) != 32 {
		t.Fatalf("fresh online command context len=%d err=%v", len(first), err)
	}
	second, err := lockedCommandContext(s, "ta", "da", "da")
	if err != nil || string(first) != string(second) {
		t.Fatalf("unchanged observations should have stable hash: equal=%v err=%v", string(first) == string(second), err)
	}
	assertReadOnly := func(expectedNext time.Time, expectedReceipts int64) {
		t.Helper()
		var gotNext time.Time
		if err := s.db.Raw(`SELECT next_check_at FROM broker_telemetry_activity WHERE tenant_id='ta' AND device_id='da'`).Scan(&gotNext).Error; err != nil {
			t.Fatal(err)
		}
		if !gotNext.Equal(expectedNext) {
			t.Fatalf("command context changed next_check_at: before=%s after=%s", expectedNext, gotNext)
		}
		if got := countActivityRows(t, s, "broker_ingress_receipts", "tenant_id='ta' AND device_id='da'"); got != expectedReceipts {
			t.Fatalf("command context wrote receipt: want=%d got=%d", expectedReceipts, got)
		}
		if got := countActivityRows(t, s, "device_status_history", "tenant_id='ta' AND device_id='da'"); got != historyBefore {
			t.Fatalf("command context wrote status history: before=%d after=%d", historyBefore, got)
		}
		if got := countActivityRows(t, s, "broker_ingress_effects", "tenant_id='ta' AND device_id='da'"); got != effectsBefore {
			t.Fatalf("command context wrote effects: before=%d after=%d", effectsBefore, got)
		}
	}
	assertReadOnly(nextBefore, receiptBefore)
	setActivity(t, s, "ta", "da", "da", "c", 1, observed.Add(time.Millisecond))
	var nextAfterTelemetry time.Time
	if err := s.db.Raw(`SELECT next_check_at FROM broker_telemetry_activity WHERE tenant_id='ta' AND device_id='da'`).Scan(&nextAfterTelemetry).Error; err != nil {
		t.Fatal(err)
	}
	receiptsAfterTelemetry := countActivityRows(t, s, "broker_ingress_receipts", "tenant_id='ta' AND device_id='da'")
	third, err := lockedCommandContext(s, "ta", "da", "da")
	if err != nil || string(first) == string(third) {
		t.Fatalf("new telemetry receipt must change fingerprint: equal=%v err=%v", string(first) == string(third), err)
	}
	if err := s.db.Exec(`UPDATE broker_connection_observations SET event_seq=2,online=false,observed_at=now(),received_at=now() WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
		t.Fatal(err)
	}
	fourth, err := lockedCommandContext(s, "ta", "da", "da")
	if err != nil || string(third) == string(fourth) {
		t.Fatalf("connection tombstone must change fingerprint: equal=%v err=%v", string(third) == string(fourth), err)
	}
	assertReadOnly(nextAfterTelemetry, receiptsAfterTelemetry)
}

func TestCommandContextRequiresFreshOnlineAndCurrentRoute(t *testing.T) {
	t.Run("expired-activity", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 5, 0)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().Add(-10*time.Second))
		if _, err := lockedCommandContext(s, "ta", "da", "da"); !errors.Is(err, ErrFenced) {
			t.Fatalf("expired heartbeat error=%v, want fenced", err)
		}
	})
	t.Run("old-epoch", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 60, 0)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now())
		if err := s.db.Exec(`UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := lockedCommandContext(s, "ta", "da", "da"); !errors.Is(err, ErrFenced) {
			t.Fatalf("old epoch error=%v", err)
		}
	})
	t.Run("draining-route", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 60, 0)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now())
		if err := s.db.Exec(`UPDATE broker_transport_bindings SET phase='draining' WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := lockedCommandContext(s, "ta", "da", "da"); !errors.Is(err, ErrFenced) {
			t.Fatalf("draining route error=%v", err)
		}
	})
	t.Run("cross-tenant", func(t *testing.T) {
		s := activityStore(t)
		if _, err := lockedCommandContext(s, "other", "da", "da"); err == nil {
			t.Fatal("cross-tenant command context accepted")
		}
	})
	t.Run("missing-child-heartbeat", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Exec(`
			INSERT INTO broker_transport_bindings VALUES('ta','gateway','c',4,'active');
			INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online) VALUES('gateway','ta','enabled','active','n-gateway','{}',0);
			INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online,parent_id) VALUES('child','ta','enabled','active','n-child','{}',0,'gateway');
			INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES('ta','gateway','c','i','boot','root-on',4,1,true,now(),now());
		`).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := lockedCommandContext(s, "ta", "child", "gateway"); !errors.Is(err, ErrFenced) {
			t.Fatalf("child without heartbeat error=%v", err)
		}
	})
	t.Run("current-parent-path-required", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Exec(`
			INSERT INTO broker_transport_bindings VALUES('ta','gateway','c',4,'active');
			INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online,parent_id) VALUES('child','ta','enabled','active','n-child','{}',0,'different-gateway');
		`).Error; err != nil {
			t.Fatal(err)
		}
		configureActivity(t, s, "child", "ta", 60, 0)
		setActivity(t, s, "ta", "child", "gateway", "c", 4, time.Now())
		if _, err := lockedCommandContext(s, "ta", "child", "gateway"); !errors.Is(err, ErrFenced) {
			t.Fatalf("reparented child error=%v", err)
		}
	})
	t.Run("valid-child-fresh-heartbeat", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Exec(`
			INSERT INTO broker_transport_bindings VALUES('ta','gateway','c',4,'active');
			INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online) VALUES('gateway','ta','enabled','active','n-gateway','{}',0);
			INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online,parent_id) VALUES('child','ta','enabled','active','n-child','{}',0,'gateway');
		`).Error; err != nil {
			t.Fatal(err)
		}
		configureActivity(t, s, "child", "ta", 60, 0)
		setActivity(t, s, "ta", "child", "gateway", "c", 4, time.Now())
		state, stateErr := s.InspectCurrent(context.Background(), "ta", "child")
		if stateErr != nil || state.Status != "online" {
			var config model.DeviceConfig
			_ = s.db.Where("id='cfg-child'").Take(&config).Error
			var activity activityRow
			_ = s.db.Where("tenant_id=? AND device_id=?", "ta", "child").Take(&activity).Error
			t.Fatalf("fixture not online: state=%+v err=%v config=%v activity=%+v", state, stateErr, derefConfig(config.OtherConfig), activity)
		}
		fingerprint, err := lockedCommandContext(s, "ta", "child", "gateway")
		if err != nil || len(fingerprint) != 32 {
			var route routeRow
			_ = s.db.Raw(`SELECT broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id='ta' AND device_id='gateway'`).Scan(&route).Error
			var child model.Device
			var activity activityRow
			_ = s.db.Where("tenant_id='ta' AND id='child'").Take(&child).Error
			_ = s.db.Where("tenant_id='ta' AND device_id='child'").Take(&activity).Error
			t.Fatalf("fresh child context length=%d err=%v route=%+v child=(parent=%v enabled=%s active=%s config=%v) activity=%+v", len(fingerprint), err, route, child.ParentID, child.IsEnabled, child.ActivateFlag, child.DeviceConfigID, activity)
		}
	})
	t.Run("root-must-be-active", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 60, 0)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now())
		if err := s.db.Model(&model.Device{}).Where("tenant_id='ta' AND id='da'").Update("activate_flag", "inactive").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := lockedCommandContext(s, "ta", "da", "da"); err == nil {
			t.Fatal("inactive root accepted")
		}
	})
	t.Run("root-must-exist-in-tenant", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 60, 0)
		setActivity(t, s, "ta", "da", "missing-root", "c", 1, time.Now())
		if _, err := lockedCommandContext(s, "ta", "da", "missing-root"); err == nil {
			t.Fatal("missing tenant-scoped root was accepted")
		}
	})
}

func derefConfig(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}

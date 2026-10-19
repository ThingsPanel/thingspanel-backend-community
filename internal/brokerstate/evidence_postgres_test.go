package brokerstate

import (
	"context"
	"testing"
	"time"

	"project/internal/model"
)

func TestInspectCurrentHeartbeatDeadlineAndReadOnly(t *testing.T) {
	s := activityStore(t)
	configureActivity(t, s, "da", "ta", 2, 0)
	setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())

	var beforeNext time.Time
	if err := s.db.Raw(`SELECT next_check_at FROM broker_telemetry_activity WHERE tenant_id='ta' AND device_id='da'`).Scan(&beforeNext).Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		name     string
		observed time.Time
		status   string
		fresh    bool
	}{{"fresh", time.Now().UTC(), "online", true}, {"expired", time.Now().UTC().Add(-4 * time.Second), "offline", true}} {
		t.Run(want.name, func(t *testing.T) {
			if want.name == "expired" {
				if err := s.db.Exec(`UPDATE broker_telemetry_activity SET observed_at=? WHERE tenant_id='ta' AND device_id='da'`, want.observed).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.InspectCurrent(context.Background(), "ta", "da")
			if err != nil || got.Status != want.status || got.Fresh != want.fresh || got.RouteEpoch != 1 || got.RoutePhase != "active" {
				t.Fatalf("InspectCurrent=%+v err=%v, want status=%s fresh=%v", got, err, want.status, want.fresh)
			}
		})
	}
	var afterNext time.Time
	if err := s.db.Raw(`SELECT next_check_at FROM broker_telemetry_activity WHERE tenant_id='ta' AND device_id='da'`).Scan(&afterNext).Error; err != nil {
		t.Fatal(err)
	}
	if !afterNext.Equal(beforeNext) {
		t.Fatalf("read changed heartbeat schedule: before=%s after=%s", beforeNext, afterNext)
	}
	var online int16
	if err := s.db.Raw(`SELECT is_online FROM devices WHERE tenant_id='ta' AND id='da'`).Scan(&online).Error; err != nil || online != 0 {
		t.Fatalf("read changed legacy device flag: online=%d err=%v", online, err)
	}
	for _, table := range []string{"device_status_history", "broker_ingress_receipts", "broker_ingress_effects", "broker_state_projection_queue"} {
		var count int64
		query := s.db.Table(table)
		if table == "broker_ingress_receipts" {
			query = query.Where("stream='state_transition'")
		}
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("read wrote %d rows to %s, err=%v", count, table, err)
		}
	}
}

func TestInspectCurrentFencesTenantDisabledDrainingAndEpoch(t *testing.T) {
	t.Run("cross-tenant-is-not-found", func(t *testing.T) {
		s := activityStore(t)
		if _, err := s.InspectCurrent(context.Background(), "other", "da"); err != ErrDeviceUnavailable {
			t.Fatalf("cross-tenant lookup error=%v, want ErrDeviceUnavailable", err)
		}
	})
	t.Run("disabled-device-is-unknown", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Model(&model.Device{}).Where("tenant_id=? AND id=?", "ta", "da").Update("is_enabled", "disabled").Error; err != nil {
			t.Fatal(err)
		}
		got, err := s.InspectCurrent(context.Background(), "ta", "da")
		if err != nil || got.Status != "unknown" || got.Fresh {
			t.Fatalf("disabled device evidence=%+v err=%v", got, err)
		}
	})
	t.Run("inactive-device-is-unknown", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Model(&model.Device{}).Where("tenant_id=? AND id=?", "ta", "da").Update("activate_flag", "inactive").Error; err != nil {
			t.Fatal(err)
		}
		got, err := s.InspectCurrent(context.Background(), "ta", "da")
		if err != nil || got.Status != "unknown" || got.Fresh {
			t.Fatalf("inactive device evidence=%+v err=%v", got, err)
		}
	})
	t.Run("draining-is-unknown", func(t *testing.T) {
		s := activityStore(t)
		if err := s.db.Exec(`UPDATE broker_transport_bindings SET phase='draining' WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
			t.Fatal(err)
		}
		got, err := s.InspectCurrent(context.Background(), "ta", "da")
		if err != nil || got.Status != "unknown" || got.RoutePhase != "draining" {
			t.Fatalf("draining evidence=%+v err=%v", got, err)
		}
	})
	t.Run("old-epoch-is-unknown", func(t *testing.T) {
		s := activityStore(t)
		configureActivity(t, s, "da", "ta", 30, 0)
		setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())
		if err := s.db.Exec(`UPDATE broker_transport_bindings SET route_epoch=2 WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
			t.Fatal(err)
		}
		got, err := s.InspectCurrent(context.Background(), "ta", "da")
		if err != nil || got.Status != "unknown" || got.RouteEpoch != 2 {
			t.Fatalf("old epoch evidence=%+v err=%v", got, err)
		}
	})
}

func TestInspectCurrentGatewayChildUnknownWithoutChildHeartbeat(t *testing.T) {
	s := activityStore(t)
	if err := s.db.Exec(`
		INSERT INTO broker_transport_bindings VALUES('ta','gateway','c',4,'active');
		INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online,parent_id)
		VALUES('child','ta','enabled','active','n-child','{}',0,'gateway');
		INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at)
		VALUES('ta','gateway','c','instance','boot','gateway-connection',4,1,true,now(),now());
		INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at)
		VALUES('ta','child','c','old-instance','old-boot','old-child-close',4,2,false,now(),now());
	`).Error; err != nil {
		t.Fatal(err)
	}
	setActivity(t, s, "ta", "child", "gateway", "c", 4, time.Now().UTC())
	got, err := s.InspectCurrent(context.Background(), "ta", "child")
	if err != nil || got.Status != "unknown" || got.Fresh || got.TransportDeviceID != "gateway" {
		t.Fatalf("child must not inherit gateway online or stale child history: %+v err=%v", got, err)
	}
}

func TestInspectCurrentGatewayChildOldParentUnknown(t *testing.T) {
	s := activityStore(t)
	if err := s.db.Exec(`
		INSERT INTO broker_transport_bindings VALUES('ta','gateway','c',4,'active');
		INSERT INTO devices(id,tenant_id,is_enabled,activate_flag,device_number,voucher,is_online,parent_id)
		VALUES('child','ta','enabled','active','n-child','{}',0,'other-gateway');
	`).Error; err != nil {
		t.Fatal(err)
	}
	configureActivity(t, s, "child", "ta", 30, 0)
	setActivity(t, s, "ta", "child", "gateway", "c", 4, time.Now().UTC())
	got, err := s.InspectCurrent(context.Background(), "ta", "child")
	if err != nil || got.Status != "unknown" || got.Fresh {
		t.Fatalf("reparented child evidence=%+v err=%v", got, err)
	}
}

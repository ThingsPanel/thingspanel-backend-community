package service

import (
	"context"
	"errors"
	"project/internal/automationbridge"
	"testing"
)

func TestPostgresNativeEffectRetryKeepsIdentityAndDoesNotRunLegacy(t *testing.T) {
	db := automationPG(t)
	fixture := seedAutomationFixture(t, db)
	var deliveries []automationbridge.Uplink
	deliver := func(ctx context.Context, event automationbridge.Uplink) error {
		deliveries = append(deliveries, event)
		// A separate transaction can lock the effect: no network call may retain it.
		var one int
		if err := db.WithContext(ctx).Raw("SELECT 1 FROM broker_ingress_effects WHERE id=? FOR UPDATE NOWAIT", fixture.effect).Scan(&one).Error; err != nil {
			t.Errorf("effect lock held during HTTP: %v", err)
		}
		if len(deliveries) == 1 {
			return errors.New("simulated response loss")
		}
		return nil
	}
	if worked, err := ProcessBrokerNativeAutomationEffect(context.Background(), db, fixture.cell, deliver); !worked || err == nil {
		t.Fatal("transport failure was not visible")
	}
	if err := db.Exec("UPDATE broker_ingress_effects SET retry_after=now() WHERE id=?", fixture.effect).Error; err != nil {
		t.Fatal(err)
	}
	if worked, err := ProcessBrokerNativeAutomationEffect(context.Background(), db, fixture.cell, deliver); !worked || err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if len(deliveries) != 2 || deliveries[0].MessageID != fixture.effect || deliveries[0] != deliveries[1] || deliveries[0].TenantID != fixture.tenant {
		t.Fatal("delivery identity changed across retry")
	}
	var effect automationEffect
	db.Where("id=?", fixture.effect).Take(&effect)
	if effect.State != "completed" {
		t.Fatalf("effect state %s", effect.State)
	}
	var legacyActions int64
	db.Table("broker_automation_actions").Where("effect_id=?", fixture.effect).Count(&legacyActions)
	if legacyActions != 0 {
		t.Fatal("new engine also ran the legacy planner")
	}
}

func TestPostgresNativeEffectRecoversExpiredLease(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	db.Exec("UPDATE broker_ingress_effects SET state='dispatching',attempts=1,retry_after=now()-interval '1 second' WHERE id=?", f.effect)
	delivered := false
	worked, err := ProcessBrokerNativeAutomationEffect(context.Background(), db, f.cell, func(_ context.Context, e automationbridge.Uplink) error {
		delivered = true
		if e.MessageID != f.effect {
			t.Error("recovered lease changed event identity")
		}
		return nil
	})
	if !worked || err != nil || !delivered {
		t.Fatalf("lease recovery failed: %v", err)
	}
}

func TestPostgresNativeEffectFencesChangedTransport(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	db.Exec("UPDATE broker_transport_bindings SET route_epoch=route_epoch+1 WHERE device_id=?", f.device)
	_, err := ProcessBrokerNativeAutomationEffect(context.Background(), db, f.cell, func(context.Context, automationbridge.Uplink) error {
		t.Fatal("fenced source reached Encore")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var e automationEffect
	db.Where("id=?", f.effect).Take(&e)
	if e.State != "fenced" {
		t.Fatalf("state=%s", e.State)
	}
}

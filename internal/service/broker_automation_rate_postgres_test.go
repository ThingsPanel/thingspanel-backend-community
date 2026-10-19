package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"project/internal/model"
)

func addAutomationTriggerEffect(t *testing.T, db automationDB, f automationFixture, deviceID string) string {
	t.Helper()
	receipt, effect := uuid.NewString(), uuid.NewString()
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload) VALUES(?,?,?,'event',?,?,?)`, receipt, f.tenant, deviceID, f.cell, f.epoch, []byte(`{}`)).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(automationTrigger{Type: model.TRIGGER_PARAM_TYPE_TEL, Values: map[string]interface{}{"temperature": float64(24)}, Cell: f.cell, Epoch: f.epoch})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload,state) VALUES(?,?,?,?,'automation',?::jsonb,'pending')`, effect, receipt, f.tenant, deviceID, string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	return effect
}

// automationDB keeps the helper's concrete type local to the existing package
// fixture without expanding its public API.
type automationDB = *gorm.DB

func TestPostgresAutomationRateWindowBurstRefillAndDeviceIsolation(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	for i := 0; i < 11; i++ {
		if i > 0 {
			addAutomationTriggerEffect(t, db, f, f.device)
		}
		worked, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
		if err != nil || !worked {
			t.Fatalf("plan trigger %d: worked=%v err=%v", i+1, worked, err)
		}
	}
	var commands int64
	if err := db.Table("broker_command_requests").Where("tenant_id=? AND device_id=?", f.tenant, f.device).Count(&commands).Error; err != nil || commands != 10 {
		t.Fatalf("same-device burst produced %d commands, want 10; err=%v", commands, err)
	}
	var skipped int64
	if err := db.Table("broker_automation_actions").Where("tenant_id=? AND scene_id=? AND state='skipped' AND action_id='@policy'", f.tenant, f.scene).Count(&skipped).Error; err != nil || skipped != 1 {
		t.Fatalf("11th same-device trigger did not leave one rate-limit audit: skipped=%d err=%v", skipped, err)
	}
	var firstTokens float64
	if err := db.Raw(`SELECT tokens FROM broker_automation_execution_windows WHERE tenant_id=? AND scene_id=? AND device_id=?`, f.tenant, f.scene, f.device).Scan(&firstTokens).Error; err != nil || firstTokens < 0 || firstTokens >= 1 {
		t.Fatalf("burst window tokens=%f want less than one available token err=%v", firstTokens, err)
	}

	// A second trigger device under the same scene receives an independent
	// bucket. The shared policy has a separate condition group for this device.
	otherDevice := uuid.NewString()
	device := model.Device{ID: otherDevice, TenantID: f.tenant, Voucher: "{}", IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "second-device", IsOnline: 1}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES(?,?,?,?,'active')`, f.tenant, otherDevice, f.cell, f.epoch).Error; err != nil {
		t.Fatal(err)
	}
	paramType, param, operator := "TEL", "temperature", ">"
	condition := model.DeviceTriggerCondition{ID: uuid.NewString(), SceneAutomationID: f.scene, Enabled: "Y", GroupID: "g2", TriggerConditionType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, TriggerSource: &otherDevice, TriggerParamType: &paramType, TriggerParam: &param, TriggerOperator: &operator, TriggerValue: "20", TenantID: f.tenant}
	if err := db.Create(&condition).Error; err != nil {
		t.Fatal(err)
	}
	otherEffect := addAutomationTriggerEffect(t, db, f, otherDevice)
	worked, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
	if err != nil || !worked {
		t.Fatalf("other device plan: worked=%v err=%v", worked, err)
	}
	var accepted int64
	if err := db.Table("broker_automation_actions").Where("effect_id=? AND state='accepted'", otherEffect).Count(&accepted).Error; err != nil || accepted != 1 {
		t.Fatalf("other device shared the exhausted bucket: accepted=%d err=%v", accepted, err)
	}
	var secondTokens float64
	if err := db.Raw(`SELECT tokens FROM broker_automation_execution_windows WHERE tenant_id=? AND scene_id=? AND device_id=?`, f.tenant, f.scene, otherDevice).Scan(&secondTokens).Error; err != nil || secondTokens != 9 {
		t.Fatalf("second device bucket tokens=%f want 9 err=%v", secondTokens, err)
	}

	// Refill is driven by elapsed database time. Backdate the stored window to
	// avoid a wall-clock sleep while exercising the actual SQL bucket update.
	if err := db.Exec(`UPDATE broker_automation_execution_windows SET last_evaluated_at=now()-interval '3 seconds' WHERE tenant_id=? AND scene_id=? AND device_id=?`, f.tenant, f.scene, f.device).Error; err != nil {
		t.Fatal(err)
	}
	refillEffect := addAutomationTriggerEffect(t, db, f, f.device)
	worked, err = ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
	if err != nil || !worked {
		t.Fatalf("refilled plan: worked=%v err=%v", worked, err)
	}
	if err := db.Table("broker_automation_actions").Where("effect_id=? AND state='accepted'", refillEffect).Count(&accepted).Error; err != nil || accepted != 1 {
		t.Fatalf("refilled device did not resume command planning: accepted=%d err=%v", accepted, err)
	}
}

func TestPostgresAutomationFailedPlanDoesNotConsumeWindowToken(t *testing.T) {
	db := automationPG(t)
	f := seedAutomationFixture(t, db)
	if err := db.Model(&model.SceneActionInfo{}).Where("scene_id=? AND tenant_id=?", f.scene, f.tenant).Update("action_value", "{invalid-json").Error; err != nil {
		t.Fatal(err)
	}
	worked, err := ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
	if err == nil || !worked {
		t.Fatalf("invalid action plan should fail and remain retryable: worked=%v err=%v", worked, err)
	}
	var windows int64
	if err := db.Table("broker_automation_execution_windows").Where("tenant_id=? AND scene_id=? AND device_id=?", f.tenant, f.scene, f.device).Count(&windows).Error; err != nil || windows != 0 {
		t.Fatalf("failed plan consumed/created limiter state: windows=%d err=%v", windows, err)
	}
	if err := db.Model(&model.SceneActionInfo{}).Where("scene_id=? AND tenant_id=?", f.scene, f.tenant).Update("action_value", `{"method":"switch","params":{"on":true}}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE broker_ingress_effects SET retry_after=now()-interval '1 second' WHERE id=?`, f.effect).Error; err != nil {
		t.Fatal(err)
	}
	worked, err = ProcessBrokerAutomationEffect(context.Background(), db, f.cell)
	if err != nil || !worked {
		t.Fatalf("repaired plan failed: worked=%v err=%v", worked, err)
	}
	var tokens float64
	if err := db.Raw(`SELECT tokens FROM broker_automation_execution_windows WHERE tenant_id=? AND scene_id=? AND device_id=?`, f.tenant, f.scene, f.device).Scan(&tokens).Error; err != nil || tokens != 9 {
		t.Fatalf("recovered plan did not retain initial burst: tokens=%f err=%v", tokens, err)
	}
}

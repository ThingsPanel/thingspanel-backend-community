package service

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/automationbridge"
	"project/internal/model"
	"time"
)

// ProcessBrokerNativeAutomationEffect releases all row locks before network I/O.
// A crashed lease is retried with the same effect ID, which Encore deduplicates.
func ProcessBrokerNativeAutomationEffect(ctx context.Context, db *gorm.DB, cell string, deliver func(context.Context, automationbridge.Uplink) error) (bool, error) {
	var candidate automationEffect
	err := db.WithContext(ctx).Table("broker_ingress_effects AS e").Select("e.*").Joins("JOIN broker_ingress_receipts r ON r.id=e.receipt_id").Where("e.state IN ('pending','dispatching') AND e.effect_kind='automation' AND e.retry_after<=now() AND r.broker_cell=?", cell).Order("e.updated_at,e.id").Take(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var claimed automationEffect
	var event automationbridge.Uplink
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source struct {
			DeviceID   string
			RouteEpoch uint64
		}
		if err := tx.Table("broker_ingress_receipts").Select("device_id,route_epoch").Where("id=? AND tenant_id=? AND broker_cell=?", candidate.ReceiptID, candidate.TenantID, cell).Take(&source).Error; err != nil {
			return err
		}
		var binding struct {
			Phase, BrokerCell string
			RouteEpoch        uint64
		}
		if err := tx.Table("broker_transport_bindings").Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id=? AND tenant_id=?", source.DeviceID, candidate.TenantID).Take(&binding).Error; err != nil {
			return err
		}
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("id=? AND state IN ('pending','dispatching') AND retry_after<=now()", candidate.ID).Take(&claimed)
		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if q.Error != nil {
			return q.Error
		}
		fence := func(reason string) error {
			claimed.ID = ""
			return tx.Model(&automationEffect{}).Where("id=?", candidate.ID).Updates(map[string]any{"state": "fenced", "last_error": reason, "updated_at": time.Now()}).Error
		}
		if binding.Phase != "active" || binding.BrokerCell != cell || binding.RouteEpoch != source.RouteEpoch {
			return fence("source transport route changed")
		}
		if time.Since(claimed.CreatedAt) > 2*time.Minute || claimed.CreatedAt.After(time.Now().Add(5*time.Second)) {
			return fence("automation trigger outside execution window")
		}
		var trigger automationTrigger
		if json.Unmarshal(claimed.Payload, &trigger) != nil || trigger.Cell != cell || trigger.Epoch != source.RouteEpoch {
			return fence("invalid persisted trigger route")
		}
		var device model.Device
		if err := tx.Where("id=? AND tenant_id=? AND is_enabled='enabled'", claimed.DeviceID, claimed.TenantID).Take(&device).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fence("logical device is unavailable")
			}
			return err
		}
		current := device
		for depth := 0; current.ID != source.DeviceID; depth++ {
			if depth >= 7 || current.ParentID == nil || *current.ParentID == "" {
				return fence("logical device no longer belongs to source transport")
			}
			var parent model.Device
			if err := tx.Where("id=? AND tenant_id=? AND is_enabled='enabled'", *current.ParentID, claimed.TenantID).Take(&parent).Error; err != nil {
				return err
			}
			current = parent
		}
		class := map[string]string{model.TRIGGER_PARAM_TYPE_TEL: "telemetry", model.TRIGGER_PARAM_TYPE_ATTR: "attribute", model.TRIGGER_PARAM_TYPE_EVT: "event", model.TRIGGER_PARAM_TYPE_STATUS: "status"}[trigger.Type]
		if class == "" {
			return fence("unsupported automation signal")
		}
		values, err := json.Marshal(trigger.Values)
		if err != nil {
			return err
		}
		event = automationbridge.Uplink{TenantID: claimed.TenantID, DeviceID: claimed.DeviceID, MessageID: claimed.ID, ObservedAt: claimed.CreatedAt.UTC().Format(time.RFC3339Nano), Class: class, Values: string(values), Source: "community-durable-ingress"}
		claimed.Attempts++
		return tx.Model(&automationEffect{}).Where("id=?", claimed.ID).Updates(map[string]any{"state": "dispatching", "attempts": claimed.Attempts, "retry_after": time.Now().Add(30 * time.Second), "updated_at": time.Now()}).Error
	})
	if err != nil {
		return true, err
	}
	if claimed.ID == "" {
		return true, nil
	}
	err = deliver(ctx, event)
	state, detail := "completed", ""
	if err != nil {
		state, detail = "pending", err.Error()
		if claimed.Attempts >= 5 {
			state = "failed"
		}
	}
	finishErr := db.WithContext(ctx).Model(&automationEffect{}).Where("id=? AND state='dispatching' AND attempts=?", claimed.ID, claimed.Attempts).Updates(map[string]any{"state": state, "last_error": detail, "retry_after": time.Now().Add(time.Second), "updated_at": time.Now()}).Error
	if finishErr != nil {
		return true, finishErr
	}
	return true, err
}

package storage

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/diagnostics"
	"project/internal/uplinkbinding"
	"time"
)

func checkTelemetryModelBindings(ctx context.Context, tx *gorm.DB, item *telemetryBatchItem) error {
	return checkBatchModelBindings(ctx, tx, []*telemetryBatchItem{item})
}
func checkBatchModelBindings(ctx context.Context, tx *gorm.DB, batch []*telemetryBatchItem) error {
	var all []uplinkbinding.Snapshot
	for _, item := range batch {
		if len(item.bindings) == 0 {
			continue
		}
		found := false
		for _, binding := range item.bindings {
			if binding.TenantID() != item.tenantID {
				return uplinkbinding.ErrFenced
			}
			if binding.DeviceID() == item.deviceID {
				found = true
			}
			all = append(all, binding)
		}
		if !found {
			return uplinkbinding.ErrFenced
		}
	}
	if len(all) == 0 {
		return nil
	}
	return uplinkbinding.CheckAll(ctx, tx, all)
}

// Fenced legacy messages retain the legacy storage contract, but the whole
// logical message and its binding check now share a transaction. Never degrade
// to per-point/unfenced fallback after a model/schema race or failed write.
func (w *telemetryWriter) doFlushFencedLegacy(batch []*telemetryBatchItem) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	history, current, _ := w.deduplicateAndConvert(batch)
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := checkBatchModelBindings(ctx, tx, batch); e != nil {
			return e
		}

		if len(history) == 0 {
			return nil
		}
		if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}, {Name: "ts"}}, DoNothing: true}).Create(&history).Error; e != nil {
			return e
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"ts", "bool_v", "number_v", "string_v", "tenant_id"})}).Create(&current).Error
	})
	if err != nil && len(batch) > 1 {
		for _, item := range batch {
			w.doFlushFencedLegacy([]*telemetryBatchItem{item})
		}
		return
	}
	if err != nil {
		w.metrics.addTelemetryFailed(int64(len(history)))
		w.logger.Warnf("model-fenced telemetry commit rejected: %v", err)
		for _, item := range batch {
			item.observation.Record(diagnostics.LogicalStorageFailed, "数据写入未提交或绑定已变化")
		}
		return
	}
	w.metrics.addTelemetryWritten(int64(len(history)))
	for _, item := range batch {
		item.observation.Record(diagnostics.LogicalCommitted, "")
		if item.afterCommit != nil {
			item.afterCommit()
		}
	}
}

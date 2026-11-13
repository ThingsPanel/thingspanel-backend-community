package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"project/internal/brokerledger"
	"project/internal/model"
)

var errTelemetryRouteFenced = errors.New("trusted telemetry route is fenced")

type telemetryEffectBudget struct {
	Count int64
	Bytes int64
}

type telemetryBinding struct {
	TenantID, DeviceID, BrokerCell, Phase string
	RouteEpoch                            uint64
}

// doFlushTrusted keeps batching while making the final SQL write and its
// automation intent atomic with the authoritative transport route. This does
// not move the ordinary-telemetry MQTT ACK boundary out of the memory bus.
func (w *telemetryWriter) doFlushTrusted(batch []*telemetryBatchItem) {
	accepted, written, failed, err := w.commitTrustedTelemetry(batch)
	if err != nil && len(batch) > 1 {
		// Best-effort fallback retains the SAME fencing transaction per item.
		// Never fall through to the legacy writer after a trusted batch error.
		for _, item := range batch {
			w.doFlushTrusted([]*telemetryBatchItem{item})
		}
		return
	}
	if err != nil {
		w.logger.Warnf("trusted telemetry batch failed; no successful commit: %v", err)
	}
	w.metrics.addTelemetryWritten(int64(written))
	w.metrics.addTelemetryFailed(int64(failed))
	w.metrics.recordTelemetryBatch(written + failed)
	if err == nil {
		for _, item := range accepted {
			if item.afterCommit != nil {
				item.afterCommit()
			}
		}
	}
}
func (w *telemetryWriter) commitTrustedTelemetry(batch []*telemetryBatchItem) (accepted []*telemetryBatchItem, written, failed int, err error) {
	if len(batch) == 0 {
		return
	}
	if w.db == nil {
		return nil, 0, telemetryPointCount(batch), errors.New("trusted telemetry database unavailable")
	}
	duplicatesCommitted := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = w.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Use the same tenant admission lock as ingress before sorted binding locks.
		tenants := map[string]bool{}
		bindings := map[string]*telemetryBatchItem{}
		for _, item := range batch {
			if validTrustedTelemetry(item) != nil {
				if item != nil {
					failed += len(item.points)
				}
				continue
			}
			tenants[item.tenantID] = true
			bindings[item.tenantID+"/"+item.route.TransportDeviceID] = item
		}
		names := make([]string, 0, len(tenants))
		for id := range tenants {
			names = append(names, id)
		}
		sort.Strings(names)
		for _, id := range names {
			if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", id).Error; e != nil {
				return e
			}
		}
		keys := make([]string, 0, len(bindings))
		for key := range bindings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		current := map[string]telemetryBinding{}
		for _, key := range keys {
			item := bindings[key]
			var binding telemetryBinding
			r := tx.Table("broker_transport_bindings").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND device_id=?", item.tenantID, item.route.TransportDeviceID).Take(&binding)
			if r.Error != nil && !errors.Is(r.Error, gorm.ErrRecordNotFound) {
				return r.Error
			}
			if r.Error == nil {
				current[key] = binding
			}
		}
		for _, item := range batch {
			if validTrustedTelemetry(item) != nil {
				continue
			}
			r := item.route
			b, ok := current[item.tenantID+"/"+r.TransportDeviceID]
			if !ok || b.Phase != "active" || b.BrokerCell != r.BrokerCell || b.RouteEpoch != r.RouteEpoch {
				failed += len(item.points)
				continue
			}
			if e := validateTelemetryAncestry(tx, item); errors.Is(e, errTelemetryRouteFenced) {
				failed += len(item.points)
				continue
			} else if e != nil {
				return e
			}
			accepted = append(accepted, item)
		}
		if len(accepted) == 0 {
			return nil
		}
		history, currentRows, duplicates := w.deduplicateAndConvert(accepted)
		if len(history) > 0 {
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}, {Name: "ts"}}, DoNothing: true}).Create(&history)
			if result.Error != nil {
				return result.Error
			}
			written = int(result.RowsAffected)
			if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"ts", "bool_v", "number_v", "string_v", "tenant_id"}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "telemetry_current_datas.ts <= excluded.ts"}}}}).Create(&currentRows).Error; e != nil {
				return e
			}
		}
		budgets := map[string]*telemetryEffectBudget{}
		for _, item := range accepted {
			budget := budgets[item.tenantID]
			if budget == nil {
				budget = &telemetryEffectBudget{}
				if e := tx.Table("broker_ingress_effects").Select("count(*) AS count,coalesce(sum(octet_length(payload::text)),0) AS bytes").Where("tenant_id=? AND state='pending'", item.tenantID).Scan(budget).Error; e != nil {
					return e
				}
				budgets[item.tenantID] = budget
			}
			if e := recordTrustedTelemetry(tx, item, budget); e != nil {
				return e
			}
		}
		duplicatesCommitted = duplicates
		return nil
	})
	if err != nil {
		return nil, 0, telemetryPointCount(batch), err
	}
	if duplicatesCommitted > 0 {
		w.metrics.addTelemetryDuplicates(int64(duplicatesCommitted))
	}
	return
}
func validTrustedTelemetry(item *telemetryBatchItem) error {
	if item == nil || item.route == nil || item.tenantID == "" || item.deviceID == "" || len(item.points) > 4096 {
		return errTelemetryRouteFenced
	}
	r := item.route
	if _, err := uuid.Parse(r.ReceiptID); err != nil {
		return errTelemetryRouteFenced
	}
	if r.TransportDeviceID == "" || r.BrokerCell == "" || r.RouteEpoch == 0 || r.ObservedAt.IsZero() || r.ObservedAt.UnixMilli() != item.timestamp || r.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return errTelemetryRouteFenced
	}
	return nil
}
func telemetryPointCount(items []*telemetryBatchItem) int {
	n := 0
	for _, item := range items {
		if item != nil {
			n += len(item.points)
		}
	}
	return n
}
func validateTelemetryAncestry(tx *gorm.DB, item *telemetryBatchItem) error {
	id := item.deviceID
	visited := map[string]bool{}
	for depth := 0; depth < 8; depth++ {
		if visited[id] {
			return errTelemetryRouteFenced
		}
		visited[id] = true
		var device model.Device
		e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND is_enabled='enabled'", id, item.tenantID).Take(&device).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return errTelemetryRouteFenced
		}
		if e != nil {
			return e
		}
		if id == item.route.TransportDeviceID {
			if device.ParentID != nil && *device.ParentID != "" {
				return errTelemetryRouteFenced
			}
			return nil
		}
		if device.ParentID == nil || *device.ParentID == "" {
			return errTelemetryRouteFenced
		}
		id = *device.ParentID
	}
	return errTelemetryRouteFenced
}
func recordTrustedTelemetry(tx *gorm.DB, item *telemetryBatchItem, budget *telemetryEffectBudget) error {
	values := map[string]interface{}{}
	for _, p := range item.points {
		if _, exists := values[p.Key]; exists {
			return errors.New("duplicate trusted telemetry point key")
		}
		values[p.Key] = p.Value
	}
	payload, e := json.Marshal(values)
	if e != nil {
		return e
	}
	if len(payload) > 1<<20 {
		return errors.New("trusted telemetry sample exceeds payload limit")
	}
	r := item.route
	digest := sha256.Sum256(payload)
	meta, e := json.Marshal(map[string]interface{}{"source": "trusted_telemetry_batch", "timestamp": item.timestamp, "logical_device_id": item.deviceID})
	if e != nil {
		return e
	}
	// A stable platform receipt is created at the actual batch acceptance boundary,
	// not at the earlier best-effort MQTT/bus acknowledgement.
	q := tx.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,created_at,processed_at) VALUES(?,?,?,'telemetry_sample',?,?,?,?::jsonb,?,'processed',?,now()) ON CONFLICT(id) DO NOTHING`, r.ReceiptID, item.tenantID, r.TransportDeviceID, r.BrokerCell, r.RouteEpoch, payload, string(meta), digest[:], r.ObservedAt)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected == 0 {
		var count int64
		if e := tx.Table("broker_ingress_receipts").Where("id=? AND tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=? AND canonical_sha256=? AND stream='telemetry_sample' AND metadata->>'logical_device_id'=?", r.ReceiptID, item.tenantID, r.TransportDeviceID, r.BrokerCell, r.RouteEpoch, digest[:], item.deviceID).Count(&count).Error; e != nil {
			return e
		}
		if count != 1 {
			return errors.New("trusted telemetry receipt conflicts")
		}
	}
	intent, e := json.Marshal(map[string]interface{}{"trigger_param_type": model.TRIGGER_PARAM_TYPE_TEL, "trigger_values": values, "route_epoch": r.RouteEpoch, "broker_cell": r.BrokerCell})
	if e != nil {
		return e
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(r.ReceiptID+"/"+item.deviceID+"/automation")).String()
	var size struct{ Bytes int64 }
	inserted := tx.Raw(`INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload,created_at) VALUES(?,?,?,?,'automation',?::jsonb,?) ON CONFLICT(receipt_id,device_id,effect_kind) DO NOTHING RETURNING octet_length(payload::text) AS bytes`, id, r.ReceiptID, item.tenantID, item.deviceID, string(intent), r.ObservedAt).Scan(&size)
	if inserted.Error != nil {
		return inserted.Error
	}
	if inserted.RowsAffected > 0 {
		budget.Count++
		budget.Bytes += size.Bytes
	}
	if budget.Count > brokerledger.MaxPendingEffects || budget.Bytes > brokerledger.MaxPendingEffectBytes {
		return errors.New("trusted telemetry automation capacity exceeded")
	}

	if e := tx.Exec(`INSERT INTO broker_telemetry_activity(tenant_id,device_id,transport_device_id,broker_cell,route_epoch,receipt_id,observed_at,next_check_at) VALUES(?,?,?,?,?,?,?,now()) ON CONFLICT(tenant_id,device_id) DO UPDATE SET transport_device_id=excluded.transport_device_id,broker_cell=excluded.broker_cell,route_epoch=excluded.route_epoch,receipt_id=excluded.receipt_id,observed_at=excluded.observed_at,next_check_at=now() WHERE broker_telemetry_activity.route_epoch<>excluded.route_epoch OR broker_telemetry_activity.broker_cell<>excluded.broker_cell OR broker_telemetry_activity.transport_device_id<>excluded.transport_device_id OR broker_telemetry_activity.observed_at<=excluded.observed_at`, item.tenantID, item.deviceID, r.TransportDeviceID, r.BrokerCell, r.RouteEpoch, r.ReceiptID, r.ObservedAt).Error; e != nil {
		return e
	}
	return tx.Exec(`INSERT INTO broker_state_projection_queue(tenant_id,device_id) VALUES(?,?) ON CONFLICT(tenant_id,device_id) DO UPDATE SET updated_at=now()`, item.tenantID, item.deviceID).Error
}

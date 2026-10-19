package brokerstate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/brokerledger"
	"project/internal/model"
)

type activityRow struct {
	TenantID, DeviceID, TransportDeviceID, BrokerCell, ReceiptID string
	RouteEpoch                                                   uint64
	ObservedAt                                                   time.Time
	NextCheckAt                                                  time.Time
}

func (activityRow) TableName() string { return "broker_telemetry_activity" }

// ProjectCurrent is the ingress_v1 state owner, including database-backed
// heartbeat/inactivity deadlines. Redis expiry notifications are only hints.
func (s *Store) ProjectCurrent(ctx context.Context, tenant, device string) (Projection, error) {
	return s.project(ctx, tenant, device, true)
}
func (s *Store) OwnsDevice(ctx context.Context, tenant, device string) (bool, error) {
	var count int64
	e := s.db.WithContext(ctx).Raw(`SELECT (SELECT count(*) FROM broker_transport_bindings WHERE tenant_id=? AND device_id=?)+(SELECT count(*) FROM broker_telemetry_activity WHERE tenant_id=? AND device_id=?)`, tenant, device, tenant, device).Scan(&count).Error
	return count > 0, e
}
func (s *Store) PendingCurrent(ctx context.Context, limit int) ([]PendingKey, error) {
	// Expiry and configuration rechecks remain recoverable after a process restart.
	if e := s.db.WithContext(ctx).Exec(`INSERT INTO broker_state_projection_queue(tenant_id,device_id,updated_at) SELECT tenant_id,device_id,now() FROM broker_telemetry_activity WHERE next_check_at<=now() ORDER BY next_check_at,tenant_id,device_id LIMIT 256 ON CONFLICT DO NOTHING`).Error; e != nil {
		return nil, e
	}
	return s.Pending(ctx, limit)
}
func lockCurrentRoute(tx *gorm.DB, tenant, device string) (routeRow, string, error) {
	var activity activityRow
	e := tx.Where("tenant_id=? AND device_id=?", tenant, device).Take(&activity).Error
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return routeRow{}, "", e
	}
	root := device
	if e == nil {
		root = activity.TransportDeviceID
	}
	route, e := lockRoute(tx, tenant, root)
	return route, root, e
}
func currentDecision(tx *gorm.DB, device model.Device, root string, route routeRow) (Decision, error) {
	// Validate the logical device's CURRENT path while holding its transport fence.
	id := device.ID
	for depth := 0; id != root; depth++ {
		if depth >= 7 {
			return Decision{}, ErrFenced
		}
		var child model.Device
		if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND id=? AND is_enabled='enabled'", device.TenantID, id).Take(&child).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return Decision{}, ErrFenced
			}
			return Decision{}, e
		}
		if child.ParentID == nil || *child.ParentID == "" {
			return Decision{}, ErrFenced
		}
		id = *child.ParentID
	}
	var cfg struct {
		Heartbeat int `json:"heartbeat"`
		Timeout   int `json:"online_timeout"`
	}
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		var config model.DeviceConfig
		if e := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", *device.DeviceConfigID, device.TenantID).Take(&config).Error; e != nil {
			return Decision{}, e
		}
		if config.OtherConfig != nil && *config.OtherConfig != "" {
			if e := json.Unmarshal([]byte(*config.OtherConfig), &cfg); e != nil {
				return Decision{}, errors.New("invalid state heartbeat configuration")
			}
		}
	}
	if cfg.Heartbeat < 0 || cfg.Timeout < 0 || cfg.Heartbeat > 31536000 || cfg.Timeout > 31536000 {
		return Decision{}, errors.New("state heartbeat duration outside supported range")
	}
	var activity activityRow
	e := tx.Where("tenant_id=? AND device_id=?", device.TenantID, device.ID).Take(&activity).Error
	hasActivity := e == nil
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return Decision{}, e
	}
	now := time.Now().UTC()
	next := now.Add(time.Minute)
	if hasActivity {
		if e := tx.Model(&activityRow{}).Where("tenant_id=? AND device_id=?", device.TenantID, device.ID).Update("next_check_at", next).Error; e != nil {
			return Decision{}, e
		}
	}
	if cfg.Heartbeat == 0 && cfg.Timeout == 0 {
		return snapshotTx(tx, device.TenantID, device.ID, route.BrokerCell, route.RouteEpoch)
	}
	if !hasActivity || activity.TransportDeviceID != root || activity.BrokerCell != route.BrokerCell || activity.RouteEpoch != route.RouteEpoch || activity.ObservedAt.After(now.Add(5*time.Second)) {
		return Decision{Status: "unknown"}, nil
	}
	interval := cfg.Heartbeat
	if interval == 0 {
		interval = cfg.Timeout
	}
	deadline := activity.ObservedAt.Add(time.Duration(interval) * time.Second)
	result := Decision{Status: "offline", Fresh: true}
	if now.Before(deadline) {
		result.Status = "online"
		result.Connections = 1
		if deadline.Before(next) {
			next = deadline
		}
	}
	if e := tx.Model(&activityRow{}).Where("tenant_id=? AND device_id=?", device.TenantID, device.ID).Update("next_check_at", next).Error; e != nil {
		return Decision{}, e
	}
	return result, nil
}
func recordStateAutomation(tx *gorm.DB, device model.Device, root string, route routeRow, status int16) error {
	login := "OFF-LINE"
	if status == 1 {
		login = "ON-LINE"
	}
	payload, e := json.Marshal(map[string]interface{}{"login": login})
	if e != nil {
		return e
	}
	digest := sha256.Sum256(payload)
	receipt := uuid.NewString()
	intent := uuid.NewSHA1(uuid.NameSpaceOID, []byte(receipt+"/"+device.ID+"/automation")).String()
	if e := tx.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,processed_at) VALUES(?,?,?,'state_transition',?,?,?,'{}',?,'processed',now())`, receipt, device.TenantID, root, route.BrokerCell, route.RouteEpoch, payload, digest[:]).Error; e != nil {
		return e
	}
	data, e := json.Marshal(map[string]interface{}{"trigger_param_type": model.TRIGGER_PARAM_TYPE_STATUS, "trigger_values": map[string]interface{}{"login": login}, "route_epoch": route.RouteEpoch, "broker_cell": route.BrokerCell})
	if e != nil {
		return e
	}
	if e := tx.Exec(`INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload) VALUES(?,?,?,?,'automation',?::jsonb)`, intent, receipt, device.TenantID, device.ID, string(data)).Error; e != nil {
		return e
	}
	return brokerledger.CheckEffectCapacity(tx, device.TenantID)
}

// DeferCurrentProjection retains failed child/root work under its current fence.
func (s *Store) DeferCurrentProjection(ctx context.Context, tenant, device string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, e := lockCurrentRoute(tx, tenant, device); e != nil {
			return e
		}
		return tx.Exec(`UPDATE broker_state_projection_queue SET updated_at=now() WHERE tenant_id=? AND device_id=?`, tenant, device).Error
	})
}

package brokerstate

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"project/internal/model"
)

// Evidence is an as-of read, not a promise that the route cannot change after
// the response. It must never authorize a command without a fresh final check.
type Evidence struct {
	DeviceID          string    `json:"device_id"`
	Status            string    `json:"status"`
	Fresh             bool      `json:"fresh"`
	Connections       int64     `json:"connections"`
	Source            string    `json:"source"`
	AsOf              time.Time `json:"as_of"`
	TransportDeviceID string    `json:"transport_device_id,omitempty"`
	BrokerCell        string    `json:"broker_cell,omitempty"`
	RouteEpoch        uint64    `json:"route_epoch,omitempty"`
	RoutePhase        string    `json:"route_phase,omitempty"`
}

// InspectCurrent reuses the state owner's heartbeat/connection decision in one
// bounded, read-only repeatable snapshot. It does not update legacy is_online,
// schedules, histories, effects, or queues. Disabled, stale and changing routes
// have an explicit unknown result rather than inheriting a cached binary flag.
func (s *Store) InspectCurrent(ctx context.Context, tenant, deviceID string) (Evidence, error) {
	out := Evidence{DeviceID: deviceID, Status: "unknown", Source: "ingress_v1", AsOf: time.Now().UTC()}
	if s == nil || s.db == nil || ctx == nil || tenant == "" || deviceID == "" {
		return out, errors.New("state database or identity unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SET LOCAL statement_timeout = '1500ms'").Error; e != nil {
			return e
		}
		var device model.Device
		if e := tx.Where("tenant_id=? AND id=?", tenant, deviceID).Take(&device).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return ErrDeviceUnavailable
			}
			return e
		}
		if device.IsEnabled != "enabled" || device.ActivateFlag != "active" {
			return nil
		}
		var activity activityRow
		e := tx.Where("tenant_id=? AND device_id=?", tenant, deviceID).Take(&activity).Error
		root := deviceID
		if e == nil {
			root = activity.TransportDeviceID
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var route routeRow
		if e := tx.Raw(`SELECT broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=?`, tenant, root).Scan(&route).Error; e != nil {
			return e
		}
		out.TransportDeviceID, out.BrokerCell, out.RouteEpoch, out.RoutePhase = root, route.BrokerCell, route.RouteEpoch, route.Phase
		if route.Phase != "active" || route.BrokerCell == "" || route.RouteEpoch == 0 {
			return nil
		}
		decision, e := evaluateCurrent(tx, device, root, route, false)
		if errors.Is(e, ErrFenced) {
			return nil
		}
		if e != nil {
			return e
		}
		out.Status, out.Fresh, out.Connections = decision.Status, decision.Fresh, decision.Connections
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, err
}

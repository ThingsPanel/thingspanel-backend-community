// Package brokerstate maintains connection observations for the existing
// community state owner. It never infers offline from an incomplete/stale view.
package brokerstate

import (
	"context"
	"errors"
	"project/internal/model"
	"time"

	"gorm.io/gorm"
	bc "project/internal/brokercontract"
)

var ErrFenced = errors.New("broker state: route is inactive or superseded")

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{db: db} }

type Decision struct {
	Status      string `json:"status"`
	Fresh       bool   `json:"fresh"`
	Connections int64  `json:"connections"`
	Changed     bool   `json:"changed"`
}

type PendingKey struct {
	TenantID string `gorm:"column:tenant_id"`
	DeviceID string `gorm:"column:device_id"`
}

type Projection struct {
	Decision
	Device model.Device
}

// Apply serializes observations with the route owner and conditionally upserts
// only increasing per-connection sequence numbers. A late close of connection A
// cannot close B; an offline tombstone cannot be revived by A's delayed online.
func (s *Store) Apply(ctx context.Context, o bc.StateObservation) (Decision, error) {
	var out Decision
	if s == nil || s.db == nil {
		return out, errors.New("state database unavailable")
	}
	if _, _, e := bc.EncodeState(o); e != nil {
		return out, e
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		route, e := lockRoute(tx, o.Principal.TenantID, o.Principal.DeviceID)
		if e != nil {
			return e
		}
		if route.BrokerCell != o.BrokerCell || route.RouteEpoch != o.RouteEpoch || (route.Phase != "active" && route.Phase != "connecting") {
			return ErrFenced
		}
		result := tx.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,?,?,?,?,?,?,?,now()) ON CONFLICT(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id) DO UPDATE SET route_epoch=excluded.route_epoch,event_seq=excluded.event_seq,online=excluded.online,observed_at=excluded.observed_at,received_at=now() WHERE broker_connection_observations.event_seq < excluded.event_seq`, o.Principal.TenantID, o.Principal.DeviceID, o.BrokerCell, o.BrokerInstance, o.NodeBootID, o.ConnectionID, o.RouteEpoch, o.EventSeq, o.Online, o.ObservedAt)
		if result.Error != nil {
			return result.Error
		}
		accepted := result.RowsAffected > 0
		if accepted {
			if e := tx.Exec(`INSERT INTO broker_state_projection_queue(tenant_id,device_id,updated_at) VALUES(?,?,now()) ON CONFLICT(tenant_id,device_id) DO UPDATE SET updated_at=now()`, o.Principal.TenantID, o.Principal.DeviceID).Error; e != nil {
				return e
			}
		}
		out, e = snapshotTx(tx, o.Principal.TenantID, o.Principal.DeviceID, route.BrokerCell, route.RouteEpoch)
		if e != nil {
			return e
		}
		out.Changed = accepted
		return nil
	})
	return out, err
}

func snapshotTx(tx *gorm.DB, tenant, device, cell string, epoch uint64) (Decision, error) {
	var out Decision
	var counts struct{ Total, Active, Stale, FreshClosed int64 }
	now := time.Now()
	past, future := now.Add(-60*time.Second), now.Add(5*time.Second)
	err := tx.Raw(`SELECT count(*) AS total,
	 count(*) FILTER(WHERE online AND observed_at>=? AND observed_at<=? AND received_at>=?) AS active,
	 count(*) FILTER(WHERE online AND NOT(observed_at>=? AND observed_at<=? AND received_at>=?)) AS stale,
	 count(*) FILTER(WHERE NOT online AND observed_at>=? AND observed_at<=? AND received_at>=?) AS fresh_closed
	 FROM broker_connection_observations WHERE tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=?`, past, future, past, past, future, past, past, future, past, tenant, device, cell, epoch).Scan(&counts).Error
	if err != nil {
		return out, err
	}
	out.Connections = counts.Active
	out.Status = "unknown"
	if counts.Active > 0 {
		out.Status, out.Fresh = "online", true
	} else if counts.Stale > 0 {
		out.Fresh = false
	} else if counts.FreshClosed > 0 {
		out.Status, out.Fresh = "offline", true
	}
	return out, nil
}

// Project re-evaluates the current connection snapshot under the route lock and
// atomically projects a determinate change into the legacy device/status tables.
func (s *Store) Project(ctx context.Context, tenant, deviceID string) (Projection, error) {
	return s.project(ctx, tenant, deviceID, false)
}
func (s *Store) project(ctx context.Context, tenant, deviceID string, activity bool) (Projection, error) {
	var out Projection
	if s == nil || s.db == nil || tenant == "" || deviceID == "" {
		return out, errors.New("state database or identity unavailable")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if activity {
			if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", tenant).Error; e != nil {
				return e
			}
		}
		var route routeRow
		var err error
		root := deviceID
		if activity {
			route, root, err = lockCurrentRoute(tx, tenant, deviceID)
		} else {
			route, err = lockRoute(tx, tenant, deviceID)
		}
		if err != nil {
			return err
		}
		if route.Phase != "active" {
			return ErrFenced
		}
		var device model.Device
		r := tx.Raw(`SELECT * FROM devices WHERE tenant_id=? AND id=? AND is_enabled='enabled' AND activate_flag='active' FOR UPDATE`, tenant, deviceID).Scan(&device)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrDeviceUnavailable
		}
		var decision Decision
		if activity {
			decision, err = currentDecision(tx, device, root, route)
		} else {
			decision, err = snapshotTx(tx, tenant, deviceID, route.BrokerCell, route.RouteEpoch)
		}
		if err != nil {
			return err
		}
		out.Decision = decision
		out.Device = device
		if decision.Status == "unknown" {
			return tx.Exec(`DELETE FROM broker_state_projection_queue WHERE tenant_id=? AND device_id=?`, tenant, deviceID).Error
		}
		status := int16(0)
		if decision.Status == "online" {
			status = 1
		}
		if device.IsOnline == status {
			return tx.Exec(`DELETE FROM broker_state_projection_queue WHERE tenant_id=? AND device_id=?`, tenant, deviceID).Error
		}
		values := map[string]any{"is_online": status}
		if status == 0 {
			values["last_offline_time"] = time.Now().UTC()
		}
		updated := tx.Model(&model.Device{}).Where("tenant_id = ? AND id = ? AND is_enabled = 'enabled' AND activate_flag = 'active'", tenant, deviceID).Updates(values)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrDeviceUnavailable
		}
		if err := tx.Create(&model.DeviceStatusHistory{TenantID: tenant, DeviceID: deviceID, Status: status, ChangeTime: time.Now().UTC()}).Error; err != nil {
			return err
		}
		if activity {
			if err := recordStateAutomation(tx, device, root, route, status); err != nil {
				return err
			}
		}
		out.Device.IsOnline = status
		out.Changed = true
		return tx.Exec(`DELETE FROM broker_state_projection_queue WHERE tenant_id=? AND device_id=?`, tenant, deviceID).Error
	})
	return out, err
}

var ErrDeviceUnavailable = errors.New("broker state: tenant-scoped active device unavailable")

type routeRow struct {
	BrokerCell string
	RouteEpoch uint64
	Phase      string
}

func lockRoute(tx *gorm.DB, tenant, device string) (routeRow, error) {
	var route routeRow
	r := tx.Raw(`SELECT broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=? FOR UPDATE`, tenant, device).Scan(&route)
	if r.Error != nil {
		return route, r.Error
	}
	if r.RowsAffected != 1 || route.BrokerCell == "" || route.RouteEpoch == 0 {
		return route, ErrFenced
	}
	return route, nil
}

// SkipProjection removes a dirty projection when heartbeat mode owns status.
// It takes the same route lock as Apply and Project so it cannot erase a newer
// observation's queue entry.
func (s *Store) SkipProjection(ctx context.Context, tenant, device string) error {
	if s == nil || s.db == nil {
		return errors.New("state database unavailable")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockRoute(tx, tenant, device); err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM broker_state_projection_queue WHERE tenant_id=? AND device_id=?`, tenant, device).Error
	})
}

// DeferProjection keeps a failed key durable while moving it behind newer work
// so a repeatedly failing device cannot monopolize every bounded page.
func (s *Store) DeferProjection(ctx context.Context, tenant, device string) error {
	if s == nil || s.db == nil {
		return errors.New("state database unavailable")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockRoute(tx, tenant, device); err != nil {
			return err
		}
		return tx.Exec(`UPDATE broker_state_projection_queue SET updated_at=now() WHERE tenant_id=? AND device_id=?`, tenant, device).Error
	})
}

// Pending returns a deterministic bounded page; the table has at most one row
// per tenant/device and Project re-locks/rechecks each row before writing.
func (s *Store) Pending(ctx context.Context, limit int) ([]PendingKey, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("state database unavailable")
	}
	if limit <= 0 || limit > 256 {
		limit = 256
	}
	var rows []PendingKey
	err := s.db.WithContext(ctx).Raw(`SELECT tenant_id,device_id FROM broker_state_projection_queue ORDER BY updated_at,tenant_id,device_id LIMIT ?`, limit).Scan(&rows).Error
	return rows, err
}

// Device returns only an active device from the requested tenant for resolving
// the existing heartbeat owner before a broker projection is applied.
func (s *Store) Device(ctx context.Context, tenant, deviceID string) (*model.Device, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("state database unavailable")
	}
	var device model.Device
	r := s.db.WithContext(ctx).Raw(`SELECT * FROM devices WHERE tenant_id=? AND id=? AND is_enabled='enabled' AND activate_flag='active'`, tenant, deviceID).Scan(&device)
	if r.Error != nil {
		return nil, r.Error
	}
	if r.RowsAffected != 1 {
		return nil, ErrDeviceUnavailable
	}
	return &device, nil
}

// Snapshot is tenant/device scoped. Stale open observations become unknown;
// absent observations never prove offline or current broker health.
func (s *Store) Snapshot(ctx context.Context, tenant, device string) (Decision, error) {
	out := Decision{Status: "unknown"}
	if s == nil || s.db == nil {
		return out, errors.New("state database unavailable")
	}
	var route routeRow
	err := s.db.WithContext(ctx).Raw(`SELECT broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=? AND phase='active'`, tenant, device).Scan(&route).Error
	if err != nil {
		return out, err
	}
	if route.BrokerCell == "" || route.RouteEpoch == 0 {
		return out, nil
	}
	return snapshotTx(s.db.WithContext(ctx), tenant, device, route.BrokerCell, route.RouteEpoch)
}

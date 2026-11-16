package brokerstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"project/internal/model"
	"time"
)

type Summary struct {
	DeviceID        string     `json:"deviceId"`
	Connectivity    string     `json:"connectivity"`
	ObservedAt      time.Time  `json:"observedAt"`
	LastTelemetryAt *time.Time `json:"lastTelemetryAt"`
	Available       bool       `json:"available"`
}

// InspectBatch is a tenant-scoped, read-only snapshot. Presence and telemetry
// reception are separate facts; neither is inferred from the legacy online bit.
func (s *Store) InspectBatch(ctx context.Context, tenant string, ids []string) ([]Summary, error) {
	if s == nil || s.db == nil || tenant == "" || len(ids) == 0 || len(ids) > 200 {
		return nil, errors.New("invalid evidence batch")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 36 || seen[id] {
			return nil, errors.New("invalid evidence batch")
		}
		seen[id] = true
	}
	out := make([]Summary, 0, len(ids))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='4s'").Error; err != nil {
			return err
		}
		var devices []model.Device
		if err := tx.Where("tenant_id=? AND id IN ?", tenant, ids).Find(&devices).Error; err != nil {
			return err
		}
		if len(devices) != len(ids) {
			return ErrDeviceUnavailable
		}
		deviceByID := map[string]model.Device{}
		configIDs := []string{}
		for _, d := range devices {
			deviceByID[d.ID] = d
			if d.DeviceConfigID != nil && *d.DeviceConfigID != "" {
				configIDs = append(configIDs, *d.DeviceConfigID)
			}
		}
		configs := map[string]model.DeviceConfig{}
		if len(configIDs) > 0 {
			var rows []model.DeviceConfig
			if err := tx.Where("tenant_id=? AND id IN ?", tenant, configIDs).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				configs[row.ID] = row
			}
		}
		type batchRoute struct {
			DeviceID   string
			BrokerCell string
			RouteEpoch uint64
			Phase      string
		}
		var routes []batchRoute
		if err := tx.Raw(`SELECT device_id,broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id IN ?`, tenant, ids).Scan(&routes).Error; err != nil {
			return err
		}
		routeByID := map[string]batchRoute{}
		for _, r := range routes {
			routeByID[r.DeviceID] = r
		}
		type connectionCount struct {
			DeviceID                              string
			BrokerCell                            string
			RouteEpoch                            uint64
			Active, Stale, FreshClosed, Delegated int64
		}
		var counts []connectionCount
		past, future := now.Add(-60*time.Second), now.Add(5*time.Second)
		if err := tx.Raw(`SELECT device_id,broker_cell,route_epoch,
          count(*) FILTER(WHERE online AND observed_at>=? AND observed_at<=? AND received_at>=?) active,
          count(*) FILTER(WHERE online AND NOT(observed_at>=? AND observed_at<=? AND received_at>=?)) stale,
          count(*) FILTER(WHERE NOT online AND observed_at>=? AND observed_at<=? AND received_at>=?) fresh_closed,
          count(*) FILTER(WHERE NULLIF(to_jsonb(o)->'delegated_route','null'::jsonb) IS NOT NULL) delegated
          FROM broker_connection_observations o WHERE tenant_id=? AND device_id IN ? GROUP BY device_id,broker_cell,route_epoch`, past, future, past, past, future, past, past, future, past, tenant, ids).Scan(&counts).Error; err != nil {
			return err
		}
		countByID := map[string]connectionCount{}
		for _, c := range counts {
			r := routeByID[c.DeviceID]
			if r.BrokerCell == c.BrokerCell && r.RouteEpoch == c.RouteEpoch {
				countByID[c.DeviceID] = c
			}
		}
		var activity []activityRow
		if err := tx.Where("tenant_id=? AND device_id IN ?", tenant, ids).Find(&activity).Error; err != nil {
			return err
		}
		last := map[string]time.Time{}
		activityByID := map[string]activityRow{}
		for _, a := range activity {
			last[a.DeviceID] = a.ObservedAt
			activityByID[a.DeviceID] = a
		}
		for _, id := range ids {
			e := Evidence{DeviceID: id, Status: "unknown", Source: "ingress_v1", AsOf: now}
			d := deviceByID[id]
			a, hasActivity := activityByID[id]
			r := routeByID[id]
			c := countByID[id]
			complex := (hasActivity && a.TransportDeviceID != id) || (hasActivity && len(a.DelegatedRoute) > 0 && string(a.DelegatedRoute) != "null") || c.Delegated > 0
			if complex {
				if err := inspectCurrentTx(tx, tenant, id, &e); err != nil {
					return err
				}
			} else if d.IsEnabled == "enabled" && d.ActivateFlag == "active" && r.Phase == "active" && r.BrokerCell != "" && r.RouteEpoch > 0 {
				var cfg struct {
					Heartbeat int `json:"heartbeat"`
					Timeout   int `json:"online_timeout"`
				}
				if d.DeviceConfigID != nil && *d.DeviceConfigID != "" {
					config, ok := configs[*d.DeviceConfigID]
					if !ok {
						return errors.New("state configuration unavailable")
					}
					if config.OtherConfig != nil && *config.OtherConfig != "" {
						if err := json.Unmarshal([]byte(*config.OtherConfig), &cfg); err != nil {
							return err
						}
					}
				}
				if cfg.Heartbeat < 0 || cfg.Timeout < 0 || cfg.Heartbeat > 31536000 || cfg.Timeout > 31536000 {
					return errors.New("state heartbeat duration outside supported range")
				}
				if cfg.Heartbeat == 0 && cfg.Timeout == 0 {
					if c.Active > 0 {
						e.Status = "online"
					} else if c.Stale == 0 && c.FreshClosed > 0 {
						e.Status = "offline"
					}
				} else if hasActivity && a.BrokerCell == r.BrokerCell && a.RouteEpoch == r.RouteEpoch && !a.ObservedAt.After(now.Add(5*time.Second)) {
					interval := cfg.Heartbeat
					if interval == 0 {
						interval = cfg.Timeout
					}
					e.Status = "offline"
					if now.Before(a.ObservedAt.Add(time.Duration(interval) * time.Second)) {
						e.Status = "online"
					}
				}
			}

			row := Summary{DeviceID: id, Connectivity: e.Status, ObservedAt: now, Available: true}
			if t, ok := last[id]; ok && !t.IsZero() {
				row.LastTelemetryAt = &t
			}
			out = append(out, row)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, err
}

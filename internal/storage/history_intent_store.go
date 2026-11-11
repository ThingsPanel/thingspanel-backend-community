package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// intentHistoryStore records history as durable intents instead of writing the
// history table in the caller's transaction. It keeps the legacy first-write-wins
// rule for one (device, key, ts) identity: only points whose intent was newly
// stored can move the current snapshot, and the snapshot is verified before the
// caller commits. The backend is written later by ReplayHistoryIntents.
type intentHistoryStore struct{ generation int64 }

// NewIntentHistoryStore returns a TelemetryHistoryStore that enqueues intents for the
// given generation. A replayer with a HistorySink must be running or intents pile up.
func NewIntentHistoryStore(generation int64) TelemetryHistoryStore {
	return intentHistoryStore{generation: generation}
}

type intentIdentity struct {
	device, key string
	ts          int64
}

func (s intentHistoryStore) AppendInTx(tx *gorm.DB, points []TelemetryData) (int, error) {
	points = append([]TelemetryData(nil), points...)
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].DeviceID != points[j].DeviceID {
			return points[i].DeviceID < points[j].DeviceID
		}
		if points[i].Key != points[j].Key {
			return points[i].Key < points[j].Key
		}
		return points[i].TS < points[j].TS
	})
	stored := make(map[intentIdentity]TelemetryData, len(points))
	if flavorOf(tx).mysql {
		created, err := insertIntentsMySQL(tx, s.generation, points)
		if err != nil {
			return 0, err
		}
		first := make(map[intentIdentity]TelemetryData, len(points))
		for _, p := range points {
			id := intentIdentity{p.DeviceID, p.Key, p.TS}
			if _, seen := first[id]; !seen {
				first[id] = p
			}
		}
		for id := range created {
			stored[id] = first[id]
		}
	} else {
		const chunk = 500
		for offset := 0; offset < len(points); offset += chunk {
			end := offset + chunk
			if end > len(points) {
				end = len(points)
			}
			args := make([]interface{}, 0, (end-offset)*8)
			values := make([]string, 0, end-offset)
			for _, p := range points[offset:end] {
				if p.TenantID == "" {
					return 0, errors.New("history intent requires a tenant")
				}
				values = append(values, "(?,?,?,?,?,?,?,?)")
				args = append(args, p.TenantID, p.DeviceID, p.Key, p.TS, p.BoolV, p.NumberV, p.StringV, s.generation)
			}
			var inserted []struct {
				DeviceID string `gorm:"column:device_id"`
				Key      string `gorm:"column:key"`
				TS       int64  `gorm:"column:ts"`
			}
			if err := tx.Raw(`INSERT INTO telemetry_history_intents (tenant_id, device_id, "key", ts, bool_v, number_v, string_v, generation)
				VALUES `+strings.Join(values, ",")+`
				ON CONFLICT (device_id, "key", ts, generation) DO NOTHING
				RETURNING device_id, "key", ts`, args...).Scan(&inserted).Error; err != nil {
				return 0, err
			}
			byID := make(map[intentIdentity]TelemetryData, end-offset)
			for _, p := range points[offset:end] {
				if _, seen := byID[intentIdentity{p.DeviceID, p.Key, p.TS}]; !seen {
					byID[intentIdentity{p.DeviceID, p.Key, p.TS}] = p
				}
			}
			for _, row := range inserted {
				id := intentIdentity{row.DeviceID, row.Key, row.TS}
				stored[id] = byID[id]
			}
		}
	}
	if len(stored) == 0 {
		return 0, nil
	}
	type currentIdentity struct{ device, key string }
	winners := make(map[currentIdentity]TelemetryData)
	for _, p := range stored {
		key := currentIdentity{p.DeviceID, p.Key}
		if prior, ok := winners[key]; !ok || p.TS > prior.TS {
			winners[key] = p
		}
	}
	rows := make([]TelemetryCurrentData, 0, len(winners))
	for _, p := range winners {
		rows = append(rows, TelemetryCurrentData{DeviceID: p.DeviceID, Key: p.Key, TS: time.UnixMilli(p.TS), BoolV: p.BoolV, NumberV: p.NumberV, StringV: p.StringV, TenantID: p.TenantID})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DeviceID != rows[j].DeviceID {
			return rows[i].DeviceID < rows[j].DeviceID
		}
		return rows[i].Key < rows[j].Key
	})
	if flavorOf(tx).mysql {
		if err := upsertCurrentMySQL(tx, rows); err != nil {
			return 0, err
		}
	} else if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"ts", "bool_v", "number_v", "string_v", "tenant_id"}), Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "telemetry_current_datas.ts <= excluded.ts"}}}}).CreateInBatches(&rows, 1000).Error; err != nil {
		return 0, err
	}
	if err := verifyTelemetryCurrentWinners(tx, rows); err != nil {
		return 0, err
	}
	return len(stored), nil
}

// postgresHistorySink writes replayed intents into the existing history table.
// Identity conflicts are ignored, which makes a repeated Append a no-op.
type postgresHistorySink struct{ db *gorm.DB }

// NewPostgresHistorySink returns the PostgreSQL history backend for intent replay.
func NewPostgresHistorySink(db *gorm.DB) HistorySink { return postgresHistorySink{db: db} }

func (s postgresHistorySink) Append(ctx context.Context, intents []HistoryIntent) error {
	points := make([]TelemetryData, len(intents))
	for i, in := range intents {
		points[i] = TelemetryData{DeviceID: in.DeviceID, Key: in.Key, TS: in.TS, BoolV: in.BoolV, NumberV: in.NumberV, StringV: in.StringV, TenantID: in.TenantID}
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}, {Name: "key"}, {Name: "ts"}}, DoNothing: true}).CreateInBatches(&points, 1000).Error
	if err != nil {
		return fmt.Errorf("postgres history sink: %w", err)
	}
	return nil
}

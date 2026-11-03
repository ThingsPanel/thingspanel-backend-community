package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HistoryIntent is one durable "write this history point" record. The identity
// (device_id, key, ts, generation) makes replay safe: a backend that has already
// stored the point must treat a second Append of it as a no-op.
type HistoryIntent struct {
	ID         int64    `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID   string   `gorm:"column:tenant_id"`
	DeviceID   string   `gorm:"column:device_id"`
	Key        string   `gorm:"column:key"`
	TS         int64    `gorm:"column:ts"`
	BoolV      *bool    `gorm:"column:bool_v"`
	NumberV    *float64 `gorm:"column:number_v"`
	StringV    *string  `gorm:"column:string_v"`
	Generation int64    `gorm:"column:generation"`
	Attempts   int      `gorm:"column:attempts"`
}

func (HistoryIntent) TableName() string { return "telemetry_history_intents" }

// HistorySink is the external time-series backend. Append must be idempotent for
// the intent identity and must return an error whenever the outcome is unknown.
type HistorySink interface {
	Append(ctx context.Context, intents []HistoryIntent) error
}

// EnqueueHistoryIntents records intents inside the caller's transaction, so they
// commit or roll back together with the snapshot and receipt. Re-enqueueing the
// same identity is a no-op; it returns how many new intents were stored.
func EnqueueHistoryIntents(tx *gorm.DB, generation int64, points []TelemetryData) (int, error) {
	if len(points) == 0 {
		return 0, nil
	}
	rows := make([]HistoryIntent, 0, len(points))
	for _, p := range points {
		if p.TenantID == "" {
			return 0, errors.New("history intent requires a tenant")
		}
		rows = append(rows, HistoryIntent{TenantID: p.TenantID, DeviceID: p.DeviceID, Key: p.Key, TS: p.TS,
			BoolV: p.BoolV, NumberV: p.NumberV, StringV: p.StringV, Generation: generation})
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 1000)
	return int(result.RowsAffected), result.Error
}

// ReplayHistoryIntents claims up to limit pending intents for owner under a lease,
// appends them to the sink, and marks them applied only after the sink succeeds.
// A sink error (including an unknown outcome) leaves them pending; once the lease
// expires any replayer may claim them again. It returns the number applied.
func ReplayHistoryIntents(ctx context.Context, db *gorm.DB, sink HistorySink, owner string, limit int, lease time.Duration) (int, error) {
	if limit <= 0 || lease <= 0 || owner == "" {
		return 0, errors.New("replay needs an owner, a positive limit and a positive lease")
	}
	var claimed []HistoryIntent
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT id, tenant_id, device_id, "key", ts, bool_v, number_v, string_v, generation, attempts
			FROM telemetry_history_intents
			WHERE state = 'pending' AND (claimed_until IS NULL OR claimed_until < NOW())
			ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED`, limit).Scan(&claimed).Error; err != nil {
			return err
		}
		if len(claimed) == 0 {
			return nil
		}
		ids := make([]int64, len(claimed))
		for i, c := range claimed {
			ids[i] = c.ID
		}
		return tx.Exec(`UPDATE telemetry_history_intents
			SET claimed_by = ?, claimed_until = NOW() + make_interval(secs => ?), attempts = attempts + 1
			WHERE id IN ?`, owner, lease.Seconds(), ids).Error
	})
	if err != nil || len(claimed) == 0 {
		return 0, err
	}
	if err := sink.Append(ctx, claimed); err != nil {
		return 0, fmt.Errorf("history sink append (intents stay pending): %w", err)
	}
	ids := make([]int64, len(claimed))
	for i, c := range claimed {
		ids[i] = c.ID
	}
	res := db.WithContext(ctx).Exec(`UPDATE telemetry_history_intents
		SET state = 'applied', applied_at = NOW(), claimed_by = NULL, claimed_until = NULL
		WHERE id IN ? AND state = 'pending' AND claimed_by = ?`, ids, owner)
	if res.Error != nil {
		return 0, fmt.Errorf("history intents appended but not acknowledged (replay is safe): %w", res.Error)
	}
	return int(res.RowsAffected), nil
}

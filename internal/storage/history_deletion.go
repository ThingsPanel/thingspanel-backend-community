package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// HistoryDeleter is implemented by backends that can remove one device's history.
// DeleteDevice must be idempotent and must succeed when the device has no history.
type HistoryDeleter interface {
	DeleteDevice(ctx context.Context, deviceID string) error
}

// EnqueueHistoryDeviceDeletion records, inside the caller's transaction (the one that
// deletes the device), that the device's history must be removed from the time-series
// backend. History intents for the device that nobody has claimed are cancelled in the same
// transaction so they cannot be delivered after the deletion.
func EnqueueHistoryDeviceDeletion(tx *gorm.DB, deviceID string) error {
	if deviceID == "" {
		return errors.New("history deletion requires a device id")
	}
	if err := tx.Exec(`DELETE FROM telemetry_history_intents
		WHERE device_id = ? AND state = 'pending' AND (claimed_until IS NULL OR claimed_until < `+flavorOf(tx).now()+`)`, deviceID).Error; err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO telemetry_history_deletions (device_id) VALUES (?)`, deviceID).Error
}

// ReplayHistoryDeletions carries out pending deletions. A deletion waits while a
// replayer still holds a live claim on that device's intents, because those rows could
// otherwise be written after the deletion; it is retried on the next call. It returns the
// number of deletions completed.
//
// Limitation: a replayer that stays inside one backend write for longer than its lease can
// still deliver rows after the lease has expired and the deletion has run.
func ReplayHistoryDeletions(ctx context.Context, db *gorm.DB, deleter HistoryDeleter, owner string, limit int, lease time.Duration) (int, error) {
	if limit <= 0 || lease <= 0 || owner == "" {
		return 0, errors.New("replay needs an owner, a positive limit and a positive lease")
	}
	type deletion struct {
		ID       int64  `gorm:"column:id"`
		DeviceID string `gorm:"column:device_id"`
	}
	var claimed []deletion
	flavor := flavorOf(db)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT d.id, d.device_id FROM telemetry_history_deletions d
			WHERE d.state = 'pending' AND (d.claimed_until IS NULL OR d.claimed_until < `+flavor.now()+`)
			AND NOT EXISTS (SELECT 1 FROM telemetry_history_intents i
				WHERE i.device_id = d.device_id AND i.state = 'pending' AND i.claimed_until IS NOT NULL AND i.claimed_until >= `+flavor.now()+`)
			ORDER BY d.id LIMIT ? `+flavor.forUpdate("d"), limit).Scan(&claimed).Error; err != nil {
			return err
		}
		for _, d := range claimed {
			leaseSQL, leaseArg := flavor.leaseExpr(lease.Seconds())
			if err := tx.Exec(`UPDATE telemetry_history_deletions
				SET claimed_by = ?, claimed_until = `+leaseSQL+`, attempts = attempts + 1 WHERE id = ?`,
				owner, leaseArg, d.ID).Error; err != nil {
				return err
			}
			// Anything still queued for this device is superseded by the deletion.
			if err := tx.Exec(`DELETE FROM telemetry_history_intents WHERE device_id = ? AND state = 'pending'`, d.DeviceID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, d := range claimed {
		if err := deleter.DeleteDevice(ctx, d.DeviceID); err != nil {
			return done, fmt.Errorf("history deletion (stays pending): %w", err)
		}
		res := db.WithContext(ctx).Exec(`UPDATE telemetry_history_deletions
			SET state = 'applied', applied_at = `+flavor.now()+`, claimed_by = NULL, claimed_until = NULL
			WHERE id = ? AND state = 'pending' AND claimed_by = ?`, d.ID, owner)
		if res.Error != nil {
			return done, fmt.Errorf("history deleted but not acknowledged (replay is safe): %w", res.Error)
		}
		done += int(res.RowsAffected)
	}
	return done, nil
}

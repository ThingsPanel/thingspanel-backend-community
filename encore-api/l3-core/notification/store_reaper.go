package notification

import (
	"context"
	"time"
)

// ExpireQueued archives queued deliveries whose send window elapsed. The
// delivery wire enums have no separate expired value, so the terminal outcome
// is failed with the stable, non-provider error code "expired".
func (s *Store) ExpireQueued(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() || limit < 1 || limit > 1000 {
		return 0, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id,id FROM notification_deliveries WHERE dispatch_status='queued' AND expires_at<=$1 ORDER BY expires_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, classifyStoreError(err)
	}
	type key struct{ tenantID, deliveryID string }
	var expired []key
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.tenantID, &item.deliveryID); err != nil {
			rows.Close()
			return 0, classifyStoreError(err)
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, classifyStoreError(err)
	}
	rows.Close()
	for _, item := range expired {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET dispatch_status='failed',delivery_status='failed',error_code='expired',updated_at=$3 WHERE tenant_id=$1 AND id=$2 AND dispatch_status='queued'`, item.tenantID, item.deliveryID, now.UTC()); err != nil {
			return 0, classifyStoreError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, classifyStoreError(err)
	}
	return len(expired), nil
}

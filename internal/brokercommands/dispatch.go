package brokercommands

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Dispatch serializes the final route/caller check and bounded broker publish
// with route changes. A transport error is ambiguous and never requeued. A DB
// commit failure leaves dispatching for the stale sweep to mark unknown.
// All paths lock binding before command, matching reliable ACK processing.
func (s *Store) Dispatch(ctx context.Context, id string, guard func(context.Context, *gorm.DB, Row) error, publish func(context.Context, Row) error) error {
	if s == nil || s.db == nil || ctx == nil || guard == nil || publish == nil {
		return ErrInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate Row
		if err := tx.Where("id=?", id).Take(&candidate).Error; err != nil {
			return err
		}
		binding, bindingErr := activeBinding(tx, candidate.TenantID, candidate.TargetDeviceID)
		if bindingErr != nil && !errors.Is(bindingErr, ErrFenced) {
			return bindingErr
		}
		var row Row
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.State != StateDispatching {
			return ErrState
		}
		finish := func(state, reason string) error {
			return tx.Model(&Row{}).Where("id=? AND state=?", id, StateDispatching).Updates(map[string]any{"state": state, "last_error": reason, "completed_at": time.Now().UTC(), "updated_at": time.Now().UTC()}).Error
		}
		if !row.ExpiresAt.After(time.Now()) {
			return finish(StateExpired, "expired before dispatch")
		}
		if bindingErr != nil || binding.BrokerCell != row.RouteCell || binding.RouteEpoch != row.RouteEpoch {
			return finish(StateFenced, "route changed before dispatch")
		}
		if err := checkDevicePair(tx, row.TenantID, row.DeviceID, row.TargetDeviceID); err != nil {
			if errors.Is(err, ErrRejected) {
				return finish(StateFenced, "device ancestry changed before dispatch")
			}
			return err
		}
		if err := guard(ctx, tx, row); err != nil {
			if errors.Is(err, ErrFenced) {
				return finish(StateFenced, "command configuration changed before dispatch")
			}
			if errors.Is(err, ErrRejected) {
				return finish(StateFailed, "caller or command configuration no longer authorized")
			}
			return err
		}
		publishCtx, cancel := context.WithDeadline(ctx, minDeadline(row.ExpiresAt, time.Now().Add(5*time.Second)))
		defer cancel()
		state, reason := StateBrokerAccepted, ""
		if err := publish(publishCtx, row); err != nil {
			state, reason = StateUnknown, "broker publication outcome unknown"
		}
		return tx.Model(&Row{}).Where("id=? AND state=?", id, StateDispatching).Updates(map[string]any{"state": state, "last_error": reason, "updated_at": time.Now().UTC()}).Error
	})
}
func minDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

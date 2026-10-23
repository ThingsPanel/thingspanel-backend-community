package notification

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

type StoreErrorKind string

const (
	StoreErrorInvalid          StoreErrorKind = "invalid"
	StoreErrorNotFound         StoreErrorKind = "not_found"
	StoreErrorConflict         StoreErrorKind = "conflict"
	StoreErrorVersionConflict  StoreErrorKind = "version_conflict"
	StoreErrorUnavailable      StoreErrorKind = "unavailable"
	StoreErrorStaleClaim       StoreErrorKind = "stale_claim"
	StoreErrorProtectorMissing StoreErrorKind = "protector_missing"
)

// StoreError exposes a stable category without forwarding driver text, SQL,
// user input, provider messages, ciphertext, or credentials.
type StoreError struct {
	Kind StoreErrorKind
}

func (e *StoreError) Error() string {
	if e == nil {
		return "notification storage error"
	}
	switch e.Kind {
	case StoreErrorInvalid:
		return "notification request is invalid"
	case StoreErrorNotFound:
		return "notification resource was not found"
	case StoreErrorConflict:
		return "notification resource conflicts with existing state"
	case StoreErrorVersionConflict:
		return "notification resource version conflicts"
	case StoreErrorStaleClaim:
		return "notification delivery claim is stale"
	case StoreErrorProtectorMissing:
		return "notification snapshot protection is unavailable"
	default:
		return "notification storage is unavailable"
	}
}

func storeErr(kind StoreErrorKind) error { return &StoreError{Kind: kind} }

func classifyStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return storeErr(StoreErrorNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return storeErr(StoreErrorConflict)
		case "23503", "23514", "22P02", "22007":
			return storeErr(StoreErrorInvalid)
		case "40001", "40P01":
			// Serialization failures and deadlocks are transient storage failures,
			// not a permanently conflicting idempotency key. Durable intake can
			// safely retry the same intent after the transaction rolls back.
			return storeErr(StoreErrorUnavailable)
		}
	}
	return storeErr(StoreErrorUnavailable)
}

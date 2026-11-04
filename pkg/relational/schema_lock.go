package relational

// This file is the single home of the MySQL schema-lock contract. It moved here from encore-api/pkg/ownermigration
// (which now only forwards to these definitions until T1-D1 deletes it); the lock name derivation and the
// dedicated-connection behaviour are unchanged.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrBusy           = errors.New("schema migration executor already holds the database lock")
	ErrOutcomeUnknown = errors.New("migration outcome is unknown; reconcile the existing owner ledger")
)

// SchemaLockName is the shared cross-owner lock contract. The caller must
// pass the exact UTF-8 database name returned by SELECT DATABASE(), never a DSN
// alias, supplied schema label, owner name, or lowercased approximation.
func SchemaLockName(database string) string {
	digest := sha256.Sum256([]byte(database))
	return "thingspanel:schema:" + hex.EncodeToString(digest[:])[:40]
}

// WithSchemaLock uses one dedicated physical connection from acquisition
// through schema verification and owner-ledger acknowledgement. A lost
// connection cannot transparently switch the migration to an unlocked session.
// All MySQL owners in this database must use this exact lock contract. This
// function does not merge ledgers or make MySQL DDL transactional.
func WithSchemaLock(ctx context.Context, db *sql.DB, apply func(*sql.Conn, string) error) (result error) {
	if db == nil || apply == nil {
		return fmt.Errorf("invalid MySQL migration executor")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var database string
	if err := conn.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&database); err != nil || database == "" {
		return fmt.Errorf("migration database must be selected and observable")
	}
	name := SchemaLockName(database)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?,0)`, name).Scan(&acquired); err != nil {
		// GET_LOCK can succeed before its result is lost. No release defer is
		// registered yet, so discard this physical session on acquisition doubt.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return errors.Join(err, fmt.Errorf("%w: schema lock acquisition was not acknowledged", ErrOutcomeUnknown))
	}
	if !acquired.Valid || acquired.Int64 != 0 && acquired.Int64 != 1 {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return fmt.Errorf("%w: schema lock acquisition returned an invalid result", ErrOutcomeUnknown)
	}
	if acquired.Int64 == 0 {
		return ErrBusy
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, `SELECT RELEASE_LOCK(?)`, name).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			// sql.Conn.Close alone returns a healthy session to the pool. If the
			// lock release is uncertain, discard the physical session instead.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			result = errors.Join(result, fmt.Errorf("%w: schema lock release was not acknowledged", ErrOutcomeUnknown))
		}
	}()
	return apply(conn, database)
}

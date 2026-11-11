// Package ownermigration forwards to pkg/relational, where the MySQL schema-lock contract now lives (T1-A0). It is
// kept only so the existing callers compile; T1-D1 deletes it.
package ownermigration

import (
	"context"
	"database/sql"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

var (
	ErrBusy           = relational.ErrBusy
	ErrOutcomeUnknown = relational.ErrOutcomeUnknown
)

// MySQLSchemaLockName forwards to relational.SchemaLockName.
func MySQLSchemaLockName(database string) string { return relational.SchemaLockName(database) }

// WithMySQLSchemaLock forwards to relational.WithSchemaLock.
func WithMySQLSchemaLock(ctx context.Context, db *sql.DB, apply func(*sql.Conn, string) error) error {
	return relational.WithSchemaLock(ctx, db, apply)
}

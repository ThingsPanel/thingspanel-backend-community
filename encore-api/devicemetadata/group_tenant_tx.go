package devicemetadata

import (
	"context"
	"database/sql"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

// beginTenantGroupTx starts a transaction whose lifetime is the lifetime of the
// trusted tenant setting. The tenant is validated before opening the connection.
func beginTenantGroupTx(ctx context.Context, tenantID string, options *sql.TxOptions) (*sql.Tx, error) {
	parsed, err := uuid.Parse(tenantID)
	if err != nil || parsed == uuid.Nil || parsed.String() != tenantID {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	tx, err := db.Stdlib().BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	var configured string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if configured != tenantID {
		_ = tx.Rollback()
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to set authenticated tenant context"}
	}
	return tx, nil
}

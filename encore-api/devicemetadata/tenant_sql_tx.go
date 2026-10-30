package devicemetadata

import (
	"context"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// beginTenantScopedSQLTx starts a tenant-scoped Encore SQL transaction. It
// accepts only the canonical non-nil UUID returned by mustTenantID and binds
// the setting to the transaction, never the pooled session. Read-only callers
// receive a repeatable-read snapshot for multi-query responses.
func beginTenantScopedSQLTx(ctx context.Context, tenantID string, readOnly bool) (*sqldb.Tx, error) {
	parsed, err := uuid.Parse(tenantID)
	if err != nil || parsed == uuid.Nil || parsed.String() != tenantID {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if readOnly {
		if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if configured != tenantID {
		_ = tx.Rollback()
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to set authenticated tenant context"}
	}
	return tx, nil
}

func wrapCustomControlTxError(err error, message string) error {
	if errs.Code(err) == errs.Unauthenticated {
		return err
	}
	return errs.WrapCode(err, errs.Internal, message)
}

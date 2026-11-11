package store

import (
	"context"
	"database/sql"
	"errors"

	"encore.app/marketinstall/app"
)

func requireTrue(row scanner) error {
	var matched sql.NullBool
	if err := row.Scan(&matched); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.ErrLeaseLost
		}
		return err
	}
	if !matched.Valid || !matched.Bool {
		return app.ErrLeaseLost
	}
	return nil
}

// Row counts prove a predicate matched; a database trigger can still change
// NEW values. Verify the actual local target before acknowledging the commit.
// These reads stay inside the same operation-row lock, not a racing post-commit
// lookup. A failed target rolls back all intermediate step/receipt writes.
func (s *Store) verifyOperationTarget(ctx context.Context, tx *sql.Tx, op app.Operation, state, code string, leased bool) error {
	lease := `lease_until IS NULL`
	if leased {
		clock := "clock_timestamp()"
		if s.dialect == MySQL {
			clock = "UTC_TIMESTAMP(6)"
		}
		lease = `lease_until>` + clock
	}
	return requireTrue(s.queryRow(ctx, tx, `SELECT state=$5 AND generation=$4 AND error_code=$6 AND (`+lease+`) FROM market_install_operations WHERE id=$1 AND tenant_id=$2 AND subject_id=$3`, op.ID, op.TenantID, op.SubjectID, op.Generation, state, code))
}

func (s *Store) verifyStepTarget(ctx context.Context, tx *sql.Tx, w app.Work, state string) error {
	return requireTrue(s.queryRow(ctx, tx, `SELECT state=$5 FROM market_install_steps WHERE operation_id=$1 AND ordinal=$2 AND step_id=$3 AND payload_hash=$4`, w.Operation.ID, w.Index, w.Step.ID, w.PayloadHash, state))
}

func (s *Store) verifyConfirmedReceipt(ctx context.Context, tx *sql.Tx, w app.Work, encoded string) error {
	match := `receipt=$5::jsonb`
	if s.dialect == MySQL {
		match = `CAST(receipt AS BINARY)=CAST($5 AS BINARY)`
	}
	return requireTrue(s.queryRow(ctx, tx, `SELECT state='confirmed' AND (`+match+`) FROM market_install_steps WHERE operation_id=$1 AND ordinal=$2 AND step_id=$3 AND payload_hash=$4`, w.Operation.ID, w.Index, w.Step.ID, w.PayloadHash, encoded))
}

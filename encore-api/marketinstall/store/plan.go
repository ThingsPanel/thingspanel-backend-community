package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"encore.app/marketinstall/app"
)

// PlanEncodingV1 identifies the existing v1 hash's JSON-number lexemes. It does
// not change app.Digest, StepHash, the provider protocol or old step receipts.
const PlanEncodingV1 = "json-number-lexeme-v1"

type storedOperation struct {
	operation app.Operation
	encoding  string
	legacy    bool
	rawPlan   []byte
}

func (s *Store) operationColumns() string {
	plan := "plan"
	if s.dialect == PostgreSQL {
		plan = "plan::text"
	}
	return `id,bundle_key,bundle_version,tenant_id,subject_id,authorization_ref,request_key,request_digest,artifact_digest,` + plan + `,plan_encoding,plan_exact,state,generation,error_code`
}

func scanStoredOperation(row scanner) (storedOperation, error) {
	var record storedOperation
	op := &record.operation
	var legacy, exact []byte
	var encoding sql.NullString
	if err := row.Scan(&op.ID, &op.BundleKey, &op.Version, &op.TenantID, &op.SubjectID, &op.AuthorizationRef, &op.RequestKey, &op.RequestDigest, &op.ArtifactDigest, &legacy, &encoding, &exact, &op.State, &op.Generation, &op.ErrorCode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return record, app.ErrNotFound
		}
		return record, err
	}
	record.encoding = encoding.String
	record.legacy = !encoding.Valid
	switch {
	case !encoding.Valid:
		if exact != nil {
			return storedOperation{}, fmt.Errorf("legacy installation has an unversioned exact plan")
		}
		record.rawPlan = legacy
	case encoding.String == PlanEncodingV1:
		if exact == nil {
			return storedOperation{}, fmt.Errorf("installation exact plan is missing")
		}
		record.rawPlan = exact
	default:
		return storedOperation{}, fmt.Errorf("installation plan encoding is unknown")
	}
	if err := json.Unmarshal(record.rawPlan, &op.Plan); err != nil {
		return storedOperation{}, err
	}
	return record, nil
}

func scanOperation(row scanner) (app.Operation, error) {
	record, err := scanStoredOperation(row)
	return record.operation, err
}

// Legacy status queries remain read-only and visible. Adoption happens only at
// Claim, while the original operation row is locked, after every recorded step
// matches the original plan. A changed digest is never recomputed and stored.
func (s *Store) verifyPlanLedger(ctx context.Context, tx *sql.Tx, op app.Operation, initial bool) error {
	if err := app.ValidatePlan(op.Plan); err != nil {
		return err
	}
	if op.ArtifactDigest != op.Plan.ArtifactDigest {
		return fmt.Errorf("installation artifact identity and plan diverged")
	}
	rows, err := s.query(ctx, tx, `SELECT ordinal,step_id,idempotency_key,payload_hash,state FROM market_install_steps WHERE operation_id=$1 ORDER BY ordinal`, op.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var ordinal int
		var id, key, hash, state string
		if err := rows.Scan(&ordinal, &id, &key, &hash, &state); err != nil {
			return err
		}
		if ordinal != count || count >= len(op.Plan.Steps) {
			return fmt.Errorf("installation step ordinals or count diverged")
		}
		step := op.Plan.Steps[count]
		expected, err := app.StepHash(step)
		if err != nil {
			return err
		}
		if step.ID != id || key != app.StableStepKey(op.ID, step) || hash != expected {
			return fmt.Errorf("installation step identity and plan diverged")
		}
		if initial && state != "pending" {
			return fmt.Errorf("new installation step was not persisted as pending")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(op.Plan.Steps) {
		return fmt.Errorf("installation plan has incomplete step identities")
	}
	return nil
}

func (s *Store) adoptExactPlan(ctx context.Context, tx *sql.Tx, record storedOperation) error {
	if !record.legacy && record.encoding == PlanEncodingV1 {
		return nil
	}
	if !record.legacy {
		return fmt.Errorf("unknown installation plan encoding cannot be adopted")
	}
	result, err := s.exec(ctx, tx, `UPDATE market_install_operations SET plan_encoding=$2,plan_exact=$3 WHERE id=$1 AND plan_encoding IS NULL AND plan_exact IS NULL`, record.operation.ID, PlanEncodingV1, string(record.rawPlan))
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	var encoding string
	var exact []byte
	if err := s.queryRow(ctx, tx, `SELECT plan_encoding,plan_exact FROM market_install_operations WHERE id=$1`, record.operation.ID).Scan(&encoding, &exact); err != nil {
		return err
	}
	if encoding != PlanEncodingV1 || !bytes.Equal(exact, record.rawPlan) {
		return fmt.Errorf("installation exact-plan adoption was not persisted")
	}
	return nil
}

func (s *Store) verifyCreatedOperation(ctx context.Context, tx *sql.Tx, expected app.Operation, encoded []byte) (app.Operation, error) {
	record, err := scanStoredOperation(s.queryRow(ctx, tx, `SELECT `+s.operationColumns()+` FROM market_install_operations WHERE id=$1`, expected.ID))
	if err != nil {
		return app.Operation{}, err
	}
	op := record.operation
	if op.ID != expected.ID || op.TenantID != expected.TenantID || op.SubjectID != expected.SubjectID || op.AuthorizationRef != expected.AuthorizationRef || op.RequestKey != expected.RequestKey || op.RequestDigest != expected.RequestDigest || op.ArtifactDigest != expected.ArtifactDigest || op.BundleKey != expected.BundleKey || op.Version != expected.Version || op.State != app.Queued || op.Generation != 0 || op.ErrorCode != "" || record.encoding != PlanEncodingV1 || !bytes.Equal(record.rawPlan, encoded) {
		return app.Operation{}, fmt.Errorf("new installation metadata or exact plan was not persisted")
	}
	projection := `plan=$2::jsonb`
	if s.dialect == MySQL {
		projection = `CAST(plan AS BINARY)=CAST($2 AS BINARY)`
	}
	if err := requireTrue(s.queryRow(ctx, tx, `SELECT `+projection+` FROM market_install_operations WHERE id=$1`, op.ID, string(encoded))); err != nil {
		return app.Operation{}, err
	}
	if err := s.verifyPlanLedger(ctx, tx, op, true); err != nil {
		return app.Operation{}, err
	}
	return op, nil
}

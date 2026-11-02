// Package store owns the A08 intent/step PostgreSQL ledger. Short local
// transactions fence progress; provider HTTP calls are never inside them.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"encore.app/marketinstall/app"
)

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

const operationColumns = `id,bundle_key,bundle_version,tenant_id,subject_id,authorization_ref,request_key,request_digest,artifact_digest,plan,state,generation,error_code`

type scanner interface{ Scan(...interface{}) error }

func scanOperation(row scanner) (app.Operation, error) {
	var op app.Operation
	var plan []byte
	if err := row.Scan(&op.ID, &op.BundleKey, &op.Version, &op.TenantID, &op.SubjectID, &op.AuthorizationRef, &op.RequestKey, &op.RequestDigest, &op.ArtifactDigest, &plan, &op.State, &op.Generation, &op.ErrorCode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return op, app.ErrNotFound
		}
		return op, err
	}
	if err := json.Unmarshal(plan, &op.Plan); err != nil {
		return app.Operation{}, err
	}
	return op, nil
}
func (s *Store) Get(ctx context.Context, tenant, subject, key string) (app.Operation, error) {
	return scanOperation(s.DB.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM market_install_operations WHERE tenant_id=$1 AND subject_id=$2 AND request_key=$3`, tenant, subject, key))
}
func (s *Store) GetByID(ctx context.Context, tenant, subject, id string) (app.Operation, error) {
	return scanOperation(s.DB.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM market_install_operations WHERE tenant_id=$1 AND subject_id=$2 AND id=$3`, tenant, subject, id))
}
func (s *Store) Create(ctx context.Context, op app.Operation) (app.Operation, error) {
	if err := app.ValidatePlan(op.Plan); err != nil {
		return app.Operation{}, err
	}
	encoded, err := json.Marshal(op.Plan)
	if err != nil {
		return app.Operation{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return app.Operation{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO market_install_operations(id,tenant_id,subject_id,authorization_ref,request_key,request_digest,artifact_digest,plan,state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,'queued') ON CONFLICT(tenant_id,request_key) DO NOTHING`, op.ID, op.TenantID, op.SubjectID, op.AuthorizationRef, op.RequestKey, op.RequestDigest, op.ArtifactDigest, string(encoded))
	if err != nil {
		return app.Operation{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return app.Operation{}, err
	}
	if affected == 0 {
		current, err := scanOperation(tx.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM market_install_operations WHERE tenant_id=$1 AND request_key=$2`, op.TenantID, op.RequestKey))
		if err != nil {
			return app.Operation{}, err
		}
		if current.SubjectID != op.SubjectID || current.RequestDigest != op.RequestDigest {
			return app.Operation{}, app.ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return app.Operation{}, err
		}
		return current, nil
	}
	if affected != 1 {
		return app.Operation{}, app.ErrLeaseLost
	}
	result, err = tx.ExecContext(ctx, `UPDATE market_install_operations SET bundle_key=$2,bundle_version=$3 WHERE id=$1`, op.ID, op.BundleKey, op.Version)
	if err := exactlyOne(result, err); err != nil {
		return app.Operation{}, err
	}
	for index, step := range op.Plan.Steps {
		hash, err := app.StepHash(step)
		if err != nil {
			return app.Operation{}, err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO market_install_steps(operation_id,ordinal,step_id,idempotency_key,payload_hash,state) VALUES($1,$2,$3,$4,$5,'pending')`, op.ID, index, step.ID, app.StableStepKey(op.ID, step), hash)
		if err := exactlyOne(result, err); err != nil {
			return app.Operation{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return app.Operation{}, err
	}
	return op, nil
}
func (s *Store) Claim(ctx context.Context, leaseSeconds int) (*app.Work, error) {
	if leaseSeconds < 5 || leaseSeconds > 300 {
		return nil, app.ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	op, err := scanOperation(tx.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM market_install_operations
 WHERE state='queued' OR(state='running' AND lease_until<NOW()) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`))
	if errors.Is(err, app.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	op.Generation++
	result, err := tx.ExecContext(ctx, `UPDATE market_install_operations SET state='running',generation=$2,lease_until=NOW()+($3 * INTERVAL '1 second'),updated_at=NOW() WHERE id=$1`, op.ID, op.Generation, leaseSeconds)
	if err := exactlyOne(result, err); err != nil {
		return nil, err
	}
	op.State = app.Running
	var index int
	var stepID, key, hash string
	err = tx.QueryRowContext(ctx, `SELECT ordinal,step_id,idempotency_key,payload_hash FROM market_install_steps WHERE operation_id=$1 AND state<>'confirmed' ORDER BY ordinal LIMIT 1`, op.ID).Scan(&index, &stepID, &key, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("installation has no pending step but is not complete")
	}
	if err != nil {
		return nil, err
	}
	if index >= len(op.Plan.Steps) || op.Plan.Steps[index].ID != stepID {
		return nil, fmt.Errorf("installation step ledger and plan diverged")
	}
	if err := app.ValidatePlan(op.Plan); err != nil {
		return nil, err
	}
	expectedHash, err := app.StepHash(op.Plan.Steps[index])
	if err != nil {
		return nil, err
	}
	if expectedHash != hash || app.StableStepKey(op.ID, op.Plan.Steps[index]) != key {
		return nil, fmt.Errorf("installation step identity and plan diverged")
	}
	prior := make([]app.PriorResult, 0, index)
	rows, err := tx.QueryContext(ctx, `SELECT step_id,receipt FROM market_install_steps WHERE operation_id=$1 AND ordinal<$2 AND state='confirmed' ORDER BY ordinal`, op.ID, index)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item app.PriorResult
		var raw []byte
		if err := rows.Scan(&item.StepID, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Receipt); err != nil {
			rows.Close()
			return nil, err
		}
		prior = append(prior, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(prior) != index {
		return nil, fmt.Errorf("installation prior receipts are incomplete")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &app.Work{Operation: op, Step: op.Plan.Steps[index], Index: index, IdempotencyKey: key, PayloadHash: hash, PriorResults: prior}, nil
}
func lockWork(ctx context.Context, tx *sql.Tx, w app.Work) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM market_install_operations WHERE id=$1 AND tenant_id=$2 AND subject_id=$3 AND generation=$4 AND state='running' AND lease_until>NOW() FOR UPDATE`, w.Operation.ID, w.Operation.TenantID, w.Operation.SubjectID, w.Operation.Generation).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ErrLeaseLost
	}
	return err
}
func (s *Store) Dispatch(ctx context.Context, w app.Work) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockWork(ctx, tx, w); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE market_install_steps SET state='dispatched',updated_at=NOW() WHERE operation_id=$1 AND ordinal=$2 AND step_id=$3 AND payload_hash=$4 AND state IN('pending','dispatched','unknown')`, w.Operation.ID, w.Index, w.Step.ID, w.PayloadHash)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Confirm(ctx context.Context, w app.Work, receipt app.Receipt) error {
	if !app.ValidReceipt(receipt, w.Step, w.PayloadHash) {
		return app.ErrInvalid
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockWork(ctx, tx, w); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE market_install_steps SET state='confirmed',receipt=$5::jsonb,updated_at=NOW() WHERE operation_id=$1 AND ordinal=$2 AND step_id=$3 AND payload_hash=$4 AND state IN('pending','dispatched','unknown')`, w.Operation.ID, w.Index, w.Step.ID, w.PayloadHash, string(encoded))
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_install_steps WHERE operation_id=$1 AND state<>'confirmed'`, w.Operation.ID).Scan(&pending); err != nil {
		return err
	}
	target := app.Queued
	if pending == 0 {
		target = app.Completed
	}
	result, err = tx.ExecContext(ctx, `UPDATE market_install_operations SET state=$2,lease_until=NULL,error_code='',updated_at=NOW() WHERE id=$1`, w.Operation.ID, target)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Hold(ctx context.Context, w app.Work, state, code string) error {
	if state != app.Paused && state != app.Rejected {
		return app.ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockWork(ctx, tx, w); err != nil {
		return err
	}
	stepState := "unknown"
	if state == app.Rejected {
		stepState = "rejected"
	}
	result, err := tx.ExecContext(ctx, `UPDATE market_install_steps SET state=$3,updated_at=NOW() WHERE operation_id=$1 AND ordinal=$2 AND state<>'confirmed'`, w.Operation.ID, w.Index, stepState)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE market_install_operations SET state=$2,error_code=$3,lease_until=NULL,updated_at=NOW() WHERE id=$1`, w.Operation.ID, state, code)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Resume(ctx context.Context, tenant, subject, id string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE market_install_operations SET state='queued',error_code='',updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND subject_id=$3 AND state='paused'`, id, tenant, subject)
	return exactlyOne(result, err)
}
func exactlyOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return app.ErrLeaseLost
	}
	return nil
}

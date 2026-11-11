// Package store owns the A08 intent/step ledger. Short local
// transactions fence progress; provider HTTP calls are never inside them.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"encore.app/marketinstall/app"
)

type Store struct {
	DB      *sql.DB
	dialect Dialect
}

// New preserves the established PostgreSQL owner and Encore connection.
func New(db *sql.DB) *Store { return &Store{DB: db, dialect: PostgreSQL} }

type scanner interface{ Scan(...interface{}) error }

func (s *Store) Get(ctx context.Context, tenant, subject, key string) (app.Operation, error) {
	return scanOperation(s.queryRow(ctx, s.DB, `SELECT `+s.operationColumns()+` FROM market_install_operations WHERE tenant_id=$1 AND subject_id=$2 AND request_key=$3`, tenant, subject, key))
}
func (s *Store) GetByID(ctx context.Context, tenant, subject, id string) (app.Operation, error) {
	return scanOperation(s.queryRow(ctx, s.DB, `SELECT `+s.operationColumns()+` FROM market_install_operations WHERE tenant_id=$1 AND subject_id=$2 AND id=$3`, tenant, subject, id))
}
func (s *Store) Create(ctx context.Context, op app.Operation) (app.Operation, error) {
	if err := app.ValidatePlan(op.Plan); err != nil {
		return app.Operation{}, err
	}
	encoded, err := json.Marshal(op.Plan)
	if err != nil {
		return app.Operation{}, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return app.Operation{}, err
	}
	defer tx.Rollback()
	result, err := s.exec(ctx, tx, s.insertOperationSQL(), op.ID, op.TenantID, op.SubjectID, op.AuthorizationRef, op.RequestKey, op.RequestDigest, op.ArtifactDigest, string(encoded), string(encoded))
	var affected int64
	if s.isDuplicate(err) {
		// MySQL rejects the statement, not the surrounding transaction. Only an
		// exact existing tenant/key result may turn error 1062 into a replay.
		affected = 0
	} else if err != nil {
		return app.Operation{}, err
	} else {
		affected, err = result.RowsAffected()
		if err != nil {
			return app.Operation{}, err
		}
	}
	if affected == 0 {
		current, err := scanOperation(s.queryRow(ctx, tx, `SELECT `+s.operationColumns()+` FROM market_install_operations WHERE tenant_id=$1 AND request_key=$2`, op.TenantID, op.RequestKey))
		if errors.Is(err, app.ErrNotFound) {
			return app.Operation{}, app.ErrConflict
		}
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
	result, err = s.exec(ctx, tx, `UPDATE market_install_operations SET bundle_key=$2,bundle_version=$3 WHERE id=$1`, op.ID, op.BundleKey, op.Version)
	if err := exactlyOne(result, err); err != nil {
		return app.Operation{}, err
	}
	for index, step := range op.Plan.Steps {
		hash, err := app.StepHash(step)
		if err != nil {
			return app.Operation{}, err
		}
		result, err := s.exec(ctx, tx, `INSERT INTO market_install_steps(operation_id,ordinal,step_id,idempotency_key,payload_hash,state) VALUES($1,$2,$3,$4,$5,'pending')`, op.ID, index, step.ID, app.StableStepKey(op.ID, step), hash)
		if err := exactlyOne(result, err); err != nil {
			return app.Operation{}, err
		}
	}
	created, err := s.verifyCreatedOperation(ctx, tx, op, encoded)
	if err != nil {
		return app.Operation{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.Operation{}, err
	}
	return created, nil
}
func (s *Store) Claim(ctx context.Context, leaseSeconds int) (*app.Work, error) {
	if leaseSeconds < 5 || leaseSeconds > 300 {
		return nil, app.ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	record, err := scanStoredOperation(s.queryRow(ctx, tx, s.claimSQL()))
	op := record.operation
	if errors.Is(err, app.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.verifyPlanLedger(ctx, tx, op, false); err != nil {
		return nil, err
	}
	if err := s.adoptExactPlan(ctx, tx, record); err != nil {
		return nil, err
	}
	if op.Generation < 0 || op.Generation == math.MaxInt64 {
		return nil, fmt.Errorf("installation generation is exhausted or invalid")
	}
	previousGeneration := op.Generation
	op.Generation++
	result, err := s.exec(ctx, tx, s.claimUpdateSQL(), op.ID, op.Generation, leaseSeconds, previousGeneration)
	if err := exactlyOne(result, err); err != nil {
		return nil, err
	}
	if err := s.verifyOperationTarget(ctx, tx, op, app.Running, op.ErrorCode, true); err != nil {
		return nil, err
	}
	op.State = app.Running
	var index int
	var stepID, key, hash string
	err = s.queryRow(ctx, tx, `SELECT ordinal,step_id,idempotency_key,payload_hash FROM market_install_steps WHERE operation_id=$1 AND state<>'confirmed' ORDER BY ordinal LIMIT 1`, op.ID).Scan(&index, &stepID, &key, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("installation has no pending step but is not complete")
	}
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(op.Plan.Steps) || op.Plan.Steps[index].ID != stepID {
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
	rows, err := s.query(ctx, tx, `SELECT step_id,receipt FROM market_install_steps WHERE operation_id=$1 AND ordinal<$2 AND state='confirmed' ORDER BY ordinal`, op.ID, index)
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
func (s *Store) lockWork(ctx context.Context, tx *sql.Tx, w app.Work) error {
	var id string
	err := s.queryRow(ctx, tx, `SELECT id FROM market_install_operations WHERE id=$1 AND tenant_id=$2 AND subject_id=$3 AND generation=$4 AND state='running' FOR UPDATE`, w.Operation.ID, w.Operation.TenantID, w.Operation.SubjectID, w.Operation.Generation).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	// Re-read the server clock after acquiring the row lock. A statement or PG
	// transaction timestamp taken before a lock wait can outlive the lease.
	clock := "clock_timestamp()"
	if s.dialect == MySQL {
		clock = "UTC_TIMESTAMP(6)"
	}
	var valid sql.NullBool
	if err := s.queryRow(ctx, tx, `SELECT lease_until>`+clock+` FROM market_install_operations WHERE id=$1`, id).Scan(&valid); err != nil {
		return err
	}
	if !valid.Valid || !valid.Bool {
		return app.ErrLeaseLost
	}
	return nil
}

func (s *Store) Dispatch(ctx context.Context, w app.Work) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockWork(ctx, tx, w); err != nil {
		return err
	}
	result, err := s.exec(ctx, tx, `UPDATE market_install_steps SET state='dispatched',updated_at=CURRENT_TIMESTAMP(6) WHERE operation_id=$1 AND ordinal=$2 AND step_id=$3 AND payload_hash=$4 AND state IN('pending','dispatched','unknown')`, w.Operation.ID, w.Index, w.Step.ID, w.PayloadHash)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	if err := s.verifyStepTarget(ctx, tx, w, "dispatched"); err != nil {
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
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockWork(ctx, tx, w); err != nil {
		return err
	}
	result, err := s.exec(ctx, tx, `UPDATE market_install_steps SET state='confirmed',receipt=`+s.jsonParameter("$5")+`,updated_at=CURRENT_TIMESTAMP(6) WHERE operation_id=$1 AND ordinal=$2 AND step_id=$3 AND payload_hash=$4 AND state IN('pending','dispatched','unknown')`, w.Operation.ID, w.Index, w.Step.ID, w.PayloadHash, string(encoded))
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	if err := s.verifyConfirmedReceipt(ctx, tx, w, string(encoded)); err != nil {
		return err
	}
	var pending int
	if err := s.queryRow(ctx, tx, `SELECT COUNT(*) FROM market_install_steps WHERE operation_id=$1 AND state<>'confirmed'`, w.Operation.ID).Scan(&pending); err != nil {
		return err
	}
	target := app.Queued
	if pending == 0 {
		target = app.Completed
	}
	result, err = s.exec(ctx, tx, `UPDATE market_install_operations SET state=$2,lease_until=NULL,error_code='',updated_at=CURRENT_TIMESTAMP(6) WHERE id=$1 AND generation=$3 AND state='running'`, w.Operation.ID, target, w.Operation.Generation)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	if err := s.verifyOperationTarget(ctx, tx, w.Operation, target, "", false); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Hold(ctx context.Context, w app.Work, state, code string) error {
	if state != app.Paused && state != app.Rejected {
		return app.ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.lockWork(ctx, tx, w); err != nil {
		return err
	}
	stepState := "unknown"
	if state == app.Rejected {
		stepState = "rejected"
	}
	result, err := s.exec(ctx, tx, `UPDATE market_install_steps SET state=$3,updated_at=CURRENT_TIMESTAMP(6) WHERE operation_id=$1 AND ordinal=$2 AND state<>'confirmed'`, w.Operation.ID, w.Index, stepState)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	if err := s.verifyStepTarget(ctx, tx, w, stepState); err != nil {
		return err
	}
	result, err = s.exec(ctx, tx, `UPDATE market_install_operations SET state=$2,error_code=$3,lease_until=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=$1 AND generation=$4 AND state='running'`, w.Operation.ID, state, code, w.Operation.Generation)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	if err := s.verifyOperationTarget(ctx, tx, w.Operation, state, code, false); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Resume(ctx context.Context, tenant, subject, id string) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	op := app.Operation{ID: id, TenantID: tenant, SubjectID: subject}
	err = s.queryRow(ctx, tx, `SELECT generation FROM market_install_operations WHERE id=$1 AND tenant_id=$2 AND subject_id=$3 AND state='paused' FOR UPDATE`, id, tenant, subject).Scan(&op.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	result, err := s.exec(ctx, tx, `UPDATE market_install_operations SET state='queued',error_code='',lease_until=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=$1 AND tenant_id=$2 AND subject_id=$3 AND generation=$4 AND state='paused'`, id, tenant, subject, op.Generation)
	if err := exactlyOne(result, err); err != nil {
		return err
	}
	if err := s.verifyOperationTarget(ctx, tx, op, app.Queued, "", false); err != nil {
		return err
	}
	return tx.Commit()
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

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"encore.app/automation/executor"
)

// Executor is one registered external engine.
type Executor struct {
	ID           string
	TenantID     string
	Name         string
	Engine       string
	Contract     executor.Contract
	ContractHash string
	Epoch        int
	Enabled      bool
	LastSeq      int64
	CredentialID string
	LastSeenAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ExecutorMessage is the record of one accepted (or refused) message.
type ExecutorMessage struct {
	TenantID       string
	ExecutorID     string
	IdempotencyKey string
	Direction      string
	Epoch          int
	Seq            int64
	Outcome        string
	ReasonCode     string
	Subject        string
	CreatedAt      time.Time
}

// CreateExecutor inserts a registration.
//
// last_seen_at is stamped here rather than left NULL: registering is itself a
// sign of life, and a row that says "never seen" would make a brand new executor
// show up as unhealthy in the list until its first heartbeat — which reads as a
// fault to the operator when nothing is wrong.
func (s *Store) CreateExecutor(ctx context.Context, e *Executor) error {
	body, err := json.Marshal(e.Contract)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
        INSERT INTO auto_executors
            (id, tenant_id, name, engine, contract, contract_hash, epoch, enabled, last_seq, credential_id, last_seen_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, now())`,
		e.ID, e.TenantID, e.Name, e.Engine, body, e.ContractHash, e.Epoch, e.Enabled, e.LastSeq, e.CredentialID)
	return err
}

func scanExecutor(row *sql.Row) (*Executor, error) {
	var (
		e        Executor
		contract []byte
		lastSeen sql.NullTime
	)
	if err := row.Scan(&e.ID, &e.TenantID, &e.Name, &e.Engine, &contract, &e.ContractHash,
		&e.Epoch, &e.Enabled, &e.LastSeq, &e.CredentialID, &lastSeen, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(contract, &e.Contract); err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		e.LastSeenAt = &t
	}
	return &e, nil
}

const executorColumns = `id, tenant_id, name, engine, contract, contract_hash,
        epoch, enabled, last_seq, coalesce(credential_id,''), last_seen_at, created_at, updated_at`

// GetExecutor returns one registration, scoped by tenant.
func (s *Store) GetExecutor(ctx context.Context, tenantID, id string) (*Executor, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+executorColumns+`
        FROM auto_executors WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	e, err := scanExecutor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// ListExecutors returns the tenant's registrations, newest first.
func (s *Store) ListExecutors(ctx context.Context, tenantID string, limit, offset int) ([]*Executor, int, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM auto_executors WHERE tenant_id=$1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+executorColumns+`
        FROM auto_executors WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*Executor, 0, limit)
	for rows.Next() {
		var (
			e        Executor
			contract []byte
			lastSeen sql.NullTime
		)
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Name, &e.Engine, &contract, &e.ContractHash,
			&e.Epoch, &e.Enabled, &e.LastSeq, &e.CredentialID, &lastSeen, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(contract, &e.Contract); err != nil {
			return nil, 0, err
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			e.LastSeenAt = &t
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}

// TouchExecutor records liveness. A missing row is not an error here: the caller
// already resolved the executor, and a heartbeat must not resurrect a deleted one.
func (s *Store) TouchExecutor(ctx context.Context, tenantID, id string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE auto_executors SET last_seen_at=$3, updated_at=now()
          WHERE tenant_id=$1 AND id=$2`, tenantID, id, at)
	return err
}

// SetExecutorEnabled stops or resumes an executor without deleting its history.
func (s *Store) SetExecutorEnabled(ctx context.Context, tenantID, id string, enabled bool) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE auto_executors SET enabled=$3, updated_at=now()
          WHERE tenant_id=$1 AND id=$2`, tenantID, id, enabled)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AdvanceExecutorSeq moves the accepted sequence forward, but only if this
// message is really newer.
//
// The condition lives in the UPDATE rather than in a read-then-write, because two
// messages with the same sequence number can arrive at the same instant and both
// would pass a read-then-write check. Exactly one of them may be accepted: the
// other one is a replay, and a replay of an action intent is a second device
// command.
func (s *Store) AdvanceExecutorSeq(ctx context.Context, tenantID, id string, epoch int, seq int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_executors
           SET last_seq=$4, last_seen_at=now(), updated_at=now()
         WHERE tenant_id=$1 AND id=$2 AND epoch=$3 AND last_seq < $4`,
		tenantID, id, epoch, seq)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RebindExecutorContract replaces the contract and bumps the epoch. Every message
// still in flight under the old epoch becomes unacceptable, which is the point.
func (s *Store) RebindExecutorContract(ctx context.Context, tenantID, id string, c executor.Contract, hash string) (*Executor, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_executors
           SET contract=$3, contract_hash=$4, engine=$5, epoch=epoch+1, last_seq=0, updated_at=now()
         WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, mustJSON(c), hash, string(c.Engine))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetExecutor(ctx, tenantID, id)
}

func mustJSON(v any) []byte {
	body, _ := json.Marshal(v)
	return body
}

// RecordExecutorMessage stores one message outcome. It is the durable half of the
// idempotency check: the sequence guard stops a replay within an epoch, and this
// table stops the same intent from being replayed after a re-registration reset
// the sequence.
func (s *Store) RecordExecutorMessage(ctx context.Context, m *ExecutorMessage) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_executor_messages
            (tenant_id, executor_id, idempotency_key, direction, epoch, seq, outcome, reason_code, subject)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
        ON CONFLICT (tenant_id, executor_id, idempotency_key) DO NOTHING`,
		m.TenantID, m.ExecutorID, m.IdempotencyKey, m.Direction, m.Epoch, m.Seq, m.Outcome, m.ReasonCode, m.Subject)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// LookupExecutorMessage returns the recorded outcome of an idempotency key.
func (s *Store) LookupExecutorMessage(ctx context.Context, tenantID, executorID, key string) (*ExecutorMessage, error) {
	var m ExecutorMessage
	err := s.DB.QueryRowContext(ctx, `
        SELECT tenant_id, executor_id, idempotency_key, direction, epoch, seq,
               outcome, coalesce(reason_code,''), coalesce(subject,''), created_at
          FROM auto_executor_messages
         WHERE tenant_id=$1 AND executor_id=$2 AND idempotency_key=$3`,
		tenantID, executorID, key).
		Scan(&m.TenantID, &m.ExecutorID, &m.IdempotencyKey, &m.Direction, &m.Epoch, &m.Seq,
			&m.Outcome, &m.ReasonCode, &m.Subject, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListExecutorMessages returns what one executor has sent and what the platform
// decided, newest first.
//
// This is the evidence half of the contract. Without it the operator can see
// that an engine is registered but not what it actually did, and "the engine
// says it sent it" would be the only account available.
func (s *Store) ListExecutorMessages(ctx context.Context, tenantID, executorID string, limit, offset int) ([]*ExecutorMessage, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM auto_executor_messages WHERE tenant_id=$1 AND executor_id=$2`,
		tenantID, executorID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `
        SELECT tenant_id, executor_id, idempotency_key, direction, epoch, seq,
               outcome, coalesce(reason_code,''), coalesce(subject,''), created_at
          FROM auto_executor_messages
         WHERE tenant_id=$1 AND executor_id=$2
         ORDER BY created_at DESC, idempotency_key DESC
         LIMIT $3 OFFSET $4`, tenantID, executorID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*ExecutorMessage, 0, limit)
	for rows.Next() {
		var m ExecutorMessage
		if err := rows.Scan(&m.TenantID, &m.ExecutorID, &m.IdempotencyKey, &m.Direction, &m.Epoch,
			&m.Seq, &m.Outcome, &m.ReasonCode, &m.Subject, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

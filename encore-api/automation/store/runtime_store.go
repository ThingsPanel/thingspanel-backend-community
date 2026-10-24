package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"encore.app/automation/domain"
)

// ---------------------------------------------------------------------------
// Ingress
// ---------------------------------------------------------------------------

// IngestEvent is the durable receipt record. EventID is the dedupe identity.
type IngestEvent struct {
	EventID    string
	TenantID   string
	DeviceID   string
	Signal     domain.SignalKind
	Key        string
	Value      any
	Unit       string
	Quality    domain.Quality
	ObservedAt time.Time
	Params     map[string]any
	Source     string
	// InputClass names the adapter that produced this event (telemetry,
	// gateway_attribute, alarm, ...) so the explain view does not have to
	// infer it from the signal.
	InputClass string
	// GatewayID is set when a gateway relayed the sample.
	GatewayID string
	// CausalRoot and CausalHop carry the alarm-driven chain origin and depth.
	CausalRoot string
	CausalHop  int
	// Seq is the receipt order assigned by the store; it is read back when a
	// receipt is replayed after a restart.
	Seq int64
}

// Receipt is the outcome of an ingest attempt.
type Receipt struct {
	EventID   string
	Seq       int64
	Duplicate bool
}

// InsertEvent persists the event. A duplicate event_id returns Duplicate=true
// and does not advance the sequence: this is the idempotency boundary that keeps
// a replayed broker message from firing a second physical action.
func (s *Store) InsertEvent(ctx context.Context, e *IngestEvent) (*Receipt, error) {
	value, err := json.Marshal(e.Value)
	if err != nil {
		return nil, err
	}
	params := []byte(nil)
	if e.Params != nil {
		params, _ = json.Marshal(e.Params)
	}
	source := e.Source
	if source == "" {
		source = "uplink"
	}
	quality := e.Quality
	if quality == "" {
		quality = domain.QualityGood
	}

	seq, err := s.NextSeq(ctx)
	if err != nil {
		return nil, err
	}

	res, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_inbox (event_id, tenant_id, device_id, signal, key, value, unit,
                                quality, observed_at, seq, source, params, input_class, gateway_id,
                                causal_root, causal_hop)
        VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12,NULLIF($13,''),NULLIF($14,''),
                NULLIF($15,''),$16)
        ON CONFLICT (event_id) DO NOTHING`,
		e.EventID, e.TenantID, e.DeviceID, string(e.Signal), e.Key, value, e.Unit,
		string(quality), e.ObservedAt, seq, source, params, e.InputClass, e.GatewayID,
		e.CausalRoot, e.CausalHop)
	if err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var existing int64
		_ = s.DB.QueryRowContext(ctx, `SELECT seq FROM auto_inbox WHERE event_id=$1`, e.EventID).Scan(&existing)
		e.Seq = existing
		return &Receipt{EventID: e.EventID, Seq: existing, Duplicate: true}, nil
	}
	e.Seq = seq
	return &Receipt{EventID: e.EventID, Seq: seq}, nil
}

// ApplyState projects the event into the latest-value table.
func (s *Store) ApplyState(ctx context.Context, e *IngestEvent) error {
	value, err := json.Marshal(e.Value)
	if err != nil {
		return err
	}
	quality := e.Quality
	if quality == "" {
		quality = domain.QualityGood
	}
	_, err = s.DB.ExecContext(ctx, `
        INSERT INTO auto_state (device_id, signal, key, tenant_id, value, unit, quality, observed_at, seq)
        VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,(SELECT seq FROM auto_inbox WHERE event_id=$9))
        ON CONFLICT (device_id, signal, key) DO UPDATE
           SET tenant_id=EXCLUDED.tenant_id, value=EXCLUDED.value, unit=EXCLUDED.unit,
               quality=EXCLUDED.quality, observed_at=EXCLUDED.observed_at,
               seq=EXCLUDED.seq, updated_at=now()
         WHERE auto_state.observed_at <= EXCLUDED.observed_at`,
		e.DeviceID, string(e.Signal), e.Key, e.TenantID, value, e.Unit, string(quality),
		e.ObservedAt, e.EventID)
	return err
}

// MarkProcessed records that the evaluator consumed the event.
func (s *Store) MarkProcessed(ctx context.Context, eventID string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE auto_inbox SET processed_at=now() WHERE event_id=$1 AND processed_at IS NULL`, eventID)
	return err
}

// LatestState reads the newest stored value for a device/signal/key.
func (s *Store) LatestState(ctx context.Context, deviceID string, signal domain.SignalKind, key string) (domain.Sample, bool, error) {
	var (
		raw        []byte
		unit       sql.NullString
		quality    string
		observedAt time.Time
		seq        int64
	)
	err := s.DB.QueryRowContext(ctx, `
        SELECT value, unit, quality, observed_at, seq
          FROM auto_state WHERE device_id=$1 AND signal=$2 AND key=$3`,
		deviceID, string(signal), key).Scan(&raw, &unit, &quality, &observedAt, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Sample{}, false, nil
	}
	if err != nil {
		return domain.Sample{}, false, err
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return domain.Sample{
		DeviceID: deviceID, Signal: signal, Key: key, Value: v,
		Unit: unit.String, Quality: domain.Quality(quality),
		ObservedAt: observedAt, Seq: seq,
	}, true, nil
}

// UnprocessedBefore reports the number of received-but-not-yet-evaluated events
// for a device that arrived at or before the given instant. The timer uses this
// as the input-progress barrier (V31): a 5:00 deadline must not fire while a
// 4:59 normal sample is still sitting in the inbox.
func (s *Store) UnprocessedBefore(ctx context.Context, tenantID, deviceID string, before time.Time) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
        SELECT count(*) FROM auto_inbox
         WHERE tenant_id=$1 AND device_id=$2 AND processed_at IS NULL AND received_at <= $3`,
		tenantID, deviceID, before).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Pending
// ---------------------------------------------------------------------------

// Pending is one "condition has been true since" record.
type Pending struct {
	ScopeKey               string
	TenantID               string
	RuleID                 string
	Revision               int
	DeviceID               string
	StartedAt              time.Time
	Deadline               time.Time
	LastEvidenceAt         time.Time
	EvidenceSeq            int64
	RequireProcessedBefore time.Time
	State                  string
	DeferCount             int
	// CausalRoot and CausalHop keep the alarm-driven chain across a wait, so a
	// loop that passes through a "for: 5m" rule still hits the hop limit.
	CausalRoot string
	CausalHop  int
}

// UpsertPending creates or extends the pending record for a scope.
func (s *Store) UpsertPending(ctx context.Context, p *Pending) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_pending (scope_key, tenant_id, rule_id, revision, device_id, started_at,
                                  deadline, last_evidence_at, evidence_seq, require_processed_before,
                                  state, causal_root, causal_hop)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',NULLIF($11,''),$12)
        ON CONFLICT (scope_key) DO UPDATE
           SET revision=EXCLUDED.revision, deadline=EXCLUDED.deadline,
               last_evidence_at=EXCLUDED.last_evidence_at, evidence_seq=EXCLUDED.evidence_seq,
               require_processed_before=EXCLUDED.require_processed_before,
               causal_root=EXCLUDED.causal_root, causal_hop=EXCLUDED.causal_hop,
               state='pending', updated_at=now()`,
		p.ScopeKey, p.TenantID, p.RuleID, p.Revision, p.DeviceID, p.StartedAt,
		p.Deadline, p.LastEvidenceAt, p.EvidenceSeq, p.RequireProcessedBefore,
		p.CausalRoot, p.CausalHop)
	return err
}

// GetPending returns the pending row for a scope.
func (s *Store) GetPending(ctx context.Context, scopeKey string) (*Pending, error) {
	var p Pending
	err := s.DB.QueryRowContext(ctx, `
        SELECT scope_key, tenant_id, rule_id, revision, device_id, started_at, deadline,
               last_evidence_at, evidence_seq, require_processed_before, state, defer_count,
               coalesce(causal_root,''), causal_hop
          FROM auto_pending WHERE scope_key=$1`, scopeKey).
		Scan(&p.ScopeKey, &p.TenantID, &p.RuleID, &p.Revision, &p.DeviceID, &p.StartedAt,
			&p.Deadline, &p.LastEvidenceAt, &p.EvidenceSeq, &p.RequireProcessedBefore,
			&p.State, &p.DeferCount, &p.CausalRoot, &p.CausalHop)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ClearPending removes a pending row (condition no longer holds, or fired).
func (s *Store) ClearPending(ctx context.Context, scopeKey string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM auto_pending WHERE scope_key=$1`, scopeKey)
	return err
}

// DeferPending pushes the deadline of a pending row that could not be decided
// because the input progress had not caught up yet.
func (s *Store) DeferPending(ctx context.Context, scopeKey string, next time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_pending SET deadline=$2, defer_count=defer_count+1, updated_at=now()
         WHERE scope_key=$1 AND state='pending'`, scopeKey, next)
	return err
}

// DuePending returns pending rows whose deadline has passed, bounded by limit.
func (s *Store) DuePending(ctx context.Context, now time.Time, limit int) ([]*Pending, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT scope_key, tenant_id, rule_id, revision, device_id, started_at, deadline,
               last_evidence_at, evidence_seq, require_processed_before, state, defer_count,
               coalesce(causal_root,''), causal_hop
          FROM auto_pending
         WHERE state='pending' AND deadline <= $1
         ORDER BY deadline
         LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Pending
	for rows.Next() {
		var p Pending
		if err := rows.Scan(&p.ScopeKey, &p.TenantID, &p.RuleID, &p.Revision, &p.DeviceID,
			&p.StartedAt, &p.Deadline, &p.LastEvidenceAt, &p.EvidenceSeq,
			&p.RequireProcessedBefore, &p.State, &p.DeferCount,
			&p.CausalRoot, &p.CausalHop); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

// Run is one accepted execution.
type Run struct {
	ID             string
	TenantID       string
	RuleID         string
	Revision       int
	DefinitionHash string
	ScopeKey       string
	Generation     int64
	DeviceID       string
	TriggerEventID string
	// CausalRoot is the alarm transition that started this chain; CausalHop is
	// how many alarm-triggered runs deep it is. Both are empty for a run started
	// by a plain telemetry sample.
	CausalRoot   string
	CausalHop    int
	CauseEventID string
	Status       string
	ReasonCode   string
	Reason       string
	Snapshot     json.RawMessage
	StartedAt    time.Time
	FinishedAt   *time.Time
	Deadline     *time.Time
	// CursorPC is the program counter that has started. A resume continues from
	// here, never from the beginning, so a crash cannot replay finished steps.
	CursorPC int
	// ResumeCount is how many times the run was taken over mid-flight. It is
	// evidence for "this run survived a crash", not a health signal.
	ResumeCount int
}

// ActiveRunCount counts runs that still hold the scope.
func (s *Store) ActiveRunCount(ctx context.Context, scopeKey string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
        SELECT count(*) FROM auto_runs
         WHERE scope_key=$1 AND status IN ('accepted','running')`, scopeKey).Scan(&n)
	return n, err
}

// NextGeneration returns the next execution generation for a scope.
func (s *Store) NextGeneration(ctx context.Context, scopeKey string) (int64, error) {
	var gen int64
	err := s.DB.QueryRowContext(ctx, `
        SELECT coalesce(max(generation),0)+1 FROM auto_runs WHERE scope_key=$1`, scopeKey).Scan(&gen)
	return gen, err
}

// CreateRun inserts the accepted run.
func (s *Store) CreateRun(ctx context.Context, r *Run) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_runs (id, tenant_id, rule_id, revision, definition_hash, scope_key,
                               generation, device_id, trigger_event_id, status, reason_code, reason,
                               snapshot, started_at, deadline, causal_root, causal_hop, cause_event_id)
        VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,NULLIF($11,''),
                NULLIF($12,''),$13,$14,$15,NULLIF($16,''),$17,NULLIF($18,''))`,
		r.ID, r.TenantID, r.RuleID, r.Revision, r.DefinitionHash, r.ScopeKey,
		r.Generation, r.DeviceID, r.TriggerEventID, r.Status, r.ReasonCode, r.Reason,
		r.Snapshot, r.StartedAt, r.Deadline, r.CausalRoot, r.CausalHop, r.CauseEventID)
	return err
}

// FinishRun closes a run.
func (s *Store) FinishRun(ctx context.Context, runID, status, reasonCode, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_runs SET status=$2, reason_code=NULLIF($3,''), reason=NULLIF($4,''), finished_at=now()
         WHERE id=$1`, runID, status, reasonCode, reason)
	return err
}

// CancelActiveRuns cancels every run still holding a scope and returns them, so
// the caller can mark their un-dispatched steps as cancelled.
func (s *Store) CancelActiveRuns(ctx context.Context, scopeKey, reasonCode, reason string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
        UPDATE auto_runs SET status='cancelled', reason_code=$2, reason=$3, finished_at=now()
         WHERE scope_key=$1 AND status IN ('accepted','running')
        RETURNING id`, scopeKey, reasonCode, reason)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetRun loads one run.
func (s *Store) GetRun(ctx context.Context, tenantID, runID string) (*Run, error) {
	var (
		r          Run
		deviceID   sql.NullString
		triggerID  sql.NullString
		reasonCode sql.NullString
		reason     sql.NullString
		snapshot   []byte
		finishedAt sql.NullTime
		deadline   sql.NullTime
	)
	err := s.DB.QueryRowContext(ctx, `
        SELECT id, tenant_id, rule_id, revision, definition_hash, scope_key, generation,
               device_id, trigger_event_id, status, reason_code, reason, snapshot,
               started_at, finished_at, deadline, cursor_pc, resume_count
          FROM auto_runs WHERE id=$1 AND tenant_id=$2`, runID, tenantID).
		Scan(&r.ID, &r.TenantID, &r.RuleID, &r.Revision, &r.DefinitionHash, &r.ScopeKey,
			&r.Generation, &deviceID, &triggerID, &r.Status, &reasonCode, &reason, &snapshot,
			&r.StartedAt, &finishedAt, &deadline, &r.CursorPC, &r.ResumeCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.DeviceID = deviceID.String
	r.TriggerEventID = triggerID.String
	r.ReasonCode = reasonCode.String
	r.Reason = reason.String
	r.Snapshot = snapshot
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	if deadline.Valid {
		r.Deadline = &deadline.Time
	}
	return &r, nil
}

// ListRuns returns recent runs for a rule or device.
func (s *Store) ListRuns(ctx context.Context, tenantID, ruleID, deviceID string, limit int) ([]*Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, tenant_id, rule_id, revision, definition_hash, scope_key, generation,
                 coalesce(device_id,''), coalesce(trigger_event_id,''), status,
                 coalesce(reason_code,''), coalesce(reason,''), snapshot, started_at, finished_at, deadline,
                 coalesce(causal_root,''), causal_hop, coalesce(cause_event_id,''),
                 cursor_pc, resume_count
            FROM auto_runs WHERE tenant_id=$1`
	args := []any{tenantID}
	if ruleID != "" {
		args = append(args, ruleID)
		q += fmt.Sprintf(` AND rule_id=$%d`, len(args))
	}
	if deviceID != "" {
		args = append(args, deviceID)
		q += fmt.Sprintf(` AND device_id=$%d`, len(args))
	}
	q += fmt.Sprintf(` ORDER BY started_at DESC LIMIT %d`, limit)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		var (
			r          Run
			snapshot   []byte
			finishedAt sql.NullTime
			deadline   sql.NullTime
		)
		if err := rows.Scan(&r.ID, &r.TenantID, &r.RuleID, &r.Revision, &r.DefinitionHash,
			&r.ScopeKey, &r.Generation, &r.DeviceID, &r.TriggerEventID, &r.Status,
			&r.ReasonCode, &r.Reason, &snapshot, &r.StartedAt, &finishedAt, &deadline,
			&r.CausalRoot, &r.CausalHop, &r.CauseEventID, &r.CursorPC, &r.ResumeCount); err != nil {
			return nil, err
		}
		r.Snapshot = snapshot
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		if deadline.Valid {
			r.Deadline = &deadline.Time
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Steps / targets
// ---------------------------------------------------------------------------

// Step is one persisted step of a run.
type Step struct {
	ID             string
	RunID          string
	Idx            int
	Kind           string
	Status         string
	IdempotencyKey string
	Attempt        int
	Detail         json.RawMessage
	UpdatedAt      time.Time
}

// UpsertStep records the identity of one step and returns the authoritative row
// id together with the attempt number.
//
// The id comes back from the database instead of being generated by the caller.
// That detail matters: after a restart the row already exists, and a caller that
// kept its own freshly generated id would update zero rows for the rest of the
// run — a step that never leaves "pending" is precisely the "stuck step" this
// task forbids.
//
// A re-entry bumps attempt and re-opens the row, because a second attempt at the
// same program counter is a retry of the same logical step, not a new step.
func (s *Store) UpsertStep(ctx context.Context, st *Step) (string, int, error) {
	var (
		id      string
		attempt int
	)
	err := s.DB.QueryRowContext(ctx, `
        INSERT INTO auto_steps (id, run_id, idx, kind, status, idempotency_key, attempt, detail)
        VALUES ($1,$2,$3,$4,$5,$6,0,$7)
        ON CONFLICT (run_id, idx) DO UPDATE
           SET kind=EXCLUDED.kind,
               status='pending',
               attempt=auto_steps.attempt + 1,
               updated_at=now()
        RETURNING id, attempt`,
		st.ID, st.RunID, st.Idx, st.Kind, st.Status, st.IdempotencyKey, st.Detail).Scan(&id, &attempt)
	return id, attempt, err
}

// StepStatus reads one step's current status. It exists so a late confirmation
// from the command owner can complete an undecided step without overwriting a
// step that already reached a terminal state.
func (s *Store) StepStatus(ctx context.Context, stepID string) (string, error) {
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status FROM auto_steps WHERE id=$1`, stepID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return status, err
}

// UpdateStepStatus records a step transition.
//
// A zero-row update is returned as an error rather than swallowed: it means the
// caller is holding an id that does not exist, and continuing would leave a step
// frozen in "pending" with no trace of why.
func (s *Store) UpdateStepStatus(ctx context.Context, stepID, status, detail string) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_steps SET status=$2, detail=NULLIF($3,'')::jsonb, updated_at=now()
         WHERE id=$1`, stepID, status, detail)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSteps returns the steps of a run ordered by index.
func (s *Store) ListSteps(ctx context.Context, runID string) ([]*Step, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id, run_id, idx, kind, status, idempotency_key, attempt, detail, updated_at
          FROM auto_steps WHERE run_id=$1 ORDER BY idx`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Step
	for rows.Next() {
		var st Step
		var detail []byte
		if err := rows.Scan(&st.ID, &st.RunID, &st.Idx, &st.Kind, &st.Status,
			&st.IdempotencyKey, &st.Attempt, &detail, &st.UpdatedAt); err != nil {
			return nil, err
		}
		st.Detail = detail
		out = append(out, &st)
	}
	return out, rows.Err()
}

// Target is one resolved (or excluded) device of a run snapshot.
type Target struct {
	DeviceID   string
	Status     string
	ReasonCode string
	Reason     string
	// Selected is the frozen "this device is part of the run" decision. It is
	// separate from Status because Status is overwritten with the dispatch
	// result, and a resumed step must still know which devices it owns.
	Selected bool
}

// InsertTargets writes the frozen target snapshot in one statement batch.
func (s *Store) InsertTargets(ctx context.Context, runID string, targets []Target) error {
	if len(targets) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO auto_targets (run_id, device_id, status, reason_code, reason, selected)
        VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6)
        ON CONFLICT (run_id, device_id) DO UPDATE
           SET status=EXCLUDED.status, reason_code=EXCLUDED.reason_code, reason=EXCLUDED.reason,
               selected=EXCLUDED.selected, updated_at=now()`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range targets {
		selected := t.Selected || t.Status == "selected"
		if _, err := stmt.ExecContext(ctx, runID, t.DeviceID, t.Status, t.ReasonCode, t.Reason,
			selected); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTargets returns the snapshot rows.
func (s *Store) ListTargets(ctx context.Context, runID string) ([]Target, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT device_id, status, coalesce(reason_code,''), coalesce(reason,''), selected
          FROM auto_targets WHERE run_id=$1 ORDER BY device_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.DeviceID, &t.Status, &t.ReasonCode, &t.Reason, &t.Selected); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

// OutboxItem is one durable intent.
type OutboxItem struct {
	ID             string
	TenantID       string
	RunID          string
	StepID         string
	Kind           string
	ScopeKey       string
	Generation     int64
	IdempotencyKey string
	Payload        json.RawMessage
	State          string
	Attempt        int
	AcceptedRef    string
	LastError      string
}

// EnqueueOutbox writes an intent inside the caller's transaction.
// EnqueueOutbox writes one intent inside the caller's transaction.
//
// It reports whether the row was actually inserted. `false` means an intent with
// the same idempotency key already exists: the same logical action, reached
// again. The caller must reuse that row instead of treating the duplicate as a
// fresh acceptance — that is what keeps a transport retry from being counted as a
// second action, while a genuinely new execution (a different run) still gets its
// own key and its own row.
func (s *Store) EnqueueOutbox(ctx context.Context, tx *sql.Tx, o *OutboxItem) (bool, error) {
	res, err := tx.ExecContext(ctx, `
        INSERT INTO auto_outbox (id, tenant_id, run_id, step_id, kind, scope_key, generation,
                                 idempotency_key, payload, state)
        VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7,$8,$9,'pending')
        ON CONFLICT (idempotency_key) DO NOTHING`,
		o.ID, o.TenantID, o.RunID, o.StepID, o.Kind, o.ScopeKey, o.Generation,
		o.IdempotencyKey, o.Payload)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ClaimOutbox leases up to limit due intents for dispatch.
func (s *Store) ClaimOutbox(ctx context.Context, limit int) ([]*OutboxItem, error) {
	rows, err := s.DB.QueryContext(ctx, `
        WITH due AS (
            SELECT id FROM auto_outbox
             WHERE state IN ('pending','dispatched') AND next_attempt_at <= now()
             ORDER BY next_attempt_at
             LIMIT $1
             FOR UPDATE SKIP LOCKED
        )
        UPDATE auto_outbox o SET state='dispatched', attempt=o.attempt+1,
                                 next_attempt_at=now() + interval '30 seconds',
                                 updated_at=now()
          FROM due WHERE o.id = due.id
        RETURNING o.id, o.tenant_id, coalesce(o.run_id,''), coalesce(o.step_id,''), o.kind,
                  o.scope_key, o.generation, o.idempotency_key, o.payload, o.state, o.attempt,
                  coalesce(o.accepted_ref,''), coalesce(o.last_error,'')`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboxItem
	for rows.Next() {
		var o OutboxItem
		var payload []byte
		if err := rows.Scan(&o.ID, &o.TenantID, &o.RunID, &o.StepID, &o.Kind, &o.ScopeKey,
			&o.Generation, &o.IdempotencyKey, &payload, &o.State, &o.Attempt,
			&o.AcceptedRef, &o.LastError); err != nil {
			return nil, err
		}
		o.Payload = payload
		out = append(out, &o)
	}
	return out, rows.Err()
}

// MarkOutbox records the dispatch result.
func (s *Store) MarkOutbox(ctx context.Context, id, state, acceptedRef, lastErr string, retryAt time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_outbox
           SET state=$2, accepted_ref=NULLIF($3,''), last_error=NULLIF($4,''),
               next_attempt_at=$5, updated_at=now()
         WHERE id=$1`, id, state, acceptedRef, lastErr, retryAt)
	return err
}

// OutboxForRun lists intents of a run (used by explain).
func (s *Store) OutboxForRun(ctx context.Context, runID string) ([]*OutboxItem, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id, tenant_id, coalesce(run_id,''), coalesce(step_id,''), kind, scope_key,
               generation, idempotency_key, payload, state, attempt,
               coalesce(accepted_ref,''), coalesce(last_error,'')
          FROM auto_outbox WHERE run_id=$1 ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OutboxItem
	for rows.Next() {
		var o OutboxItem
		var payload []byte
		if err := rows.Scan(&o.ID, &o.TenantID, &o.RunID, &o.StepID, &o.Kind, &o.ScopeKey,
			&o.Generation, &o.IdempotencyKey, &payload, &o.State, &o.Attempt,
			&o.AcceptedRef, &o.LastError); err != nil {
			return nil, err
		}
		o.Payload = payload
		out = append(out, &o)
	}
	return out, rows.Err()
}

// PublishEvent stores a business transition for downstream consumers. A nil tx
// means "write it on its own", which is what the read-side event feed needs.
func (s *Store) PublishEvent(ctx context.Context, tx *sql.Tx, id, tenantID, kind, subject string, payload any) (int64, error) {
	seq, err := s.NextSeq(ctx)
	if err != nil {
		return 0, err
	}
	body, _ := json.Marshal(payload)
	const q = `INSERT INTO auto_events (id, tenant_id, kind, subject, payload, seq)
	           VALUES ($1,$2,$3,$4,$5,$6)`
	if tx != nil {
		_, err = tx.ExecContext(ctx, q, id, tenantID, kind, subject, body, seq)
	} else {
		_, err = s.DB.ExecContext(ctx, q, id, tenantID, kind, subject, body, seq)
	}
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// EventRecord is one business transition in the feed.
type EventRecord struct {
	ID        string
	Kind      string
	Subject   string
	Payload   json.RawMessage
	Seq       int64
	CreatedAt time.Time
}

// EventsSince returns transitions after a sequence, for A05 consumers.
func (s *Store) EventsSince(ctx context.Context, tenantID string, after int64, limit int) ([]*EventRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id, kind, subject, payload, seq, created_at
          FROM auto_events WHERE tenant_id=$1 AND seq > $2 ORDER BY seq LIMIT $3`,
		tenantID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRecord
	for rows.Next() {
		var e EventRecord
		var payload []byte
		if err := rows.Scan(&e.ID, &e.Kind, &e.Subject, &payload, &e.Seq, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Payload = payload
		out = append(out, &e)
	}
	return out, rows.Err()
}

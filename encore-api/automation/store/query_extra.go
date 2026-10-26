package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// UnprocessedEvents returns received-but-not-evaluated receipts in receipt order.
func (s *Store) UnprocessedEvents(ctx context.Context, limit int) ([]*IngestEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `
        SELECT event_id, tenant_id, device_id, signal, key, value, coalesce(unit,''),
               quality, observed_at, seq, params,
               coalesce(input_class,''), coalesce(gateway_id,''),
               coalesce(causal_root,''), causal_hop
          FROM auto_inbox
         WHERE processed_at IS NULL
         ORDER BY seq
         LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IngestEvent
	for rows.Next() {
		var (
			e      IngestEvent
			value  []byte
			signal string
			qual   string
			params []byte
		)
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.DeviceID, &signal, &e.Key, &value,
			&e.Unit, &qual, &e.ObservedAt, &e.Seq, &params,
			&e.InputClass, &e.GatewayID, &e.CausalRoot, &e.CausalHop); err != nil {
			return nil, err
		}
		e.Signal = domainSignal(signal)
		e.Quality = domainQuality(qual)
		_ = json.Unmarshal(value, &e.Value)
		if len(params) > 0 {
			_ = json.Unmarshal(params, &e.Params)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// GetOutbox loads one intent by id.
func (s *Store) GetOutbox(ctx context.Context, id string) (*OutboxItem, error) {
	return s.getOutboxWhere(ctx, `id=$1`, id)
}

// GetOutboxByKey loads the intent that already carries a logical action
// identity. A retry of the same step finds the row it created earlier, so the
// outcome can be reused instead of accepted a second time.
func (s *Store) GetOutboxByKey(ctx context.Context, key string) (*OutboxItem, error) {
	return s.getOutboxWhere(ctx, `idempotency_key=$1`, key)
}

func (s *Store) getOutboxWhere(ctx context.Context, where string, arg any) (*OutboxItem, error) {
	var (
		o       OutboxItem
		payload []byte
	)
	err := s.DB.QueryRowContext(ctx, `
        SELECT id, tenant_id, coalesce(run_id,''), coalesce(step_id,''), kind, scope_key,
               generation, idempotency_key, payload, state, attempt,
               coalesce(accepted_ref,''), coalesce(last_error,'')
          FROM auto_outbox WHERE `+where, arg).
		Scan(&o.ID, &o.TenantID, &o.RunID, &o.StepID, &o.Kind, &o.ScopeKey, &o.Generation,
			&o.IdempotencyKey, &payload, &o.State, &o.Attempt, &o.AcceptedRef, &o.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Payload = payload
	return &o, nil
}

// GetEpisodeByID loads an episode by id, active or historical, so a detail link
// into the history keeps working after the alarm recovered.
func (s *Store) GetEpisodeByID(ctx context.Context, tenantID, id string) (*Episode, error) {
	e, err := scanEpisode(s.row(ctx, nil, `SELECT `+episodeColumns+`
          FROM alarm_episodes WHERE id=$1 AND tenant_id=$2`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// GetActiveEpisodeByID is kept for the ack path, which may only touch an active
// alarm.
func (s *Store) GetActiveEpisodeByID(ctx context.Context, tenantID, id string) (*Episode, error) {
	e, err := scanEpisode(s.row(ctx, nil, `SELECT `+episodeColumns+`
          FROM alarm_episodes WHERE id=$1 AND tenant_id=$2 AND state='active'`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// BacklogStats reports the queue depths used by readiness and performance tests.
type BacklogStats struct {
	UnprocessedEvents int
	DuePending        int
	PendingOutbox     int
	ActiveRuns        int
	OldestUnprocessed *time.Time
}

// Backlog computes current backlog.
func (s *Store) Backlog(ctx context.Context) (*BacklogStats, error) {
	var st BacklogStats
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM auto_inbox WHERE processed_at IS NULL`).
		Scan(&st.UnprocessedEvents); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM auto_pending WHERE state='pending'`).
		Scan(&st.DuePending); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM auto_outbox WHERE state IN ('pending','dispatched')`).
		Scan(&st.PendingOutbox); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM auto_runs WHERE status IN ('accepted','running','queued')`).
		Scan(&st.ActiveRuns); err != nil {
		return nil, err
	}
	var oldest sql.NullTime
	if err := s.DB.QueryRowContext(ctx, `SELECT min(received_at) FROM auto_inbox WHERE processed_at IS NULL`).
		Scan(&oldest); err != nil {
		return nil, err
	}
	if oldest.Valid {
		st.OldestUnprocessed = &oldest.Time
	}
	return &st, nil
}

// RulesMatchingDevice returns the enabled rules whose trigger names "self" and
// that are therefore evaluated for any device reporting the matching signal.
//
// The device page needs them: they are not bound, but they do apply, and a page
// that lists only bindings would tell the customer "nothing runs on this
// device" while three legacy scene automations quietly do.
func (s *Store) RulesMatchingDevice(ctx context.Context, tenantID, deviceID string) ([]*Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT DISTINCT `+ruleColumns+`
          FROM auto_rules r
         WHERE r.tenant_id = $1
           AND r.deleted_at IS NULL
           AND r.enabled
           AND EXISTS (
             SELECT 1 FROM jsonb_array_elements(r.definition->'trigger'->'groups') g,
                           jsonb_array_elements(g->'conditions') c
              WHERE (c->>'source' = 'self' OR c->>'source' IS NULL)
           )
         ORDER BY r.name
         LIMIT 200`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

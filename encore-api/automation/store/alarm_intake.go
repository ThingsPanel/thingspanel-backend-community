package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AlarmIntake is one authorised event-channel submission.
//
// The primary key (tenant, policy key, action id) is the dedupe boundary: a scene
// action that is retried after a timeout must not raise a second alarm, and it
// must not be able to change its mind by resending the same id with a different
// body. That second half is what makes the record worth keeping — a pure
// "have I seen this id" set would happily accept a rewritten payload.
type AlarmIntake struct {
	TenantID     string
	PolicyKey    string
	ActionID     string
	Source       string
	SubjectID    string
	Dimension    string
	Kind         string
	PayloadHash  string
	Outcome      string
	ReasonCode   string
	EpisodeID    string
	TransitionID string
	CreatedAt    time.Time
}

// ReserveAlarmIntake claims an action id before any lifecycle work is done.
//
// It returns the row that now owns the id and whether this call created it. A
// caller that lost the race gets the winner's row back and must replay that
// decision instead of computing a new one.
func (s *Store) ReserveAlarmIntake(ctx context.Context, in *AlarmIntake) (*AlarmIntake, bool, error) {
	res, err := s.exec(ctx, nil, `
        INSERT INTO alarm_event_intake (tenant_id, policy_key, action_id, source, subject_id,
                                        dimension, kind, payload_hash, outcome)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'in_flight')
        ON CONFLICT (tenant_id, policy_key, action_id) DO NOTHING`,
		in.TenantID, in.PolicyKey, in.ActionID, in.Source, in.SubjectID,
		in.Dimension, in.Kind, in.PayloadHash)
	if err != nil {
		return nil, false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return in, true, nil
	}
	existing, err := s.AlarmIntake(ctx, in.TenantID, in.PolicyKey, in.ActionID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

// AlarmIntake loads one intake record.
func (s *Store) AlarmIntake(ctx context.Context, tenantID, policyKey, actionID string) (*AlarmIntake, error) {
	var in AlarmIntake
	err := s.row(ctx, nil, `
        SELECT tenant_id, policy_key, action_id, source, subject_id, dimension, kind,
               payload_hash, outcome, reason_code, COALESCE(episode_id,''),
               COALESCE(transition_id,''), created_at
          FROM alarm_event_intake
         WHERE tenant_id=$1 AND policy_key=$2 AND action_id=$3`,
		tenantID, policyKey, actionID).
		Scan(&in.TenantID, &in.PolicyKey, &in.ActionID, &in.Source, &in.SubjectID, &in.Dimension,
			&in.Kind, &in.PayloadHash, &in.Outcome, &in.ReasonCode, &in.EpisodeID,
			&in.TransitionID, &in.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &in, nil
}

// FinishAlarmIntake records the decision that the action id produced, in the same
// transaction as the lifecycle change it describes.
func (s *Store) FinishAlarmIntake(ctx context.Context, tx *sql.Tx, tenantID, policyKey, actionID,
	outcome, reasonCode, episodeID, transitionID string) error {
	_, err := s.exec(ctx, tx, `
        UPDATE alarm_event_intake
           SET outcome=$4, reason_code=$5, episode_id=NULLIF($6,''),
               transition_id=NULLIF($7,'')
         WHERE tenant_id=$1 AND policy_key=$2 AND action_id=$3`,
		tenantID, policyKey, actionID, outcome, reasonCode, episodeID, transitionID)
	return err
}

// ReleaseAlarmIntake drops a reservation whose owner failed before deciding
// anything, so the caller can try again instead of being stuck behind a row that
// says nothing.
func (s *Store) ReleaseAlarmIntake(ctx context.Context, tenantID, policyKey, actionID string) error {
	_, err := s.exec(ctx, nil, `
        DELETE FROM alarm_event_intake
         WHERE tenant_id=$1 AND policy_key=$2 AND action_id=$3 AND outcome='in_flight'`,
		tenantID, policyKey, actionID)
	return err
}

// UnrelayedAlarmEvents returns published alarm transitions that have not yet been
// turned into an alarm signal for the rule engine.
//
// The relay reads the event exit, not the alarm table: the point of the outbox is
// that a transition is announced exactly once, in order, with its identity
// attached. Polling alarm_episodes instead would make "did this already fire"
// depend on comparing timestamps.
func (s *Store) UnrelayedAlarmEvents(ctx context.Context, limit int) ([]*EventRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.query(ctx, nil, `
        SELECT id, tenant_id, kind, subject, payload, seq, created_at
          FROM auto_events
         WHERE relayed_at IS NULL AND kind LIKE 'alarm.%'
         ORDER BY seq
         LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRecord
	for rows.Next() {
		var e EventRecord
		var payload []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Kind, &e.Subject, &payload, &e.Seq, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// MarkEventRelayed moves the relay cursor past one event.
func (s *Store) MarkEventRelayed(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := s.exec(ctx, tx, `UPDATE auto_events SET relayed_at=now() WHERE id=$1 AND relayed_at IS NULL`, id)
	return err
}

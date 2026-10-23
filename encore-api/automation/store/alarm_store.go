package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"encore.app/automation/domain"
)

// Episode is one active or historical alarm instance. Identity is
// (tenant, policy, subject, dimension); a continuous abnormal condition keeps
// updating the same row instead of appending a new alarm per sample.
type Episode struct {
	ID        string
	TenantID  string
	PolicyID  string
	SubjectID string
	Dimension string
	State     string
	Revision  int
	Epoch     int64
	OpenedAt  time.Time
	LastSeq   int64
	AckedAt   *time.Time
	AckedBy   string
	ClearedAt *time.Time
	UpdatedAt time.Time
}

// Transition is one lifecycle fact.
type Transition struct {
	ID        string
	EpisodeID string
	TenantID  string
	Kind      string
	Seq       int64
	Revision  int
	Epoch     int64
	Evidence  any
	CreatedAt time.Time
}

// GetActiveEpisode looks up the active instance for a subject.
func (s *Store) GetActiveEpisode(ctx context.Context, tenantID, policyID, subjectID, dimension string) (*Episode, error) {
	var (
		e         Episode
		ackedAt   sql.NullTime
		ackedBy   sql.NullString
		clearedAt sql.NullTime
	)
	err := s.DB.QueryRowContext(ctx, `
        SELECT id, tenant_id, policy_id, subject_id, dimension, state, revision, epoch,
               opened_at, last_seq, acked_at, acked_by, cleared_at, updated_at
          FROM alarm_episodes
         WHERE tenant_id=$1 AND policy_id=$2 AND subject_id=$3 AND dimension=$4 AND state='active'`,
		tenantID, policyID, subjectID, dimension).
		Scan(&e.ID, &e.TenantID, &e.PolicyID, &e.SubjectID, &e.Dimension, &e.State,
			&e.Revision, &e.Epoch, &e.OpenedAt, &e.LastSeq, &ackedAt, &ackedBy, &clearedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ackedAt.Valid {
		e.AckedAt = &ackedAt.Time
	}
	e.AckedBy = ackedBy.String
	if clearedAt.Valid {
		e.ClearedAt = &clearedAt.Time
	}
	return &e, nil
}

// OpenEpisode creates the active instance. The partial unique index makes a
// concurrent duplicate fail instead of creating two active alarms.
func (s *Store) OpenEpisode(ctx context.Context, e *Episode) error {
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO alarm_episodes (id, tenant_id, policy_id, subject_id, dimension, state,
                                    revision, epoch, opened_at, last_seq)
        VALUES ($1,$2,$3,$4,$5,'active',$6,$7,$8,$9)`,
		e.ID, e.TenantID, e.PolicyID, e.SubjectID, e.Dimension, e.Revision, e.Epoch,
		e.OpenedAt, e.LastSeq)
	return err
}

// TouchEpisode advances the sequence of an active episode.
func (s *Store) TouchEpisode(ctx context.Context, episodeID string, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE alarm_episodes SET last_seq=$2, updated_at=now() WHERE id=$1`, episodeID, seq)
	return err
}

// ClearEpisode closes an episode and returns whether it actually transitioned.
func (s *Store) ClearEpisode(ctx context.Context, episodeID string, seq int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE alarm_episodes SET state='cleared', cleared_at=now(), last_seq=$2, updated_at=now()
         WHERE id=$1 AND state='active'`, episodeID, seq)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AckEpisode acknowledges an active episode. Acknowledging is not recovery: the
// state stays active.
func (s *Store) AckEpisode(ctx context.Context, tenantID, episodeID, actor string, seq int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE alarm_episodes SET acked_at=now(), acked_by=$3, last_seq=$4, updated_at=now()
         WHERE id=$1 AND tenant_id=$2 AND state='active'`, episodeID, tenantID, actor, seq)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AddTransition writes a lifecycle fact. The unique (episode, kind, seq) index
// makes a replayed transition a no-op instead of a duplicate notification.
func (s *Store) AddTransition(ctx context.Context, tx *sql.Tx, t *Transition) (bool, error) {
	evidence, _ := json.Marshal(t.Evidence)
	var err error
	if tx != nil {
		_, err = tx.ExecContext(ctx, `
            INSERT INTO alarm_transitions (id, episode_id, tenant_id, kind, seq, revision, epoch, evidence)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
            ON CONFLICT (episode_id, kind, seq) DO NOTHING`,
			t.ID, t.EpisodeID, t.TenantID, t.Kind, t.Seq, t.Revision, t.Epoch, evidence)
	} else {
		_, err = s.DB.ExecContext(ctx, `
            INSERT INTO alarm_transitions (id, episode_id, tenant_id, kind, seq, revision, epoch, evidence)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
            ON CONFLICT (episode_id, kind, seq) DO NOTHING`,
			t.ID, t.EpisodeID, t.TenantID, t.Kind, t.Seq, t.Revision, t.Epoch, evidence)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListEpisodes returns episodes for a policy or subject.
func (s *Store) ListEpisodes(ctx context.Context, tenantID, policyID, subjectID, state string, limit int) ([]*Episode, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, tenant_id, policy_id, subject_id, dimension, state, revision, epoch,
                 opened_at, last_seq, acked_at, acked_by, cleared_at, updated_at
            FROM alarm_episodes WHERE tenant_id=$1`
	args := []any{tenantID}
	if policyID != "" {
		args = append(args, policyID)
		q += ` AND policy_id=$` + itoa(len(args))
	}
	if subjectID != "" {
		args = append(args, subjectID)
		q += ` AND subject_id=$` + itoa(len(args))
	}
	if state != "" {
		args = append(args, state)
		q += ` AND state=$` + itoa(len(args))
	}
	q += ` ORDER BY opened_at DESC LIMIT ` + itoa(limit)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Episode
	for rows.Next() {
		var (
			e         Episode
			ackedAt   sql.NullTime
			ackedBy   sql.NullString
			clearedAt sql.NullTime
		)
		if err := rows.Scan(&e.ID, &e.TenantID, &e.PolicyID, &e.SubjectID, &e.Dimension,
			&e.State, &e.Revision, &e.Epoch, &e.OpenedAt, &e.LastSeq, &ackedAt, &ackedBy,
			&clearedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if ackedAt.Valid {
			e.AckedAt = &ackedAt.Time
		}
		e.AckedBy = ackedBy.String
		if clearedAt.Valid {
			e.ClearedAt = &clearedAt.Time
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ListTransitions returns the lifecycle history of an episode.
func (s *Store) ListTransitions(ctx context.Context, episodeID string) ([]*Transition, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id, episode_id, tenant_id, kind, seq, revision, epoch, evidence, created_at
          FROM alarm_transitions WHERE episode_id=$1 ORDER BY seq, created_at`, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Transition
	for rows.Next() {
		var t Transition
		var evidence []byte
		if err := rows.Scan(&t.ID, &t.EpisodeID, &t.TenantID, &t.Kind, &t.Seq,
			&t.Revision, &t.Epoch, &evidence, &t.CreatedAt); err != nil {
			return nil, err
		}
		if len(evidence) > 0 {
			_ = json.Unmarshal(evidence, &t.Evidence)
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ListActiveEpisodes returns the active episodes of every tenant, oldest first.
//
// A worker serves all tenants, so this scan is deliberately not tenant scoped;
// the caller filters by policy definition and by subject.
func (s *Store) ListActiveEpisodes(ctx context.Context, limit int) ([]*Episode, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id, tenant_id, policy_id, subject_id, dimension, state, revision, epoch,
               opened_at, last_seq, acked_at, acked_by, cleared_at, updated_at
          FROM alarm_episodes
         WHERE state='active'
         ORDER BY opened_at
         LIMIT `+strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Episode
	for rows.Next() {
		var (
			e         Episode
			ackedAt   sql.NullTime
			ackedBy   sql.NullString
			clearedAt sql.NullTime
		)
		if err := rows.Scan(&e.ID, &e.TenantID, &e.PolicyID, &e.SubjectID, &e.Dimension,
			&e.State, &e.Revision, &e.Epoch, &e.OpenedAt, &e.LastSeq,
			&ackedAt, &ackedBy, &clearedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if ackedAt.Valid {
			t := ackedAt.Time
			e.AckedAt = &t
		}
		e.AckedBy = ackedBy.String
		if clearedAt.Valid {
			t := clearedAt.Time
			e.ClearedAt = &t
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// LastTransition returns the most recent lifecycle fact of an episode, which is
// what makes a repeated "no data" note idempotent: the sweep only adds a note
// when the last fact is something else.
func (s *Store) LastTransition(ctx context.Context, episodeID string) (*Transition, error) {
	var tr Transition
	var evidence []byte
	err := s.DB.QueryRowContext(ctx, `
        SELECT id, episode_id, tenant_id, kind, seq, revision, epoch, evidence, created_at
          FROM alarm_transitions WHERE episode_id=$1
         ORDER BY seq DESC, created_at DESC LIMIT 1`, episodeID).
		Scan(&tr.ID, &tr.EpisodeID, &tr.TenantID, &tr.Kind, &tr.Seq, &tr.Revision,
			&tr.Epoch, &evidence, &tr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(evidence, &tr.Evidence)
	return &tr, nil
}

// LatestObservedAt returns the newest observation time for one signal across a
// set of keys. It answers "when did this device last say anything about the
// fields this rule watches", which is what a no-data decision needs.
func (s *Store) LatestObservedAt(ctx context.Context, deviceID string, signal domain.SignalKind, keys []string) (time.Time, bool, error) {
	if len(keys) == 0 {
		return time.Time{}, false, nil
	}
	var latest sql.NullTime
	err := s.DB.QueryRowContext(ctx, `
        SELECT max(observed_at) FROM auto_state
         WHERE device_id=$1 AND signal=$2 AND key = ANY($3)`,
		deviceID, string(signal), keys).Scan(&latest)
	if err != nil {
		return time.Time{}, false, err
	}
	if !latest.Valid {
		return time.Time{}, false, nil
	}
	return latest.Time, true, nil
}

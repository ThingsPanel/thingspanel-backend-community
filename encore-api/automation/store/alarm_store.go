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
	ID       string
	TenantID string
	PolicyID string
	// PolicyKey is the stable business key. The active-alarm identity is
	// (tenant, policy_key, subject, dimension): a re-created policy keeps the
	// same alarm instead of opening a second one for the same physical problem.
	PolicyKey string
	SubjectID string
	Dimension string
	State     string
	Level     string
	Source    string
	Revision  int
	Epoch     int64
	OpenedAt  time.Time
	FirstSeq  int64
	LastSeq   int64
	// SampleCount is the bounded update for a repeated abnormal sample: it grows,
	// the row does not. Without it, "the condition still holds" would either be
	// invisible or be appended as a new lifecycle fact on every uplink.
	SampleCount  int64
	LastEvidence json.RawMessage
	AckedAt      *time.Time
	AckedBy      string
	ClearedAt    *time.Time
	UpdatedAt    time.Time
}

// episodeColumns is the single column list every episode query uses, so adding a
// field cannot silently leave one read path behind.
const episodeColumns = `id, tenant_id, policy_id, policy_key, subject_id, dimension, state,
       level, source, revision, epoch, opened_at, first_seq, last_seq, sample_count,
       last_evidence, acked_at, acked_by, cleared_at, updated_at`

// scanEpisode reads one row in episodeColumns order.
func scanEpisode(row interface{ Scan(...any) error }) (*Episode, error) {
	var (
		e         Episode
		level     sql.NullString
		source    sql.NullString
		evidence  []byte
		ackedAt   sql.NullTime
		ackedBy   sql.NullString
		clearedAt sql.NullTime
	)
	if err := row.Scan(&e.ID, &e.TenantID, &e.PolicyID, &e.PolicyKey, &e.SubjectID, &e.Dimension,
		&e.State, &level, &source, &e.Revision, &e.Epoch, &e.OpenedAt, &e.FirstSeq, &e.LastSeq,
		&e.SampleCount, &evidence, &ackedAt, &ackedBy, &clearedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.Level = level.String
	e.Source = source.String
	if len(evidence) > 0 {
		e.LastEvidence = evidence
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
	return &e, nil
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
//
// policyKey is the policy's stable business key (see Rule.PolicyKey), not the
// rule id: the identity has to survive the policy being re-created.
func (s *Store) GetActiveEpisode(ctx context.Context, tenantID, policyKey, subjectID, dimension string) (*Episode, error) {
	return s.getActiveEpisode(ctx, nil, tenantID, policyKey, subjectID, dimension)
}

func (s *Store) getActiveEpisode(ctx context.Context, tx *sql.Tx, tenantID, policyKey, subjectID, dimension string) (*Episode, error) {
	row := s.row(ctx, tx, `SELECT `+episodeColumns+`
          FROM alarm_episodes
         WHERE tenant_id=$1 AND policy_key=$2 AND subject_id=$3 AND dimension=$4 AND state='active'`,
		tenantID, policyKey, subjectID, dimension)
	e, err := scanEpisode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// OpenEpisode creates the active instance. The partial unique index makes a
// concurrent duplicate fail instead of creating two active alarms.
func (s *Store) OpenEpisode(ctx context.Context, e *Episode) error {
	return s.openEpisode(ctx, nil, e)
}

func (s *Store) openEpisode(ctx context.Context, tx *sql.Tx, e *Episode) error {
	_, err := s.exec(ctx, tx, `
        INSERT INTO alarm_episodes (id, tenant_id, policy_id, policy_key, subject_id, dimension,
                                    state, level, source, revision, epoch, opened_at,
                                    first_seq, last_seq, sample_count)
        VALUES ($1,$2,$3,$4,$5,$6,'active',$7,$8,$9,$10,$11,$12,$13,1)`,
		e.ID, e.TenantID, e.PolicyID, e.PolicyKey, e.SubjectID, e.Dimension,
		e.Level, e.Source, e.Revision, e.Epoch, e.OpenedAt, e.FirstSeq, e.LastSeq)
	return err
}

// TouchEpisode advances the sequence of an active episode.
func (s *Store) TouchEpisode(ctx context.Context, episodeID string, seq int64) error {
	_, err := s.exec(ctx, nil, `
        UPDATE alarm_episodes SET last_seq=GREATEST(last_seq,$2), updated_at=now() WHERE id=$1`,
		episodeID, seq)
	return err
}

// RefreshEpisode applies one repeated abnormal sample.
//
// It is deliberately a bounded update rather than a new lifecycle fact: the
// condition has not changed, only the evidence has. last_seq is raised with
// GREATEST so an out-of-order sample cannot rewind the watermark, and the
// returned bool says whether the sample was accepted at all.
func (s *Store) RefreshEpisode(ctx context.Context, tx *sql.Tx, episodeID string, seq int64, evidence []byte) (bool, error) {
	res, err := s.exec(ctx, tx, `
        UPDATE alarm_episodes
           SET last_seq = GREATEST(last_seq, $2),
               sample_count = sample_count + 1,
               last_evidence = COALESCE($3, last_evidence),
               updated_at = now()
         WHERE id=$1 AND last_seq < $2`, episodeID, seq, evidence)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AdvanceEpisodeRevision records that a newer policy revision still sees the same
// abnormality. The episode keeps its identity and its opened_at; only the
// revision it is judged by moves forward. Publishing a new version therefore does
// not clear the alarm and does not open a second one.
func (s *Store) AdvanceEpisodeRevision(ctx context.Context, tx *sql.Tx, episodeID string, revision int, seq int64, evidence []byte) (bool, error) {
	res, err := s.exec(ctx, tx, `
        UPDATE alarm_episodes
           SET revision = $2, epoch = $2, last_seq = GREATEST(last_seq, $3),
               last_evidence = COALESCE($4, last_evidence), updated_at = now()
         WHERE id=$1 AND revision < $2`, episodeID, revision, seq, evidence)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Clear outcomes. A clear is refused, not silently dropped, when the caller is
// out of order or is an executor from a superseded revision: "the alarm stayed
// active" and "the alarm was cleared by a stale worker" must never look alike.
const (
	ClearApplied     = "cleared"
	ClearAlreadyDone = "already_cleared"
	ClearOutOfOrder  = "out_of_order"
	ClearStaleRev    = "stale_revision"
	ClearNotActive   = "not_active"
)

// ClearEpisode closes an episode and reports what actually happened.
func (s *Store) ClearEpisode(ctx context.Context, tx *sql.Tx, episodeID string, seq int64, revision int) (string, error) {
	var (
		lastSeq, epRevision int64
	)
	err := s.row(ctx, tx, `SELECT last_seq, revision FROM alarm_episodes WHERE id=$1 FOR UPDATE`, episodeID).
		Scan(&lastSeq, &epRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return ClearNotActive, nil
	}
	if err != nil {
		return "", err
	}
	if seq <= lastSeq {
		return ClearOutOfOrder, nil
	}
	if int64(revision) < epRevision {
		return ClearStaleRev, nil
	}
	res, err := s.exec(ctx, tx, `
        UPDATE alarm_episodes SET state='cleared', cleared_at=now(), last_seq=$2, updated_at=now()
         WHERE id=$1 AND state='active'`, episodeID, seq)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ClearAlreadyDone, nil
	}
	return ClearApplied, nil
}

// AckEpisode acknowledges an active episode. Acknowledging is not recovery: the
// state stays active.
func (s *Store) AckEpisode(ctx context.Context, tenantID, episodeID, actor string, seq int64) (bool, error) {
	res, err := s.exec(ctx, nil, `
        UPDATE alarm_episodes SET acked_at=now(), acked_by=$3,
               last_seq=GREATEST(last_seq,$4), updated_at=now()
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
	q := `SELECT ` + episodeColumns + ` FROM alarm_episodes WHERE tenant_id=$1`
	args := []any{tenantID}
	if policyID != "" {
		args = append(args, policyID)
		q += ` AND (policy_id=$` + itoa(len(args)) + ` OR policy_key=$` + itoa(len(args)) + `)`
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

	rows, err := s.query(ctx, nil, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Episode
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
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
	rows, err := s.query(ctx, nil, `SELECT `+episodeColumns+`
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
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
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

// GetActiveEpisodeTx is the transactional form used by the lifecycle decision:
// the lookup, the episode change and the published transition must see the same
// snapshot.
func (s *Store) GetActiveEpisodeTx(ctx context.Context, tx *sql.Tx, tenantID, policyKey, subjectID, dimension string) (*Episode, error) {
	return s.getActiveEpisode(ctx, tx, tenantID, policyKey, subjectID, dimension)
}

// OpenEpisodeTx is the transactional form of OpenEpisode.
func (s *Store) OpenEpisodeTx(ctx context.Context, tx *sql.Tx, e *Episode) error {
	return s.openEpisode(ctx, tx, e)
}

// AlarmStats is the reconciliation contract with the legacy alarm tables.
//
// This service cannot read the legacy database, so it publishes what it believes
// instead of guessing what the other side has: the migration tool diffs the two
// and reports the difference. Silent agreement is not something we can claim, so
// we do not.
type AlarmStats struct {
	ActiveEpisodes int64
	TotalEpisodes  int64
	Transitions    int64
	ByPolicy       map[string]int64
	ByState        map[string]int64
	ByLevel        map[string]int64
}

// AlarmStatsOf counts episodes and transitions for one tenant.
func (s *Store) AlarmStatsOf(ctx context.Context, tenantID string) (*AlarmStats, error) {
	out := &AlarmStats{
		ByPolicy: map[string]int64{}, ByState: map[string]int64{}, ByLevel: map[string]int64{},
	}
	if err := s.row(ctx, nil, `
        SELECT count(*) FILTER (WHERE state='active'), count(*)
          FROM alarm_episodes WHERE tenant_id=$1`, tenantID).
		Scan(&out.ActiveEpisodes, &out.TotalEpisodes); err != nil {
		return nil, err
	}
	if err := s.row(ctx, nil, `
        SELECT count(*) FROM alarm_transitions WHERE tenant_id=$1`, tenantID).
		Scan(&out.Transitions); err != nil {
		return nil, err
	}
	for _, spec := range []struct {
		col  string
		into map[string]int64
	}{
		{"policy_key", out.ByPolicy},
		{"state", out.ByState},
		{"level", out.ByLevel},
	} {
		rows, err := s.query(ctx, nil, `
            SELECT COALESCE(NULLIF(`+spec.col+`,''),'(none)'), count(*)
              FROM alarm_episodes WHERE tenant_id=$1 GROUP BY 1`, tenantID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			var n int64
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return nil, err
			}
			spec.into[k] = n
		}
		rows.Close()
	}
	return out, nil
}

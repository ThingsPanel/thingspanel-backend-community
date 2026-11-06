package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// EpisodeQuery is the paginated read model; it does not change alarm lifecycle.
type EpisodeQuery struct {
	SubjectID, State, Level string
	Start, End              time.Time
	Limit, Offset           int
}

type EpisodePage struct {
	Episodes             []*Episode
	Total, ActiveDevices int64
}

// QueryEpisodes reads the page and counters from the same tenant-scoped snapshot.
func (s *Store) QueryEpisodes(ctx context.Context, tenantID string, f EpisodeQuery) (*EpisodePage, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := " FROM alarm_episodes WHERE tenant_id=$1"
	args := []any{tenantID}
	add := func(column, op string, value any) {
		args = append(args, value)
		where += " AND " + column + op + "$" + strconv.Itoa(len(args))
	}
	if f.SubjectID != "" {
		add("subject_id", "=", f.SubjectID)
	}
	if f.State != "" {
		add("state", "=", f.State)
	}
	if f.Level != "" {
		add("level", "=", f.Level)
	}
	if !f.Start.IsZero() {
		add("opened_at", ">=", f.Start)
	}
	if !f.End.IsZero() {
		add("opened_at", "<=", f.End)
	}
	out := &EpisodePage{Episodes: []*Episode{}}
	err = tx.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT CASE WHEN state='active' THEN NULLIF(subject_id,'') END)`+where, args...).Scan(&out.Total, &out.ActiveDevices)
	if err != nil {
		return nil, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT "+episodeColumns+where+" ORDER BY opened_at DESC, id DESC LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		ep, scanErr := scanEpisode(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		out.Episodes = append(out.Episodes, ep)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

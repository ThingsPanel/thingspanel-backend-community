package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ActiveAlarmCounts stays inside the alarm owner and groups only requested subjects.
func (s *Store) ActiveAlarmCounts(ctx context.Context, tenant string, ids []string) (map[string]int64, error) {
	if tenant == "" || len(ids) == 0 || len(ids) > 200 {
		return nil, errors.New("invalid alarm summary batch")
	}
	args := []any{tenant}
	marks := make([]string, len(ids))
	out := map[string]int64{}
	for i, id := range ids {
		if id == "" || len(id) > 128 {
			return nil, errors.New("invalid alarm subject")
		}
		args = append(args, id)
		marks[i] = fmt.Sprintf("$%d", i+2)
		out[id] = 0
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT subject_id,count(*) FROM alarm_episodes WHERE tenant_id=$1 AND state='active' AND subject_id IN ("+strings.Join(marks, ",")+") GROUP BY subject_id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int64
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		out[id] = count
	}
	return out, rows.Err()
}

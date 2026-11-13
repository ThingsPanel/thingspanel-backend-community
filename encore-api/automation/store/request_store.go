package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// LookupRequest returns the response a previous request with this key produced.
//
// The key is scoped by tenant: a key that is merely a hash of the arguments
// would otherwise be shared between two customers who happened to type the same
// thing, and the second one would receive the first one's rule.
func (s *Store) LookupRequest(ctx context.Context, tenantID, key string) (json.RawMessage, bool, error) {
	if key == "" {
		return nil, false, nil
	}
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `
        SELECT response FROM auto_requests WHERE tenant_id=$1 AND request_key=$2`,
		tenantID, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// SaveRequest remembers the response for a key. A concurrent duplicate loses
// the insert and keeps its own result; both callers see a success either way,
// and the row that survives is the first one that finished.
func (s *Store) SaveRequest(ctx context.Context, tenantID, key, endpoint string, response []byte) error {
	if key == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
        INSERT INTO auto_requests (tenant_id, request_key, endpoint, response)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (tenant_id, request_key) DO NOTHING`,
		tenantID, key, endpoint, response)
	return err
}

// PruneRequests drops dedupe rows older than the retention window. It runs from
// the same sweeper as the timer tick; the table is not an audit log.
func (s *Store) PruneRequests(ctx context.Context, keepHours int) (int64, error) {
	if keepHours <= 0 {
		keepHours = 24
	}
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM auto_requests WHERE created_at < now() - ($1 || ' hours')::interval`,
		keepHours)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

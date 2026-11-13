package devicemetadata

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// resolveNativeInitialGroupsTx resolves only the requested/default group IDs.
// The caller owns the transaction and remains responsible for membership
// inserts, group-version changes, audit, and commit/rollback.
func resolveNativeInitialGroupsTx(ctx context.Context, tx *sqldb.Tx, tenantID string, requested NativeDeviceField[[]string], canWriteGroups bool) ([]string, error) {
	if requested.Null {
		return nil, invalidGroupQuery("initialGroupIds must be an array when supplied")
	}
	if requested.Present && len(requested.Value) == 0 {
		return []string{}, nil
	}
	if !requested.Present && !canWriteGroups {
		return []string{}, nil
	}
	if requested.Present && !canWriteGroups {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "group membership write permission is required"}
	}

	canonical := make([]string, 0, len(requested.Value))
	if requested.Present {
		if len(requested.Value) > 100 {
			return nil, invalidGroupQuery("initialGroupIds contains more than 100 items")
		}
		seen := make(map[string]struct{}, len(requested.Value))
		for _, rawID := range requested.Value {
			id, err := nativeReadUUID(rawID)
			if err != nil {
				return nil, invalidGroupQuery("initialGroupIds contains an invalid UUID")
			}
			if _, exists := seen[id]; exists {
				return nil, invalidGroupQuery("initialGroupIds must not contain duplicate UUIDs")
			}
			seen[id] = struct{}{}
			canonical = append(canonical, id)
		}
		if len(canonical) == 0 {
			return []string{}, nil
		}
	}
	sort.Strings(canonical)
	if err := validateNativeInitialGroupsSQLScope(tx, tenantID); err != nil {
		return nil, err
	}

	// Match lockDeviceGroupTree exactly: same JSON keys/values and hash seed.
	lockKey, err := json.Marshal(struct {
		Tenant string `json:"tenant"`
		Scope  string `json:"scope"`
	}{tenantID, "device-group-tree"})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to scope device group tree"}
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(lockKey)); err != nil {
		return nil, wrapDBErr(err, "lock device group tree failed")
	}

	if !requested.Present {
		rows, err := tx.Query(ctx, `SELECT id::text FROM device_groups WHERE tenant_id=$1 AND parent_id IS NULL AND deleted_at IS NULL ORDER BY id LIMIT 2 FOR UPDATE`, tenantID)
		if err != nil {
			return nil, wrapDBErr(err, "resolve initial device group failed")
		}
		defer rows.Close()
		roots := make([]string, 0, 2)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, wrapDBErr(err, "resolve initial device group failed")
			}
			roots = append(roots, id)
		}
		if err := rows.Err(); err != nil {
			return nil, wrapDBErr(err, "resolve initial device group failed")
		}
		rows.Close()
		if len(roots) == 1 {
			return roots, nil
		}
		return []string{}, nil
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM device_groups
		WHERE tenant_id=$1 AND deleted_at IS NULL
		  AND id=ANY(string_to_array($2, ',')::uuid[])
		ORDER BY id
		FOR UPDATE
	`, tenantID, strings.Join(canonical, ","))
	if err != nil {
		return nil, wrapDBErr(err, "resolve initial device groups failed")
	}
	defer rows.Close()
	locked := make(map[string]struct{}, len(canonical))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrapDBErr(err, "resolve initial device groups failed")
		}
		locked[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "resolve initial device groups failed")
	}
	rows.Close()
	for _, id := range canonical {
		if _, ok := locked[id]; !ok {
			return nil, &errs.Error{Code: errs.NotFound, Message: "one or more device groups were not found"}
		}
	}
	return canonical, nil
}

func validateNativeInitialGroupsSQLScope(tx *sqldb.Tx, tenantID string) error {
	parsed, err := uuid.Parse(tenantID)
	if err != nil || parsed == uuid.Nil || parsed.String() != tenantID {
		return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated tenant context"}
	}
	if tx == nil {
		return &errs.Error{Code: errs.Internal, Message: "device group transaction is unavailable"}
	}
	return nil
}

package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const moveDeviceGroupOperation = "moveDeviceGroup"

type MoveDeviceGroupReq struct {
	ParentID                *string
	ExpectedResourceVersion int64
}

type moveDeviceGroupBody struct {
	ParentID                json.RawMessage `json:"parentId"`
	ExpectedResourceVersion json.RawMessage `json:"expectedResourceVersion"`
}

type moveDeviceGroupBodySyntaxError struct{ err error }

func (e *moveDeviceGroupBodySyntaxError) Error() string { return e.err.Error() }
func (e *moveDeviceGroupBodySyntaxError) Unwrap() error { return e.err }

func decodeMoveDeviceGroupBody(body []byte) (*MoveDeviceGroupReq, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] != '{' {
		return nil, &moveDeviceGroupBodySyntaxError{invalidGroupQuery("request body must be a valid MoveDeviceGroup object")}
	}
	var input moveDeviceGroupBody
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, &moveDeviceGroupBodySyntaxError{invalidGroupQuery("request body must be a valid MoveDeviceGroup object")}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, &moveDeviceGroupBodySyntaxError{invalidGroupQuery("request body must contain one JSON object")}
	}
	if len(input.ParentID) == 0 || len(input.ExpectedResourceVersion) == 0 {
		return nil, invalidGroupQuery("parentId and expectedResourceVersion are required")
	}
	out := &MoveDeviceGroupReq{}
	if string(input.ParentID) != "null" {
		var value string
		if err := json.Unmarshal(input.ParentID, &value); err != nil {
			return nil, invalidGroupQuery("parentId must be a UUID or null")
		}
		id, err := normalizeDeviceGroupID(value, false)
		if err != nil {
			return nil, invalidGroupQuery("parentId must be a UUID or null")
		}
		out.ParentID = &id
	}
	if err := json.Unmarshal(input.ExpectedResourceVersion, &out.ExpectedResourceVersion); err != nil || out.ExpectedResourceVersion < 1 || out.ExpectedResourceVersion > 9007199254740991 {
		return nil, invalidGroupQuery("expectedResourceVersion must be a positive safe integer")
	}
	return out, nil
}

func canonicalMoveDeviceGroupBody(req *MoveDeviceGroupReq) ([]byte, error) {
	if req == nil {
		return nil, invalidGroupQuery("request body is required")
	}
	return json.Marshal(struct {
		ParentID                *string `json:"parentId"`
		ExpectedResourceVersion int64   `json:"expectedResourceVersion"`
	}{req.ParentID, req.ExpectedResourceVersion})
}

type moveDeviceGroupResult struct {
	status    int
	response  []byte
	requestID string
	replayed  bool
}

type groupTreeRow struct {
	parent  *string
	name    string
	deleted bool
}

func moveDeviceGroupIdempotently(ctx context.Context, principal groupWritePrincipal, groupID, key string, req *MoveDeviceGroupReq, requestID string) (*moveDeviceGroupResult, error) {
	if !validIdempotencyKey(key) {
		return nil, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters")
	}
	groupID, err := normalizeDeviceGroupID(groupID, false)
	if err != nil {
		return nil, err
	}
	if req == nil || req.ExpectedResourceVersion < 1 {
		return nil, invalidGroupQuery("expectedResourceVersion must be a positive integer")
	}
	var parent *string
	if req.ParentID != nil {
		id, err := normalizeDeviceGroupID(*req.ParentID, false)
		if err != nil {
			return nil, err
		}
		parent = &id
	}
	canonicalPath := "/api/v2/device-groups/" + groupID + "/moves"
	body, err := canonicalMoveDeviceGroupBody(&MoveDeviceGroupReq{ParentID: parent, ExpectedResourceVersion: req.ExpectedResourceVersion})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to canonicalize request"}
	}
	requestHashBytes := sha256.Sum256(append([]byte("POST\n"+canonicalPath+"\n"), body...))
	requestHash := hex.EncodeToString(requestHashBytes[:])
	tx, err := beginTenantGroupTx(ctx, principal.tenantID, nil)
	if err != nil {
		return nil, wrapDBErr(err, "move device group failed")
	}
	defer tx.Rollback()
	keyScope, _ := json.Marshal(struct {
		Tenant, Subject, Operation, Path, Key string
	}{principal.tenantID, principal.subjectID, moveDeviceGroupOperation, canonicalPath, key})
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(keyScope)); err != nil {
		return nil, wrapDBErr(err, "move device group failed")
	}
	if err := lockDeviceGroupTree(ctx, tx, principal.tenantID); err != nil {
		return nil, err
	}

	var storedHash, state string
	var storedStatus sql.NullInt64
	var responseHash sql.NullString
	var storedResponse []byte
	var replayUntil time.Time
	err = tx.QueryRowContext(ctx, `SELECT request_hash,state,http_status,response,response_hash,replay_until
		FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5 FOR UPDATE`,
		principal.tenantID, principal.subjectID, moveDeviceGroupOperation, canonicalPath, key,
	).Scan(&storedHash, &state, &storedStatus, &storedResponse, &responseHash, &replayUntil)
	if err == nil {
		if !replayUntil.After(time.Now().UTC()) || state == "expired" {
			if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,updated_at=NOW() WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, moveDeviceGroupOperation, canonicalPath, key); err != nil {
				return nil, wrapDBErr(err, "expire idempotency receipt failed")
			}
			if err := tx.Commit(); err != nil {
				return nil, wrapDBErr(err, "expire idempotency receipt failed")
			}
			return nil, idempotencyConflict("idempotency key has expired")
		}
		if storedHash != requestHash {
			return nil, idempotencyConflict("idempotency key was already used for a different request")
		}
		if state != "completed" || !storedStatus.Valid || storedResponse == nil {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "idempotency request is still being processed"}
		}
		var stored DeviceGroupResponse
		if err := json.Unmarshal(storedResponse, &stored); err != nil {
			return nil, wrapDBErr(err, "decode stored move response failed")
		}
		canonicalResponse, err := json.Marshal(stored)
		if err != nil || !responseHash.Valid || responseHash.String != fmt.Sprintf("%x", sha256.Sum256(canonicalResponse)) {
			return nil, &errs.Error{Code: errs.Internal, Message: "stored idempotency response is invalid"}
		}
		if err := tx.Commit(); err != nil {
			return nil, wrapDBErr(err, "move device group failed")
		}
		return &moveDeviceGroupResult{status: int(storedStatus.Int64), response: storedResponse, requestID: stored.Meta.RequestID, replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqldb.ErrNoRows) {
		return nil, wrapDBErr(err, "read idempotency receipt failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_idempotency (tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,state,replay_until) VALUES ($1,$2,$3,$4,$5,$6,'in_progress',NOW()+INTERVAL '24 hours')`, principal.tenantID, principal.subjectID, moveDeviceGroupOperation, canonicalPath, key, requestHash); err != nil {
		return nil, wrapDBErr(err, "create idempotency receipt failed")
	}

	before, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if before.ResourceVersion != req.ExpectedResourceVersion {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before moving"}
	}
	beforeVersion := before.ResourceVersion
	changed := before.ParentID == nil && parent != nil || before.ParentID != nil && parent == nil || before.ParentID != nil && parent != nil && *before.ParentID != *parent
	if changed {
		rows, err := tx.QueryContext(ctx, `SELECT id::text,parent_id::text,name,deleted_at IS NOT NULL FROM device_groups WHERE tenant_id=$1`, principal.tenantID)
		if err != nil {
			return nil, wrapDBErr(err, "read device group tree failed")
		}
		groups := make(map[string]groupTreeRow)
		for rows.Next() {
			var id string
			var parentID sql.NullString
			var name string
			var deleted bool
			if err := rows.Scan(&id, &parentID, &name, &deleted); err != nil {
				rows.Close()
				return nil, wrapDBErr(err, "read device group tree failed")
			}
			var p *string
			if parentID.Valid {
				v := parentID.String
				p = &v
			}
			groups[id] = groupTreeRow{parent: p, name: name, deleted: deleted}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, wrapDBErr(err, "read device group tree failed")
		}
		rows.Close()
		if parent != nil {
			p, ok := groups[*parent]
			if !ok || p.deleted {
				return nil, &errs.Error{Code: errs.NotFound, Message: "parent device group not found"}
			}
		}
		if parent != nil && *parent == groupID {
			return nil, idempotencyConflict("a device group cannot be its own parent")
		}
		subtreeDepth, err := movedSubtreeDepth(groups, groupID)
		if err != nil {
			return nil, err
		}
		parentDepth := 0
		if parent != nil {
			seen := map[string]bool{}
			cursor := parent
			for cursor != nil {
				id := *cursor
				if seen[id] {
					return nil, idempotencyConflict("device group tree contains a cycle")
				}
				seen[id] = true
				if id == groupID {
					return nil, idempotencyConflict("a device group cannot be moved under its descendant")
				}
				row, ok := groups[id]
				if !ok || row.deleted {
					return nil, &errs.Error{Code: errs.NotFound, Message: "parent device group not found"}
				}
				parentDepth++
				cursor = row.parent
				if parentDepth > maxDeviceGroupDepth {
					return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group tree exceeds the maximum depth"}
				}
			}
		}
		if parentDepth+1+subtreeDepth > maxDeviceGroupDepth {
			return nil, invalidGroupQuery("move would exceed the maximum device group depth of 32")
		}
		for id, row := range groups {
			if id != groupID && !row.deleted && row.name == before.Name && sameOptionalID(row.parent, parent) {
				return nil, &errs.Error{Code: errs.AlreadyExists, Message: "a device group with this name already exists under the selected parent"}
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE device_groups SET parent_id=$3,updated_by=$4,updated_at=NOW(),resource_version=resource_version+1 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND resource_version=$5`, principal.tenantID, groupID, parent, principal.actorID, beforeVersion)
		if err != nil {
			if isGroupNameUniqueViolation(err) {
				return nil, &errs.Error{Code: errs.AlreadyExists, Message: "a device group with this name already exists under the selected parent"}
			}
			return nil, wrapDBErr(err, "move device group failed")
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before moving"}
		}
	}
	after, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	responseJSON, err := json.Marshal(DeviceGroupResponse{Data: after, Meta: RequestMeta{RequestID: requestID}})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to encode response"}
	}
	responseDigest := sha256.Sum256(responseJSON)
	if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='completed',http_status=200,response=$6::jsonb,response_hash=$7,updated_at=NOW() WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, moveDeviceGroupOperation, canonicalPath, key, responseJSON, hex.EncodeToString(responseDigest[:])); err != nil {
		return nil, wrapDBErr(err, "complete idempotency receipt failed")
	}
	changes, _ := json.Marshal(struct {
		ParentChanged bool `json:"parentChanged"`
	}{changed})
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_audit_events (id,tenant_id,created_by,operation_id,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes) VALUES ($1,$2,$3,NULL,$4,$5,'device_group',$6,$7,$8,'succeeded',$9::jsonb)`, uuid.NewString(), principal.tenantID, principal.actorID, requestID, moveDeviceGroupOperation, groupID, beforeVersion, after.ResourceVersion, changes); err != nil {
		return nil, wrapDBErr(err, "audit device group move failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "move device group failed")
	}
	return &moveDeviceGroupResult{status: http.StatusOK, response: responseJSON, requestID: requestID}, nil
}

func movedSubtreeDepth(groups map[string]groupTreeRow, rootID string) (int, error) {
	children := make(map[string][]string)
	for id, row := range groups {
		if row.parent != nil && !row.deleted {
			children[*row.parent] = append(children[*row.parent], id)
		}
	}
	maxDepth := 0
	active := map[string]bool{}
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if active[id] {
			return idempotencyConflict("device group tree contains a cycle")
		}
		active[id] = true
		if depth > maxDepth {
			maxDepth = depth
		}
		if depth > maxDeviceGroupDepth {
			return invalidGroupQuery("device group subtree exceeds the maximum depth of 32")
		}
		for _, child := range children[id] {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		delete(active, id)
		return nil
	}
	if err := visit(rootID, 0); err != nil {
		return 0, err
	}
	return maxDepth, nil
}

func sameOptionalID(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

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
	"sort"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const (
	addDeviceGroupMembersOperation    = "addDeviceGroupMembers"
	removeDeviceGroupMembersOperation = "removeDeviceGroupMembers"
	maxDeviceGroupMembersPerRequest   = 100
)

type SetGroupMembersReq struct {
	DeviceIDs            []string `json:"deviceIds"`
	ExpectedGroupVersion int64    `json:"expectedGroupVersion"`
}

type setGroupMembersBody struct {
	DeviceIDs            json.RawMessage `json:"deviceIds"`
	ExpectedGroupVersion json.RawMessage `json:"expectedGroupVersion"`
}

type setGroupMembersBodySyntaxError struct{ err error }

func (e *setGroupMembersBodySyntaxError) Error() string { return e.err.Error() }
func (e *setGroupMembersBodySyntaxError) Unwrap() error { return e.err }

type setGroupMembersResult struct {
	status    int
	response  []byte
	requestID string
}

func decodeSetGroupMembersBody(body []byte) (*SetGroupMembersReq, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] != '{' {
		return nil, &setGroupMembersBodySyntaxError{invalidGroupQuery("request body must be a valid SetGroupMembers object")}
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var input setGroupMembersBody
	if err := decoder.Decode(&input); err != nil {
		return nil, &setGroupMembersBodySyntaxError{invalidGroupQuery("request body must be a valid SetGroupMembers object")}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, &setGroupMembersBodySyntaxError{invalidGroupQuery("request body must contain one JSON object")}
	}
	if len(input.DeviceIDs) == 0 || len(input.ExpectedGroupVersion) == 0 {
		return nil, invalidGroupQuery("deviceIds and expectedGroupVersion are required")
	}
	var ids []string
	if err := json.Unmarshal(input.DeviceIDs, &ids); err != nil || len(ids) == 0 || len(ids) > maxDeviceGroupMembersPerRequest {
		return nil, invalidGroupQuery("deviceIds must contain 1 to 100 UUIDs")
	}
	seen := make(map[string]struct{}, len(ids))
	for i, raw := range ids {
		id, err := normalizeDeviceGroupID(raw, false)
		if err != nil {
			return nil, invalidGroupQuery("deviceIds must contain unique UUIDs")
		}
		if _, ok := seen[id]; ok {
			return nil, invalidGroupQuery("deviceIds must contain unique UUIDs")
		}
		seen[id] = struct{}{}
		ids[i] = id
	}
	var version int64
	if err := json.Unmarshal(input.ExpectedGroupVersion, &version); err != nil || version < 1 || version > 9007199254740991 {
		return nil, invalidGroupQuery("expectedGroupVersion must be a positive safe integer")
	}
	sort.Strings(ids)
	return &SetGroupMembersReq{DeviceIDs: ids, ExpectedGroupVersion: version}, nil
}

func canonicalSetGroupMembersBody(req *SetGroupMembersReq) ([]byte, error) {
	if req == nil || len(req.DeviceIDs) == 0 || len(req.DeviceIDs) > maxDeviceGroupMembersPerRequest || req.ExpectedGroupVersion < 1 {
		return nil, invalidGroupQuery("invalid SetGroupMembers request")
	}
	ids := append([]string(nil), req.DeviceIDs...)
	sort.Strings(ids)
	return json.Marshal(struct {
		DeviceIDs            []string `json:"deviceIds"`
		ExpectedGroupVersion int64    `json:"expectedGroupVersion"`
	}{ids, req.ExpectedGroupVersion})
}

func setDeviceGroupMembersIdempotently(ctx context.Context, principal groupWritePrincipal, groupID, key string, req *SetGroupMembersReq, operation, requestID string) (*setGroupMembersResult, error) {
	if operation != addDeviceGroupMembersOperation && operation != removeDeviceGroupMembersOperation {
		return nil, &errs.Error{Code: errs.Internal, Message: "unsupported device group membership operation"}
	}
	if !validIdempotencyKey(key) {
		return nil, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters")
	}
	groupID, err := normalizeDeviceGroupID(groupID, false)
	if err != nil {
		return nil, err
	}
	if _, err := canonicalSetGroupMembersBody(req); err != nil {
		return nil, err
	}
	ids := append([]string(nil), req.DeviceIDs...)
	for i, raw := range ids {
		id, err := normalizeDeviceGroupID(raw, false)
		if err != nil {
			return nil, invalidGroupQuery("deviceIds must contain unique UUIDs")
		}
		ids[i] = id
	}
	sort.Strings(ids)
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return nil, invalidGroupQuery("deviceIds must contain unique UUIDs")
		}
	}
	canonicalBody, err := json.Marshal(struct {
		DeviceIDs            []string `json:"deviceIds"`
		ExpectedGroupVersion int64    `json:"expectedGroupVersion"`
	}{ids, req.ExpectedGroupVersion})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to canonicalize request"}
	}
	pathSuffix := "/member-additions"
	if operation == removeDeviceGroupMembersOperation {
		pathSuffix = "/member-removals"
	}
	canonicalPath := "/api/v2/device-groups/" + groupID + pathSuffix
	requestHashBytes := sha256.Sum256(append([]byte("POST\n"+canonicalPath+"\n"), canonicalBody...))
	requestHash := hex.EncodeToString(requestHashBytes[:])
	tx, err := beginTenantGroupTx(ctx, principal.tenantID, nil)
	if err != nil {
		return nil, wrapDBErr(err, "update device group members failed")
	}
	defer tx.Rollback()
	keyScope, _ := json.Marshal(struct{ Tenant, Subject, Operation, Path, Key string }{principal.tenantID, principal.subjectID, operation, canonicalPath, key})
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(keyScope)); err != nil {
		return nil, wrapDBErr(err, "update device group members failed")
	}
	// Membership changes use the same lock as create/update/move so group
	// version checks and hierarchy mutations have one tenant-wide order.
	if err := lockDeviceGroupTree(ctx, tx, principal.tenantID); err != nil {
		return nil, err
	}
	var storedHash, state string
	var storedStatus sql.NullInt64
	var storedResponseHash sql.NullString
	var storedResponse []byte
	var replayUntil time.Time
	err = tx.QueryRowContext(ctx, `SELECT request_hash,state,http_status,response,response_hash,replay_until
		FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5 FOR UPDATE`,
		principal.tenantID, principal.subjectID, operation, canonicalPath, key,
	).Scan(&storedHash, &state, &storedStatus, &storedResponse, &storedResponseHash, &replayUntil)
	if err == nil {
		if !replayUntil.After(time.Now().UTC()) || state == "expired" {
			if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,updated_at=NOW()
				WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, operation, canonicalPath, key); err != nil {
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
			return nil, wrapDBErr(fmt.Errorf("decode idempotency response: %w", err), "update device group members failed")
		}
		canonicalResponse, err := json.Marshal(stored)
		if err != nil || !storedResponseHash.Valid || storedResponseHash.String != fmt.Sprintf("%x", sha256.Sum256(canonicalResponse)) {
			return nil, &errs.Error{Code: errs.Internal, Message: "stored idempotency response is invalid"}
		}
		if err := tx.Commit(); err != nil {
			return nil, wrapDBErr(err, "update device group members failed")
		}
		return &setGroupMembersResult{status: int(storedStatus.Int64), response: canonicalResponse, requestID: stored.Meta.RequestID}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqldb.ErrNoRows) {
		return nil, wrapDBErr(err, "read idempotency receipt failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_idempotency
		(tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,state,replay_until)
		VALUES ($1,$2,$3,$4,$5,$6,'in_progress',NOW()+INTERVAL '24 hours')`,
		principal.tenantID, principal.subjectID, operation, canonicalPath, key, requestHash); err != nil {
		return nil, wrapDBErr(err, "create idempotency receipt failed")
	}
	group, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if group.ResourceVersion != req.ExpectedGroupVersion {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before changing members"}
	}
	// Lock and validate every target before changing any relation. Additions
	// require a live device; removals may unlink an existing relation to a
	// same-tenant tombstone so cleanup is not blocked by device deletion.
	for _, deviceID := range ids {
		var foundID string
		query := `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`
		if operation == removeDeviceGroupMembersOperation {
			query = `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 FOR UPDATE`
		}
		err := tx.QueryRowContext(ctx, query, principal.tenantID, deviceID).Scan(&foundID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
		if err != nil {
			return nil, wrapDBErr(err, "validate device group member failed")
		}
	}
	changedIDs := make([]string, 0, len(ids))
	if operation == addDeviceGroupMembersOperation {
		for _, deviceID := range ids {
			result, err := tx.ExecContext(ctx, `INSERT INTO device_group_members (tenant_id,group_id,device_id,created_by)
				VALUES ($1,$2,$3,$4) ON CONFLICT (tenant_id,group_id,device_id) DO NOTHING`, principal.tenantID, groupID, deviceID, principal.actorID)
			if err != nil {
				return nil, wrapDBErr(err, "add device group members failed")
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return nil, wrapDBErr(err, "add device group members failed")
			}
			if rows > 0 {
				changedIDs = append(changedIDs, deviceID)
			}
		}
	} else {
		// Resolve the actual relation set while the group/tree and target rows
		// are locked. This operation unlinks memberships only; it never deletes
		// the device or group and does not revoke access to either resource.
		for _, deviceID := range ids {
			var existingID string
			err := tx.QueryRowContext(ctx, `SELECT device_id::text FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3 FOR UPDATE`, principal.tenantID, groupID, deviceID).Scan(&existingID)
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, wrapDBErr(err, "read device group member relation failed")
			}
			changedIDs = append(changedIDs, existingID)
		}
		if len(changedIDs) > 0 {
			for _, deviceID := range changedIDs {
				result, err := tx.ExecContext(ctx, `DELETE FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3`, principal.tenantID, groupID, deviceID)
				if err != nil {
					return nil, wrapDBErr(err, "remove device group members failed")
				}
				rows, err := result.RowsAffected()
				if err != nil {
					return nil, wrapDBErr(err, "remove device group members failed")
				}
				if rows != 1 {
					return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group membership changed; fetch it again before removing members"}
				}
			}
		}
	}
	beforeVersion := group.ResourceVersion
	if len(changedIDs) > 0 {
		result, err := tx.ExecContext(ctx, `UPDATE device_groups SET resource_version=resource_version+1,updated_by=$4,updated_at=NOW()
			WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND resource_version=$3`, principal.tenantID, groupID, req.ExpectedGroupVersion, principal.actorID)
		if err != nil {
			return nil, wrapDBErr(err, "update device group version failed")
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, wrapDBErr(err, "update device group version failed")
		}
		if rows != 1 {
			return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before changing members"}
		}
	}
	after, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	response, err := json.Marshal(DeviceGroupResponse{Data: after, Meta: RequestMeta{RequestID: requestID}})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to encode response"}
	}
	responseHash := sha256.Sum256(response)
	if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='completed',http_status=200,response=$6::jsonb,response_hash=$7,updated_at=NOW()
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, operation, canonicalPath, key, response, hex.EncodeToString(responseHash[:])); err != nil {
		return nil, wrapDBErr(err, "complete idempotency receipt failed")
	}
	var changes map[string][]string
	if operation == addDeviceGroupMembersOperation {
		changes = map[string][]string{"addedDeviceIds": changedIDs}
	} else {
		changes = map[string][]string{"removedDeviceIds": changedIDs}
	}
	redactedChanges, _ := json.Marshal(changes)
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_audit_events
		(id,tenant_id,created_by,operation_id,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
		VALUES ($1,$2,$3,NULL,$4,$5,'device_group',$6,$7,$8,'succeeded',$9::jsonb)`, uuid.NewString(), principal.tenantID, principal.actorID, requestID, operation, groupID, beforeVersion, after.ResourceVersion, redactedChanges); err != nil {
		return nil, wrapDBErr(err, "audit device group member update failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "update device group members failed")
	}
	return &setGroupMembersResult{status: 200, response: response, requestID: requestID}, nil
}

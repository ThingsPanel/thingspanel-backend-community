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
	"time"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const deleteDeviceGroupOperation = "deleteDeviceGroup"

type deleteDeviceGroupResult struct {
	status    int
	response  []byte
	requestID string
	etag      string
	replayed  bool
}

func deleteDeviceGroupIdempotently(ctx context.Context, principal groupWritePrincipal, groupID, key, ifMatch, requestID string) (*deleteDeviceGroupResult, error) {
	if !validIdempotencyKey(key) {
		return nil, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters")
	}
	if !validDeviceGroupIfMatch(ifMatch) {
		return nil, invalidGroupQuery("If-Match must be a strong device-group ETag")
	}
	groupID, err := normalizeDeviceGroupID(groupID, false)
	if err != nil {
		return nil, err
	}
	canonicalPath := createGroupLocation(groupID)
	requestHash := sha256.Sum256([]byte("DELETE\n" + canonicalPath + "\n" + ifMatch))
	hash := hex.EncodeToString(requestHash[:])
	tx, err := beginTenantGroupTx(ctx, principal.tenantID, nil)
	if err != nil {
		return nil, wrapDBErr(err, "delete device group failed")
	}
	defer tx.Rollback()
	lockKey, err := json.Marshal(struct {
		Tenant, Subject, Operation, Path, Key string
	}{principal.tenantID, principal.subjectID, deleteDeviceGroupOperation, canonicalPath, key})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to scope idempotency request"}
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(lockKey)); err != nil {
		return nil, wrapDBErr(err, "delete device group failed")
	}
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
		principal.tenantID, principal.subjectID, deleteDeviceGroupOperation, canonicalPath, key,
	).Scan(&storedHash, &state, &storedStatus, &storedResponse, &storedResponseHash, &replayUntil)
	if err == nil {
		if !replayUntil.After(time.Now().UTC()) || state == "expired" {
			if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,updated_at=NOW()
				WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, deleteDeviceGroupOperation, canonicalPath, key); err != nil {
				return nil, wrapDBErr(err, "expire idempotency receipt failed")
			}
			if err := tx.Commit(); err != nil {
				return nil, wrapDBErr(err, "expire idempotency receipt failed")
			}
			return nil, idempotencyConflict("idempotency key has expired")
		}
		if storedHash != hash {
			return nil, idempotencyConflict("idempotency key was already used for a different request")
		}
		if state != "completed" || !storedStatus.Valid || storedResponse == nil {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "idempotency request is still being processed"}
		}
		var stored DeviceGroupResponse
		if err := json.Unmarshal(storedResponse, &stored); err != nil {
			return nil, wrapDBErr(fmt.Errorf("decode idempotency response: %w", err), "delete device group failed")
		}
		canonical, err := json.Marshal(stored)
		if err != nil || !storedResponseHash.Valid || storedResponseHash.String != fmt.Sprintf("%x", sha256.Sum256(canonical)) {
			return nil, &errs.Error{Code: errs.Internal, Message: "stored idempotency response is invalid"}
		}
		if err := tx.Commit(); err != nil {
			return nil, wrapDBErr(err, "delete device group failed")
		}
		return &deleteDeviceGroupResult{status: int(storedStatus.Int64), response: canonical, requestID: stored.Meta.RequestID, etag: groupReadETag(stored.Data), replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqldb.ErrNoRows) {
		return nil, wrapDBErr(err, "read idempotency receipt failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_idempotency
		(tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,state,replay_until)
		VALUES ($1,$2,$3,$4,$5,$6,'in_progress',NOW()+INTERVAL '24 hours')`,
		principal.tenantID, principal.subjectID, deleteDeviceGroupOperation, canonicalPath, key, hash); err != nil {
		return nil, wrapDBErr(err, "create idempotency receipt failed")
	}
	before, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if groupReadETag(before) != ifMatch {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before deleting"}
	}
	var hasChildren, hasMembers bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM device_groups WHERE tenant_id=$1 AND parent_id=$2 AND deleted_at IS NULL),
		EXISTS (SELECT 1 FROM device_group_members WHERE tenant_id=$1 AND group_id=$2)`, principal.tenantID, groupID).Scan(&hasChildren, &hasMembers); err != nil {
		return nil, wrapDBErr(err, "check device group references failed")
	}
	if hasChildren || hasMembers {
		return nil, &errs.Error{Code: errs.AlreadyExists, Message: "device group has child groups or members; remove them before deleting"}
	}
	var updatedAt time.Time
	err = tx.QueryRowContext(ctx, `UPDATE device_groups SET deleted_at=NOW(),updated_at=NOW(),updated_by=$3,resource_version=resource_version+1
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND resource_version=$4
		RETURNING updated_at`, principal.tenantID, groupID, principal.actorID, before.ResourceVersion).Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before deleting"}
	}
	if err != nil {
		return nil, wrapDBErr(err, "delete device group failed")
	}
	after := before
	after.ResourceVersion++
	after.UpdatedBy = principal.actorID
	after.UpdatedAt = updatedAt
	after.DirectDeviceCount = 0
	after.DescendantDeviceCount = 0
	after.SubtreeDeviceCount = 0
	response := DeviceGroupResponse{Data: after, Meta: RequestMeta{RequestID: requestID}}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to encode response"}
	}
	responseHash := sha256.Sum256(responseJSON)
	if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='completed',http_status=200,response=$6::jsonb,response_hash=$7,updated_at=NOW()
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, deleteDeviceGroupOperation, canonicalPath, key, responseJSON, hex.EncodeToString(responseHash[:])); err != nil {
		return nil, wrapDBErr(err, "complete idempotency receipt failed")
	}
	changes, _ := json.Marshal(struct {
		Deleted bool `json:"deleted"`
	}{Deleted: true})
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_audit_events
		(id,tenant_id,created_by,operation_id,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
		VALUES ($1,$2,$3,NULL,$4,$5,'device_group',$6,$7,$8,'succeeded',$9::jsonb)`,
		uuid.NewString(), principal.tenantID, principal.actorID, requestID, deleteDeviceGroupOperation, groupID, before.ResourceVersion, after.ResourceVersion, changes); err != nil {
		return nil, wrapDBErr(err, "audit device group delete failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "delete device group failed")
	}
	return &deleteDeviceGroupResult{status: http.StatusOK, response: responseJSON, requestID: requestID, etag: groupReadETag(after)}, nil
}

//encore:api auth raw method=DELETE path=/api/v2/device-groups/:groupID
func (s *Service) DeleteDeviceGroup(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	principal, err := groupPrincipalForWrite()
	if err == nil {
		data := authn.CurrentData()
		if data == nil || !authz.Can(data, "group", "delete") {
			err = &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to delete device groups"}
		}
	}
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, 0)
		return
	}
	if _, err := parseGroupReadQuery(r); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	var bodyByte [1]byte
	bodyCount, bodyErr := http.MaxBytesReader(w, r.Body, 1).Read(bodyByte[:])
	if bodyErr != nil && !errors.Is(bodyErr, io.EOF) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("DELETE request body must be empty"), http.StatusBadRequest)
		return
	} else if bodyCount != 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("DELETE request body must be empty"), http.StatusBadRequest)
		return
	}
	ifMatchValues := r.Header.Values("If-Match")
	if len(ifMatchValues) == 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match is required"}, http.StatusPreconditionRequired)
		return
	}
	if len(ifMatchValues) != 1 || !validDeviceGroupIfMatch(ifMatchValues[0]) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("If-Match must contain one strong device-group ETag"), http.StatusBadRequest)
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	key := r.Header.Get("Idempotency-Key")
	if len(keys) != 1 || !validIdempotencyKey(key) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters"), http.StatusBadRequest)
		return
	}
	groupID, err := normalizeDeviceGroupID(encore.CurrentRequest().PathParams.Get("groupID"), false)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := deleteDeviceGroupIdempotently(r.Context(), principal, groupID, key, ifMatchValues[0], requestID)
	if err != nil {
		status := 0
		switch errs.Code(err) {
		case errs.AlreadyExists:
			status = http.StatusConflict
		case errs.FailedPrecondition:
			status = http.StatusPreconditionFailed
		case errs.InvalidArgument:
			status = http.StatusUnprocessableEntity
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", result.requestID)
	w.Header().Set("ETag", result.etag)
	w.WriteHeader(result.status)
	_, _ = w.Write(result.response)
}

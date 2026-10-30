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
	"strings"
	"time"
	"unicode/utf8"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

const updateDeviceGroupOperation = "updateDeviceGroup"

type UpdateDeviceGroupReq struct {
	Name           *string
	Description    json.RawMessage
	DescriptionSet bool
	SortOrder      *int
}

type updateDeviceGroupResult struct {
	status    int
	response  []byte
	requestID string
	etag      string
	replayed  bool
}

type updateDeviceGroupBody struct {
	Name        json.RawMessage `json:"name"`
	Description json.RawMessage `json:"description"`
	SortOrder   json.RawMessage `json:"sortOrder"`
}

type updateDeviceGroupBodySyntaxError struct{ err error }

func (e *updateDeviceGroupBodySyntaxError) Error() string { return e.err.Error() }
func (e *updateDeviceGroupBodySyntaxError) Unwrap() error { return e.err }

func invalidUpdateDeviceGroupBodyShape(message string) error {
	return &updateDeviceGroupBodySyntaxError{err: invalidGroupQuery(message)}
}

func decodeUpdateDeviceGroupBody(body []byte) (*UpdateDeviceGroupReq, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] != '{' {
		return nil, invalidUpdateDeviceGroupBodyShape("request body must be a valid UpdateDeviceGroup object")
	}
	var input updateDeviceGroupBody
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, invalidUpdateDeviceGroupBodyShape("request body must be a valid UpdateDeviceGroup object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, invalidUpdateDeviceGroupBodyShape("request body must contain one JSON object")
	}

	out := &UpdateDeviceGroupReq{}
	if len(input.Name) != 0 {
		var name string
		if string(input.Name) == "null" || json.Unmarshal(input.Name, &name) != nil || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 {
			return nil, invalidGroupQuery("name must contain 1 to 128 characters")
		}
		out.Name = &name
	}
	if len(input.Description) != 0 {
		out.DescriptionSet = true
		if string(input.Description) == "null" {
			out.Description = json.RawMessage("null")
		} else {
			var description string
			if err := json.Unmarshal(input.Description, &description); err != nil || utf8.RuneCountInString(description) > 2000 {
				return nil, invalidGroupQuery("description must be a string of at most 2000 characters or null")
			}
			canonical, _ := json.Marshal(description)
			out.Description = canonical
		}
	}
	if len(input.SortOrder) != 0 {
		var sortOrder int64
		if err := json.Unmarshal(input.SortOrder, &sortOrder); err != nil || sortOrder < 0 || sortOrder > 2147483647 {
			return nil, invalidGroupQuery("sortOrder must be an integer between 0 and 2147483647")
		}
		value := int(sortOrder)
		out.SortOrder = &value
	}
	return out, nil
}

func canonicalUpdateDeviceGroupBody(req *UpdateDeviceGroupReq) ([]byte, error) {
	if req == nil {
		return nil, invalidGroupQuery("request body is required")
	}
	var description json.RawMessage
	if req.DescriptionSet {
		description = req.Description
	}
	return json.Marshal(struct {
		Name        *string         `json:"name,omitempty"`
		Description json.RawMessage `json:"description,omitempty"`
		SortOrder   *int            `json:"sortOrder,omitempty"`
	}{Name: req.Name, Description: description, SortOrder: req.SortOrder})
}

func validDeviceGroupIfMatch(value string) bool {
	if len(value) != len(`"sha256-`)+64+1 || !strings.HasPrefix(value, `"sha256-`) || !strings.HasSuffix(value, `"`) {
		return false
	}
	_, err := hex.DecodeString(value[len(`"sha256-`) : len(value)-1])
	return err == nil
}

func updateDeviceGroupIdempotently(ctx context.Context, principal groupWritePrincipal, groupID, key, ifMatch string, req *UpdateDeviceGroupReq, requestID string) (*updateDeviceGroupResult, error) {
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
	body, err := canonicalUpdateDeviceGroupBody(req)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to canonicalize request"}
	}
	canonicalPath := createGroupLocation(groupID)
	requestHashBytes := sha256.Sum256(append([]byte("PATCH\n"+canonicalPath+"\n"+ifMatch+"\n"), body...))
	requestHash := hex.EncodeToString(requestHashBytes[:])
	tx, err := beginTenantGroupTx(ctx, principal.tenantID, nil)
	if err != nil {
		return nil, wrapDBErr(err, "update device group failed")
	}
	defer tx.Rollback()

	lockKey, err := json.Marshal(struct {
		Tenant    string `json:"tenant"`
		Subject   string `json:"subject"`
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Key       string `json:"key"`
	}{principal.tenantID, principal.subjectID, updateDeviceGroupOperation, canonicalPath, key})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to scope idempotency request"}
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(lockKey)); err != nil {
		return nil, wrapDBErr(err, "update device group failed")
	}
	// Serialize hierarchy mutations with create and move operations. Renames
	// also affect the strong representation ETags of descendants.
	if err := lockDeviceGroupTree(ctx, tx, principal.tenantID); err != nil {
		return nil, err
	}

	var storedHash, state string
	var storedStatus sql.NullInt64
	var storedResponseHash sql.NullString
	var storedResponse []byte
	var replayUntil time.Time
	err = tx.QueryRowContext(ctx, `SELECT request_hash,state,http_status,response,response_hash,replay_until
		FROM metadata_idempotency
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5
		FOR UPDATE`, principal.tenantID, principal.subjectID, updateDeviceGroupOperation, canonicalPath, key,
	).Scan(&storedHash, &state, &storedStatus, &storedResponse, &storedResponseHash, &replayUntil)
	if err == nil {
		if !replayUntil.After(time.Now().UTC()) || state == "expired" {
			if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,updated_at=NOW()
				WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, updateDeviceGroupOperation, canonicalPath, key); err != nil {
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
			return nil, wrapDBErr(fmt.Errorf("decode idempotency response: %w", err), "update device group failed")
		}
		canonicalResponse, err := json.Marshal(stored)
		if err != nil || !storedResponseHash.Valid || storedResponseHash.String != fmt.Sprintf("%x", sha256.Sum256(canonicalResponse)) {
			return nil, &errs.Error{Code: errs.Internal, Message: "stored idempotency response is invalid"}
		}
		if err := tx.Commit(); err != nil {
			return nil, wrapDBErr(err, "update device group failed")
		}
		return &updateDeviceGroupResult{status: int(storedStatus.Int64), response: storedResponse, requestID: stored.Meta.RequestID, etag: groupReadETag(stored.Data), replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqldb.ErrNoRows) {
		return nil, wrapDBErr(err, "update device group failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_idempotency
		(tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,state,replay_until)
		VALUES ($1,$2,$3,$4,$5,$6,'in_progress',NOW()+INTERVAL '24 hours')`,
		principal.tenantID, principal.subjectID, updateDeviceGroupOperation, canonicalPath, key, requestHash); err != nil {
		return nil, wrapDBErr(err, "create idempotency receipt failed")
	}

	before, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if groupReadETag(before) != ifMatch {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before updating"}
	}
	beforeVersion := before.ResourceVersion
	changedFields := make([]string, 0, 3)
	name := before.Name
	if req.Name != nil {
		name = *req.Name
		if name != before.Name {
			changedFields = append(changedFields, "name")
		}
	}
	description := before.Description
	if req.DescriptionSet {
		if string(req.Description) == "null" {
			description = nil
		} else {
			var value string
			if err := json.Unmarshal(req.Description, &value); err != nil {
				return nil, &errs.Error{Code: errs.Internal, Message: "normalized description is invalid"}
			}
			description = &value
		}
		if !equalOptionalString(description, before.Description) {
			changedFields = append(changedFields, "description")
		}
	}
	sortOrder := before.SortOrder
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
		if sortOrder != before.SortOrder {
			changedFields = append(changedFields, "sortOrder")
		}
	}
	if len(changedFields) > 0 {
		result, err := tx.ExecContext(ctx, `UPDATE device_groups
			SET name=$3,description=$4,sort_order=$5,updated_by=$6,updated_at=NOW(),resource_version=resource_version+1
			WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND resource_version=$7`,
			principal.tenantID, groupID, name, description, sortOrder, principal.actorID, before.ResourceVersion)
		if err != nil {
			if isGroupNameUniqueViolation(err) {
				return nil, &errs.Error{Code: errs.AlreadyExists, Message: "a device group with this name already exists under the selected parent"}
			}
			return nil, wrapDBErr(err, "update device group failed")
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, wrapDBErr(err, "update device group failed")
		}
		if rows != 1 {
			return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device group has changed; fetch it again before updating"}
		}
	}
	after, err := getDeviceGroupForUpdateTx(ctx, tx, principal.tenantID, groupID)
	if err != nil {
		return nil, err
	}
	response := DeviceGroupResponse{Data: after, Meta: RequestMeta{RequestID: requestID}}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to encode response"}
	}
	responseHash := sha256.Sum256(responseJSON)
	if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency
		SET state='completed',http_status=200,response=$6::jsonb,response_hash=$7,updated_at=NOW()
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`,
		principal.tenantID, principal.subjectID, updateDeviceGroupOperation, canonicalPath, key, responseJSON, hex.EncodeToString(responseHash[:])); err != nil {
		return nil, wrapDBErr(err, "complete idempotency receipt failed")
	}
	changes, _ := json.Marshal(struct {
		Changed bool     `json:"changed"`
		Fields  []string `json:"fields"`
	}{Changed: len(changedFields) > 0, Fields: changedFields})
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_audit_events
		(id,tenant_id,created_by,operation_id,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
		VALUES ($1,$2,$3,NULL,$4,$5,'device_group',$6,$7,$8,'succeeded',$9::jsonb)`,
		uuid.NewString(), principal.tenantID, principal.actorID, requestID, updateDeviceGroupOperation, after.ID, beforeVersion, after.ResourceVersion, changes); err != nil {
		return nil, wrapDBErr(err, "audit device group update failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "update device group failed")
	}
	return &updateDeviceGroupResult{status: 200, response: responseJSON, requestID: requestID, etag: groupReadETag(after)}, nil
}

func getDeviceGroupForUpdateTx(ctx context.Context, tx *sql.Tx, tenantID, groupID string) (DeviceGroup, error) {
	var group DeviceGroup
	err := tx.QueryRowContext(ctx, `SELECT id,tenant_id,parent_id,name,description,sort_order,resource_version,created_by,updated_by,created_at,updated_at,0::int
		FROM device_groups WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenantID, groupID).Scan(
		&group.ID, &group.TenantID, &group.ParentID, &group.Name, &group.Description, &group.SortOrder, &group.ResourceVersion,
		&group.CreatedBy, &group.UpdatedBy, &group.CreatedAt, &group.UpdatedAt, &group.DirectDeviceCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
			return DeviceGroup{}, &errs.Error{Code: errs.NotFound, Message: "device group not found"}
		}
		return DeviceGroup{}, wrapDBErr(err, "read device group for update failed")
	}
	groups := []DeviceGroup{group}
	if err := hydrateGroupReadMetadata(ctx, tenantID, groups, tx); err != nil {
		return DeviceGroup{}, err
	}
	return groups[0], nil
}

func equalOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

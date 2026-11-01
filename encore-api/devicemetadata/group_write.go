package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	createDeviceGroupOperation = "createDeviceGroup"
	createDeviceGroupPath      = "/api/v2/device-groups"
	metadataReplayWindow       = 24 * time.Hour
)

type CreateDeviceGroupReq struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	ParentID    *string `json:"parentId"`
	SortOrder   int     `json:"sortOrder"`
}

type groupWritePrincipal struct {
	tenantID  string
	subjectID string
	actorID   string
}

func groupPrincipalForWrite() (groupWritePrincipal, error) {
	data := authn.CurrentData()
	if data == nil {
		return groupWritePrincipal{}, &errs.Error{Code: errs.Unauthenticated, Message: "missing authenticated principal"}
	}
	tenantID, err := requiredTenantID(data)
	if err != nil {
		return groupWritePrincipal{}, err
	}
	subjectID := strings.TrimSpace(data.UserID)
	if subjectID == "" || len(subjectID) > 128 {
		return groupWritePrincipal{}, &errs.Error{Code: errs.Unauthenticated, Message: "missing authenticated subject"}
	}
	if !authz.Can(data, "group", "write") {
		return groupWritePrincipal{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to write device groups"}
	}
	return groupWritePrincipal{
		tenantID: normalizePrincipalID(tenantID), subjectID: subjectID, actorID: subjectID,
	}, nil
}

func normalizeCreateDeviceGroup(req *CreateDeviceGroupReq) (*CreateDeviceGroupReq, error) {
	if req == nil {
		return nil, invalidGroupQuery("request body is required")
	}
	out := *req
	if strings.TrimSpace(out.Name) == "" || utf8.RuneCountInString(out.Name) > 128 {
		return nil, invalidGroupQuery("name must contain 1 to 128 characters")
	}
	if out.Description != nil && utf8.RuneCountInString(*out.Description) > 2000 {
		return nil, invalidGroupQuery("description must contain at most 2000 characters")
	}
	if out.SortOrder < 0 {
		return nil, invalidGroupQuery("sortOrder must be non-negative")
	}
	if out.ParentID != nil {
		parentID, err := normalizeDeviceGroupID(*out.ParentID, false)
		if err != nil {
			return nil, err
		}
		out.ParentID = &parentID
	}
	return &out, nil
}

func validIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

func groupCreateRequestHash(req *CreateDeviceGroupReq) (string, error) {
	canonical, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("POST\n"+createDeviceGroupPath+"\n"), canonical...))
	return hex.EncodeToString(sum[:]), nil
}

type groupCreateResult struct {
	status    int
	response  []byte
	location  string
	requestID string
	replayed  bool
}

func createDeviceGroupIdempotently(ctx context.Context, principal groupWritePrincipal, key string, req *CreateDeviceGroupReq, requestID string) (*groupCreateResult, error) {
	if !validIdempotencyKey(key) {
		return nil, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters")
	}
	normalized, err := normalizeCreateDeviceGroup(req)
	if err != nil {
		return nil, err
	}
	requestHash, err := groupCreateRequestHash(normalized)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to canonicalize request"}
	}
	tx, err := beginTenantGroupTx(ctx, principal.tenantID, nil)
	if err != nil {
		return nil, wrapDBErr(err, "create device group failed")
	}
	defer tx.Rollback()

	lockKey, err := json.Marshal(struct {
		Tenant    string `json:"tenant"`
		Subject   string `json:"subject"`
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Key       string `json:"key"`
	}{principal.tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, key})
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to scope idempotency request"}
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(lockKey)); err != nil {
		return nil, wrapDBErr(err, "create device group failed")
	}
	if err := lockDeviceGroupTree(ctx, tx, principal.tenantID); err != nil {
		return nil, err
	}

	var storedHash, state string
	var storedStatus sql.NullInt64
	var storedResponseHash sql.NullString
	var storedResponse []byte
	var replayUntil time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT request_hash,state,http_status,response,response_hash,replay_until
		FROM metadata_idempotency
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5
		FOR UPDATE`, principal.tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, key,
	).Scan(&storedHash, &state, &storedStatus, &storedResponse, &storedResponseHash, &replayUntil)
	if err == nil {
		if !replayUntil.After(time.Now().UTC()) || state == "expired" {
			if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,updated_at=NOW()
				WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, principal.tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, key); err != nil {
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
			return nil, wrapDBErr(fmt.Errorf("decode idempotency response: %w", err), "create device group failed")
		}
		canonicalResponse, err := json.Marshal(stored)
		if err != nil || !storedResponseHash.Valid || storedResponseHash.String != fmt.Sprintf("%x", sha256.Sum256(canonicalResponse)) {
			return nil, &errs.Error{Code: errs.Internal, Message: "stored idempotency response is invalid"}
		}
		if err := tx.Commit(); err != nil {
			return nil, wrapDBErr(err, "create device group failed")
		}
		return &groupCreateResult{status: int(storedStatus.Int64), response: storedResponse, location: createGroupLocation(stored.Data.ID), requestID: stored.Meta.RequestID, replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sqldb.ErrNoRows) {
		return nil, wrapDBErr(err, "create device group failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_idempotency
		(tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,state,replay_until)
		VALUES ($1,$2,$3,$4,$5,$6,'in_progress',NOW()+INTERVAL '24 hours')`,
		principal.tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, key, requestHash); err != nil {
		return nil, wrapDBErr(err, "create idempotency receipt failed")
	}

	created, err := insertDeviceGroupTx(ctx, tx, principal, normalized)
	if err != nil {
		if isGroupNameUniqueViolation(err) {
			return nil, &errs.Error{Code: errs.AlreadyExists, Message: "a device group with this name already exists under the selected parent"}
		}
		return nil, err
	}
	response := DeviceGroupResponse{Data: created, Meta: RequestMeta{RequestID: requestID}}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "failed to encode response"}
	}
	responseHash := sha256.Sum256(responseJSON)
	if _, err := tx.ExecContext(ctx, `UPDATE metadata_idempotency
		SET state='completed',http_status=201,response=$6::jsonb,response_hash=$7,updated_at=NOW()
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`,
		principal.tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, key, responseJSON, hex.EncodeToString(responseHash[:])); err != nil {
		return nil, wrapDBErr(err, "complete idempotency receipt failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_audit_events
		(id,tenant_id,created_by,operation_id,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
		VALUES ($1,$2,$3,NULL,$4,$5,'device_group',$6,NULL,1,'succeeded','{"created":true}'::jsonb)`,
		uuid.NewString(), principal.tenantID, principal.actorID, requestID, createDeviceGroupOperation, created.ID); err != nil {
		return nil, wrapDBErr(err, "audit device group creation failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "create device group failed")
	}
	return &groupCreateResult{status: 201, response: responseJSON, location: createGroupLocation(created.ID), requestID: requestID}, nil
}

// lockDeviceGroupTree serializes hierarchy mutations for one tenant. Every
// create, rename (which affects descendant paths), and move must acquire this
// lock after its idempotency-key lock and before reading the tree.
func lockDeviceGroupTree(ctx context.Context, tx *sql.Tx, tenantID string) error {
	key, err := json.Marshal(struct {
		Tenant string `json:"tenant"`
		Scope  string `json:"scope"`
	}{tenantID, "device-group-tree"})
	if err != nil {
		return &errs.Error{Code: errs.Internal, Message: "failed to scope device group tree"}
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(key)); err != nil {
		return wrapDBErr(err, "lock device group tree failed")
	}
	return nil
}

func isGroupNameUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName == "uq_device_groups_root_name" || pgErr.ConstraintName == "uq_device_groups_tenant_parent_name"
	}
	return isUniqueViolation(err, "uq_device_groups_root_name") || isUniqueViolation(err, "uq_device_groups_tenant_parent_name")
}

func insertDeviceGroupTx(ctx context.Context, tx *sql.Tx, principal groupWritePrincipal, req *CreateDeviceGroupReq) (DeviceGroup, error) {
	if req.ParentID != nil {
		var parentID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM device_groups WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, principal.tenantID, *req.ParentID).Scan(&parentID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return DeviceGroup{}, &errs.Error{Code: errs.NotFound, Message: "parent device group not found"}
			}
			return DeviceGroup{}, wrapDBErr(err, "validate parent device group failed")
		}
		var maxDepth int
		var hasCycle bool
		err := tx.QueryRowContext(ctx, `
			WITH RECURSIVE ancestors(id,parent_id,depth,path) AS (
				SELECT id,parent_id,1,ARRAY[id] FROM device_groups
				WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
				UNION ALL
				SELECT p.id,p.parent_id,a.depth+1,a.path || p.id
				FROM ancestors a JOIN device_groups p ON p.tenant_id=$1 AND p.id=a.parent_id AND p.deleted_at IS NULL
				WHERE a.depth < 34 AND NOT p.id = ANY(a.path)
			)
			SELECT max(depth),COALESCE(bool_or(parent_id = ANY(path)),false) FROM ancestors`, principal.tenantID, parentID).Scan(&maxDepth, &hasCycle)
		if err != nil {
			return DeviceGroup{}, wrapDBErr(err, "validate parent device group depth failed")
		}
		if hasCycle {
			return DeviceGroup{}, &errs.Error{Code: errs.FailedPrecondition, Message: "parent device group tree contains a cycle"}
		}
		if maxDepth >= maxDeviceGroupDepth {
			return DeviceGroup{}, &errs.Error{Code: errs.InvalidArgument, Message: "device group depth cannot exceed 32"}
		}
	}
	id := uuid.NewString()
	var parent any
	if req.ParentID != nil {
		parent = *req.ParentID
	}
	var group DeviceGroup
	err := tx.QueryRowContext(ctx, `INSERT INTO device_groups
		(id,tenant_id,parent_id,name,description,sort_order,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
		RETURNING id,tenant_id,parent_id,name,description,sort_order,resource_version,created_by,updated_by,created_at,updated_at`,
		id, principal.tenantID, parent, req.Name, req.Description, req.SortOrder, principal.actorID,
	).Scan(&group.ID, &group.TenantID, &group.ParentID, &group.Name, &group.Description, &group.SortOrder, &group.ResourceVersion, &group.CreatedBy, &group.UpdatedBy, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return DeviceGroup{}, err
	}
	groups := []DeviceGroup{group}
	if err := hydrateGroupReadMetadata(ctx, principal.tenantID, groups, tx); err != nil {
		return DeviceGroup{}, err
	}
	return groups[0], nil
}

func createGroupLocation(id string) string { return createDeviceGroupPath + "/" + id }

func idempotencyConflict(message string) error {
	return &errs.Error{Code: errs.AlreadyExists, Message: message}
}

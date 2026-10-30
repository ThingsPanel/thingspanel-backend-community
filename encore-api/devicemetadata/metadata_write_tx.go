package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// metadataWriteCommand is an internal, already authorized non-secret command.
// Request bytes must be the validated canonical request, including omission vs
// explicit null. A handler must reauthorize before entering this function,
// including receipt replay. Secret-bearing responses require the separately
// designed encrypted receipt path and must never use this helper.
type metadataWriteCommand struct {
	TenantID, SubjectID, Operation, Path, Key string
	Method, IfMatch, RequestID                string
	CanonicalBody                             []byte
}

type metadataWriteResponse struct {
	Status    int             `json:"status"`
	Body      json.RawMessage `json:"body"`
	ETag      string          `json:"etag"`
	Location  string          `json:"location"`
	RequestID string          `json:"requestId"`
	Replayed  bool            `json:"-"`
}

type metadataWriteAudit struct {
	ResourceKind                string
	ResourceID                  string
	BeforeVersion, AfterVersion *int64
	RedactedChanges             json.RawMessage
}

// runMetadataWriteTx uses the existing receipt and audit owners. The mutation,
// redacted audit and durable successful response commit together; errors leave
// no in-progress receipt behind. It deliberately does not refactor group paths
// or introduce a second persistence owner.
func runMetadataWriteTx(ctx context.Context, command metadataWriteCommand, apply func(*sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error)) (*metadataWriteResponse, error) {
	id, err := uuid.Parse(command.TenantID)
	if err != nil || id == uuid.Nil || id.String() != command.TenantID || !validAccessTemplateText(command.SubjectID, 1, 128, true) {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated metadata identity"}
	}
	if apply == nil || !validIdempotencyKey(command.Key) || command.Operation == "" || len(command.Operation) > 128 || !strings.HasPrefix(command.Path, "/api/v2/") || len(command.Path) > 1024 || strings.ContainsAny(command.Path, "?#\r\n") || command.RequestID == "" || len(command.RequestID) > 128 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid metadata write command"}
	}
	if command.Method != "POST" && command.Method != "PATCH" && command.Method != "PUT" && command.Method != "DELETE" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid metadata write method"}
	}
	requestSum := sha256.Sum256(append([]byte(command.Method+"\n"+command.Path+"\n"+command.IfMatch+"\n"), command.CanonicalBody...))
	requestHash := hex.EncodeToString(requestSum[:])
	tx, err := beginTenantScopedSQLTx(ctx, command.TenantID, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lock, err := json.Marshal([]string{command.TenantID, command.SubjectID, command.Operation, command.Path, command.Key})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(lock)); err != nil {
		return nil, err
	}
	args := []any{command.TenantID, command.SubjectID, command.Operation, command.Path, command.Key}
	var storedHash, state string
	var status sql.NullInt64
	var responseHash sql.NullString
	var stored []byte
	var expired bool
	err = tx.QueryRow(ctx, `SELECT request_hash,state,http_status,response::text,response_hash,replay_until<=clock_timestamp() FROM metadata_idempotency
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5 FOR UPDATE`, args...).Scan(&storedHash, &state, &status, &stored, &responseHash, &expired)
	if err == nil {
		if state == "expired" || expired {
			if _, err := tx.Exec(ctx, `UPDATE metadata_idempotency SET state='expired',http_status=409,response=NULL,response_hash=NULL,updated_at=NOW()
				WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, args...); err != nil {
				return nil, err
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return nil, idempotencyConflict("idempotency key has expired")
		}
		if storedHash != requestHash {
			return nil, idempotencyConflict("idempotency key was already used for a different request")
		}
		if state != "completed" || !status.Valid || len(stored) == 0 {
			return nil, &errs.Error{Code: errs.Unavailable, Message: "idempotency request is still being processed"}
		}
		sum := sha256.Sum256(stored)
		var result metadataWriteResponse
		if !responseHash.Valid || responseHash.String != hex.EncodeToString(sum[:]) || json.Unmarshal(stored, &result) != nil || result.Status != int(status.Int64) || !json.Valid(result.Body) {
			return nil, &errs.Error{Code: errs.Internal, Message: "stored metadata receipt is invalid"}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		result.Replayed = true
		return &result, nil
	}
	if !errors.Is(err, sqldb.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_idempotency(tenant_id,subject_id,operation_name,canonical_path,idempotency_key,request_hash,state,replay_until)
		VALUES($1,$2,$3,$4,$5,$6,'in_progress',NOW()+INTERVAL '24 hours')`, append(args, requestHash)...); err != nil {
		return nil, err
	}
	result, audit, err := apply(tx)
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 || !json.Valid(result.Body) || audit.ResourceKind == "" || !json.Valid(audit.RedactedChanges) {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid metadata mutation result"}
	}
	resourceID, err := uuid.Parse(audit.ResourceID)
	if err != nil || resourceID == uuid.Nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "invalid metadata audit resource"}
	}
	result.RequestID = command.RequestID
	result.Replayed = false
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	// Hash the database's actual JSONB representation. JSONB can normalize
	// numeric notation (1e3 -> 1000); hashing pre-insert JSON would break valid
	// arbitrary-precision template response replay after that normalization.
	completeArgs := append(args, result.Status, encoded)
	if err := tx.QueryRow(ctx, `UPDATE metadata_idempotency SET state='completed',http_status=$6,response=$7::jsonb,
		response_hash=encode(digest(($7::jsonb)::text,'sha256'),'hex'),updated_at=NOW()
		WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5 RETURNING response::text`, completeArgs...).Scan(&stored); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_audit_events
		(id,tenant_id,created_by,request_id,operation_name,resource_kind,resource_id,before_version,after_version,outcome,redacted_changes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'succeeded',$10::jsonb)`, uuid.NewString(), command.TenantID, command.SubjectID, command.RequestID, command.Operation, audit.ResourceKind, resourceID.String(), audit.BeforeVersion, audit.AfterVersion, []byte(audit.RedactedChanges)); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(stored, &result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}

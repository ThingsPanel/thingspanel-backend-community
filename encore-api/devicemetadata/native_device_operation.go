package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Device metadata mutations that must be applied by the runtime owner are
// durable operations: the metadata transaction records the desired state, an
// outbox row addressed to the runtime owner, and a pending operation. The
// operation only becomes succeeded when that owner reports the applied
// generation back (DM-RT-007). Nothing in this package may mark a
// runtime-dependent operation succeeded on its own.
const (
	operationKindAdministrativeState = "administrative_state"
	operationKindTemplateBinding     = "template_binding"
	operationKindGatewayBinding      = "gateway_binding"
	operationKindDeviceDeletion      = "device_deletion"
	operationKindCredentialChange    = "credential_change"
	operationKindDeviceRenumber      = "device_renumber"

	deviceRuntimeOutboxPending = "pending"
)

// NativeOperation is the durable job projection defined by the contract.
type NativeOperation struct {
	ID              string                   `json:"id"`
	TenantID        string                   `json:"tenantId"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
	CreatedBy       string                   `json:"createdBy"`
	UpdatedBy       string                   `json:"updatedBy"`
	ResourceVersion int64                    `json:"resourceVersion"`
	Kind            string                   `json:"kind"`
	State           string                   `json:"state"`
	ResourceID      *string                  `json:"resourceId"`
	Progress        NativeOperationProgress  `json:"progress"`
	Result          json.RawMessage          `json:"result"`
	Failure         json.RawMessage          `json:"failure"`
	ExpiresAt       *time.Time               `json:"expiresAt"`
}

type NativeOperationProgress struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type NativeOperationResponse struct {
	Data NativeOperation `json:"data"`
	Meta RequestMeta     `json:"meta"`
}

func operationRequestDigest(method, path, ifMatch string, body []byte) string {
	sum := sha256.Sum256(append([]byte(method+"\n"+path+"\n"+ifMatch+"\n"), body...))
	return hex.EncodeToString(sum[:])
}

// createDeviceOperationTx records the durable job for a runtime-dependent
// metadata mutation. The returned operation stays pending until the runtime
// owner acknowledges the outbox row; callers must not shorten that.
func createDeviceOperationTx(
	ctx context.Context,
	tx *sqldb.Tx,
	tenantID, subjectID, kind, deviceID, operationName, requestID, digest string,
	checkpoint map[string]any,
) (string, error) {
	if checkpoint == nil {
		checkpoint = map[string]any{}
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return "", &errs.Error{Code: errs.Internal, Message: "failed to encode operation checkpoint"}
	}
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_operations(
			id,tenant_id,created_by,updated_by,kind,state,originating_operation_name,originating_subject_id,
			resource_kind,resource_id,authorized_selection,request_digest,total_items,completed_items,failed_items,
			checkpoint,expires_at)
		VALUES($1,$2,$3,$3,$4,'pending',$5,$3,'device',$6,'{}'::jsonb,$7,1,0,0,$8::jsonb,NOW()+INTERVAL '7 days')`,
		id, tenantID, subjectID, kind, operationName, deviceID, digest, encoded); err != nil {
		return "", err
	}
	return id, nil
}

// createCompletedDeviceOperationTx records an operation for a request that
// produced no runtime-visible change (repeating the current desired state).
// It is already terminal because there is nothing for the runtime owner to
// apply, which keeps repeated identical requests free of extra side effects.
func createCompletedDeviceOperationTx(
	ctx context.Context,
	tx *sqldb.Tx,
	tenantID, subjectID, kind, deviceID, operationName, requestID, digest string,
) (string, error) {
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_operations(
			id,tenant_id,created_by,updated_by,kind,state,originating_operation_name,originating_subject_id,
			resource_kind,resource_id,authorized_selection,request_digest,total_items,completed_items,failed_items,
			checkpoint,result,finished_at,expires_at)
		VALUES($1,$2,$3,$3,$4,'succeeded',$5,$3,'device',$6,'{}'::jsonb,$7,1,1,0,'{"awaiting":"none"}'::jsonb,
			jsonb_build_object('resourceIds',jsonb_build_array($8::text)),NOW(),NOW()+INTERVAL '7 days')`,
		id, tenantID, subjectID, kind, operationName, deviceID, digest, deviceID); err != nil {
		return "", err
	}
	return id, nil
}

// enqueueDeviceRuntimeTx writes the integration row the runtime owner consumes.
// The desired document is the complete target state for that concern, not a
// delta, so a consumer that applies the newest row per (device, kind) converges.
func enqueueDeviceRuntimeTx(
	ctx context.Context,
	tx *sqldb.Tx,
	tenantID, deviceID, operationID, kind string,
	generation int64,
	desired any,
) error {
	encoded, err := json.Marshal(desired)
	if err != nil {
		return &errs.Error{Code: errs.Internal, Message: "failed to encode runtime request"}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO device_runtime_outbox(
			id,tenant_id,device_id,operation_id,kind,generation,desired,state,updated_by)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,'metadata')`,
		uuid.NewString(), tenantID, deviceID, operationID, kind, generation, encoded, deviceRuntimeOutboxPending); err != nil {
		return err
	}
	return nil
}

const nativeOperationSelectFields = `id::text,tenant_id::text,created_at,updated_at,created_by,updated_by,
	resource_version,kind,state,resource_id::text,total_items,completed_items,failed_items,result,failure,expires_at`

type nativeOperationRowScanner interface {
	Scan(...any) error
}

func scanNativeOperation(row nativeOperationRowScanner) (*NativeOperation, error) {
	var out NativeOperation
	var createdAt, updatedAt, expiresAt sql.NullTime
	var resourceID sql.NullString
	var result, failure []byte
	err := row.Scan(&out.ID, &out.TenantID, &createdAt, &updatedAt, &out.CreatedBy, &out.UpdatedBy,
		&out.ResourceVersion, &out.Kind, &out.State, &resourceID,
		&out.Progress.Total, &out.Progress.Completed, &out.Progress.Failed, &result, &failure, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "operation not found"}
	}
	if err != nil {
		return nil, wrapDBErr(err, "read operation failed")
	}
	out.CreatedAt = createdAt.Time.UTC()
	out.UpdatedAt = updatedAt.Time.UTC()
	out.ResourceID = nullableStringPointer(resourceID)
	if len(result) == 0 {
		out.Result = json.RawMessage(`null`)
	} else {
		out.Result = append(json.RawMessage(nil), result...)
	}
	if len(failure) == 0 {
		out.Failure = json.RawMessage(`null`)
	} else {
		out.Failure = append(json.RawMessage(nil), failure...)
	}
	if expiresAt.Valid {
		expires := expiresAt.Time.UTC()
		out.ExpiresAt = &expires
	}
	return &out, nil
}

func loadNativeOperationTx(ctx context.Context, tx *sql.Tx, tenantID, operationID string) (*NativeOperation, error) {
	return scanNativeOperation(tx.QueryRowContext(ctx,
		`SELECT `+nativeOperationSelectFields+` FROM metadata_operations WHERE tenant_id=$1 AND id=$2`,
		tenantID, operationID))
}

func loadNativeOperationEncoreTx(ctx context.Context, tx *sqldb.Tx, tenantID, operationID string) (*NativeOperation, error) {
	return scanNativeOperation(tx.QueryRow(ctx,
		`SELECT `+nativeOperationSelectFields+` FROM metadata_operations WHERE tenant_id=$1 AND id=$2`,
		tenantID, operationID))
}

// currentNativeOperationIdentity requires the metadata read scope and, because
// an operation may expose which resources a job touched, is limited to the
// same verified tenant. A subject that is not a tenant administrator can still
// poll a job it started; the operation row records that subject.
func currentNativeOperationIdentity() (nativeDeviceReadIdentity, error) {
	return currentNativeDeviceReadIdentity()
}

//encore:api auth raw method=GET path=/api/v2/metadata-operations/:operationID
func (s *Service) GetMetadataOperation(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeOperationIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	operationID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("operationID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	tx, err := beginTenantGroupTx(r.Context(), identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, wrapDBErr(err, "read operation failed"), 0, false)
		return
	}
	defer tx.Rollback()
	operation, err := loadNativeOperationTx(r.Context(), tx, identity.tenantID, operationID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if err := tx.Commit(); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, wrapDBErr(err, "read operation failed"), 0, false)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", NativeOperationResponse{
		Data: *operation, Meta: RequestMeta{RequestID: requestID},
	})
}

// writeOperationAccepted emits the 202 envelope required by the contract: the
// operation is accepted, not completed. Callers must not translate this into a
// success state in the UI.
func writeOperationAccepted(w http.ResponseWriter, requestID, instance, operationID string, etag string, operation *NativeOperation) {
	w.Header().Set("Location", "/api/v2/metadata-operations/"+operationID)
	nativehttp.WriteJSON(w, requestID, instance, http.StatusAccepted, etag, NativeOperationResponse{
		Data: *operation, Meta: RequestMeta{RequestID: requestID},
	})
}

package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

// Rotation and revocation are durable operations: metadata records the new
// credential state and an outbox row for the runtime owner, and the operation
// stays pending until that owner reports back (DM-RT-007). Secrets never appear
// in these requests, responses, audit rows or outbox documents.

type nativeRotateCredentialInput struct {
	ReplacementCredentialID string
	OverlapSeconds          int
	ExpectedDeviceVersion   int64
}

type nativeRevokeCredentialInput struct {
	ExpectedDeviceVersion int64
	Reason                string
}

func decodeNativeRotateCredential(body []byte) (nativeRotateCredentialInput, error) {
	var out nativeRotateCredentialInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("replacementCredentialId", "overlapSeconds", "expectedDeviceVersion")); err != nil {
		return out, err
	}
	for _, name := range []string{"replacementCredentialId", "overlapSeconds", "expectedDeviceVersion"} {
		if _, ok := fields[name]; !ok {
			return out, invalid(name, "is required")
		}
	}
	var replacement string
	if err := decodeString(fields["replacementCredentialId"], "replacementCredentialId", &replacement); err != nil {
		return out, err
	}
	if out.ReplacementCredentialID, err = nativeReadUUID(replacement); err != nil {
		return out, invalid("replacementCredentialId", "must be a UUID")
	}
	var overlap int64
	if err := json.Unmarshal(fields["overlapSeconds"], &overlap); err != nil || overlap < 0 || overlap > 3600 {
		return out, invalid("overlapSeconds", "must be an integer between 0 and 3600")
	}
	out.OverlapSeconds = int(overlap)
	if out.ExpectedDeviceVersion, err = decodeExpectedDeviceVersion(fields["expectedDeviceVersion"]); err != nil {
		return out, err
	}
	return out, nil
}

func decodeNativeRevokeCredential(body []byte) (nativeRevokeCredentialInput, error) {
	var out nativeRevokeCredentialInput
	fields, err := strictJSONObject(body)
	if err != nil {
		return out, err
	}
	if err := rejectUnknown(fields, setOf("expectedDeviceVersion", "reason")); err != nil {
		return out, err
	}
	for _, name := range []string{"expectedDeviceVersion", "reason"} {
		if _, ok := fields[name]; !ok {
			return out, invalid(name, "is required")
		}
	}
	if out.ExpectedDeviceVersion, err = decodeExpectedDeviceVersion(fields["expectedDeviceVersion"]); err != nil {
		return out, err
	}
	if err := decodeString(fields["reason"], "reason", &out.Reason); err != nil {
		return out, err
	}
	if n := len([]rune(out.Reason)); n < 1 || n > 512 || !validAccessTemplateText(out.Reason, 1, 512, true) {
		return out, invalid("reason", "must be 1 to 512 printable characters")
	}
	return out, nil
}

func decodeExpectedDeviceVersion(raw json.RawMessage) (int64, error) {
	var version int64
	if err := json.Unmarshal(raw, &version); err != nil || version < 1 || version > 9007199254740991 {
		return 0, invalid("expectedDeviceVersion", "must be a positive integer")
	}
	return version, nil
}

// lockDeviceForCredentialChangeTx locks the live device and enforces the
// caller's expected version; a stale version is a precondition failure.
func lockDeviceForCredentialChangeTx(ctx context.Context, tx *sqldb.Tx, tenantID, deviceID string, expected int64) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, `SELECT resource_version FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		tenantID, deviceID).Scan(&version)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return 0, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	if err != nil {
		return 0, err
	}
	if version != expected {
		return 0, &errs.Error{Code: errs.FailedPrecondition, Message: "device has changed; read it again before changing credentials"}
	}
	return version, nil
}

type lockedCredential struct {
	status                string
	rotationOperationID   *string
	supersedesCredentialI *string
}

func lockCredentialTx(ctx context.Context, tx *sqldb.Tx, tenantID, deviceID, credentialID string) (lockedCredential, error) {
	var c lockedCredential
	err := tx.QueryRow(ctx, `SELECT status,rotation_operation_id::text,supersedes_credential_id::text FROM device_credentials
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND purpose='device_access' FOR UPDATE`,
		tenantID, deviceID, credentialID).Scan(&c.status, &c.rotationOperationID, &c.supersedesCredentialI)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return c, &errs.Error{Code: errs.NotFound, Message: "credential not found"}
	}
	return c, err
}

func nativeCredentialChangeAccepted(ctx context.Context, tx *sqldb.Tx, tenantID, operationID, requestID string) (metadataWriteResponse, error) {
	operation, err := loadNativeOperationEncoreTx(ctx, tx, tenantID, operationID)
	if err != nil {
		return metadataWriteResponse{}, err
	}
	return operationAcceptedResponse(operation, operationID, requestID, "")
}

func rotateNativeDeviceCredential(ctx context.Context, identity nativeCredentialWriteIdentity, deviceID, credentialID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeRotateCredential(body)
	if err != nil {
		return nil, err
	}
	if input.ReplacementCredentialID == credentialID {
		return nil, invalid("replacementCredentialId", "must differ from the credential being rotated")
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/credentials/" + credentialID + "/rotations"
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "rotateDeviceCredential",
		Method: http.MethodPost, Path: path, Key: key, RequestID: requestID, CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		version, err := lockDeviceForCredentialChangeTx(ctx, tx, identity.tenantID, deviceID, input.ExpectedDeviceVersion)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		// Lock in a stable order so concurrent rotations cannot deadlock.
		first, second := credentialID, input.ReplacementCredentialID
		if second < first {
			first, second = second, first
		}
		locked := map[string]lockedCredential{}
		for _, id := range []string{first, second} {
			if locked[id], err = lockCredentialTx(ctx, tx, identity.tenantID, deviceID, id); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		current, replacement := locked[credentialID], locked[input.ReplacementCredentialID]
		if current.status != "active" {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "only an active credential can be rotated"}
		}
		if replacement.status != "pending" || replacement.rotationOperationID != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "replacement credential must be pending and not used by another rotation"}
		}
		var alreadyRotating bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_credentials WHERE tenant_id=$1 AND device_id=$2
			AND supersedes_credential_id=$3 AND status<>'revoked')`, identity.tenantID, deviceID, credentialID).Scan(&alreadyRotating); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if alreadyRotating {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "credential already has a rotation in progress"}
		}
		digest := operationRequestDigest(command.Method, path, "", canonical)
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID, operationKindCredentialChange,
			deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-application", "releaseTask": "DM-RT-007", "change": "rotation"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE device_credentials SET rotation_operation_id=$4,supersedes_credential_id=$5,
			updated_by=$6,resource_version=resource_version+1 WHERE tenant_id=$1 AND device_id=$2 AND id=$3`,
			identity.tenantID, deviceID, input.ReplacementCredentialID, operationID, credentialID, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID, operationKindCredentialChange, version,
			map[string]any{"deviceId": deviceID, "change": "rotation", "credentialId": credentialID,
				"replacementCredentialId": input.ReplacementCredentialID, "overlapSeconds": input.OverlapSeconds}); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeCredentialChangeAccepted(ctx, tx, identity.tenantID, operationID, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"change": "rotation", "credentialId": credentialID,
			"replacementCredentialId": input.ReplacementCredentialID, "overlapSeconds": input.OverlapSeconds})
		return response, metadataWriteAudit{ResourceKind: "device_credential", ResourceID: credentialID, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func revokeNativeDeviceCredential(ctx context.Context, identity nativeCredentialWriteIdentity, deviceID, credentialID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	input, err := decodeNativeRevokeCredential(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	path := "/api/v2/devices/" + deviceID + "/credentials/" + credentialID + "/revocations"
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "revokeDeviceCredential",
		Method: http.MethodPost, Path: path, Key: key, RequestID: requestID, CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		version, err := lockDeviceForCredentialChangeTx(ctx, tx, identity.tenantID, deviceID, input.ExpectedDeviceVersion)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		current, err := lockCredentialTx(ctx, tx, identity.tenantID, deviceID, credentialID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if current.status == "revoked" {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "credential is already revoked"}
		}
		digest := operationRequestDigest(command.Method, path, "", canonical)
		operationID, err := createDeviceOperationTx(ctx, tx, identity.tenantID, identity.subjectID, operationKindCredentialChange,
			deviceID, command.Operation, requestID, digest,
			map[string]any{"awaiting": "runtime-application", "releaseTask": "DM-RT-007", "change": "revocation"})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		// Revocation is one-way and takes effect in metadata immediately; the
		// runtime owner is told to drop the credential through the outbox.
		if _, err := tx.Exec(ctx, `UPDATE device_credentials SET status='revoked',revoked_at=NOW(),revoked_by=$4,
			revocation_reason=$5,updated_by=$4,resource_version=resource_version+1
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND status<>'revoked'`,
			identity.tenantID, deviceID, credentialID, identity.subjectID, input.Reason); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := enqueueDeviceRuntimeTx(ctx, tx, identity.tenantID, deviceID, operationID, operationKindCredentialChange, version,
			map[string]any{"deviceId": deviceID, "change": "revocation", "credentialId": credentialID}); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeCredentialChangeAccepted(ctx, tx, identity.tenantID, operationID, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		// The free-text reason is operator input and stays out of the audit change set.
		changes, _ := json.Marshal(map[string]any{"change": "revocation", "credentialId": credentialID})
		return response, metadataWriteAudit{ResourceKind: "device_credential", ResourceID: credentialID, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func serveNativeCredentialChange(w http.ResponseWriter, r *http.Request, run func(context.Context, nativeCredentialWriteIdentity, string, string, string, []byte, string) (*metadataWriteResponse, error)) {
	requestID := newID()
	w.Header().Set("Cache-Control", "no-store")
	identity, err := currentNativeCredentialWriteIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	params := encore.CurrentRequest().PathParams
	deviceID, err := nativeReadUUID(params.Get("deviceID"))
	if err != nil {
		writeNativeCredentialMutationError(w, r, requestID, invalid("deviceId", "must be a UUID"))
		return
	}
	credentialID, err := nativeReadUUID(params.Get("credentialID"))
	if err != nil {
		writeNativeCredentialMutationError(w, r, requestID, invalid("credentialId", "must be a UUID"))
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := readNativeCredentialBody(w, r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := run(r.Context(), identity, deviceID, credentialID, key, body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/credentials/:credentialID/rotations
func (s *Service) RotateNativeDeviceCredential(w http.ResponseWriter, r *http.Request) {
	serveNativeCredentialChange(w, r, rotateNativeDeviceCredential)
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/credentials/:credentialID/revocations
func (s *Service) RevokeNativeDeviceCredential(w http.ResponseWriter, r *http.Request) {
	serveNativeCredentialChange(w, r, revokeNativeDeviceCredential)
}

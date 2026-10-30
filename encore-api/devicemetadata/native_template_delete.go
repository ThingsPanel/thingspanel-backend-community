package devicemetadata

import (
	"context"
	"errors"
	"io"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
)

const deleteNativeTemplateOperation = "deleteDeviceAccessTemplate"

var errNativeTemplateInUse = &errs.Error{Code: errs.FailedPrecondition, Message: "device access template is still used by devices; unbind them before deleting"}

// deleteNativeTemplate soft-deletes a template that no live device uses. The
// delete is conditional on the template's current ETag and idempotent per key,
// like every other metadata write. Only references this service owns are
// checked; products, OTA, automation rules and market packages live elsewhere.
func deleteNativeTemplate(ctx context.Context, identity nativeTemplateWriteIdentity, id, key, ifMatch, requestID string) (*metadataWriteResponse, error) {
	command := metadataWriteCommand{TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: deleteNativeTemplateOperation, Method: "DELETE", Path: "/api/v2/device-access-templates/" + id, Key: key, RequestID: requestID, IfMatch: ifMatch, CanonicalBody: []byte("{}")}
	return runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		stored, err := loadAccessTemplateConfigurationTx(ctx, tx, identity.tenantID, id, true)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		current, err := loadNativeDeviceAccessTemplateTx(ctx, tx, identity.tenantID, id, nil)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		etag, err := nativeTemplateETag(current)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if ifMatch != etag {
			return metadataWriteResponse{}, metadataWriteAudit{}, errNativeTemplateETagMismatch
		}
		var inUse bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM device_access_bindings b JOIN devices d ON d.tenant_id=b.tenant_id AND d.id=b.device_id
			WHERE b.tenant_id=$1 AND b.template_id=$2 AND d.deleted_at IS NULL)`, identity.tenantID, id).Scan(&inUse); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if inUse {
			return metadataWriteResponse{}, metadataWriteAudit{}, errNativeTemplateInUse
		}
		var deletedID string
		err = tx.QueryRow(ctx, `UPDATE device_access_templates SET deleted_at=NOW(),updated_at=NOW(),updated_by=$3,resource_version=resource_version+1
			WHERE tenant_id=$1 AND id=$2 AND resource_version=$4 AND deleted_at IS NULL RETURNING id`,
			identity.tenantID, id, identity.subjectID, stored.ResourceVersion).Scan(&deletedID)
		if errors.Is(err, sqldb.ErrNoRows) {
			return metadataWriteResponse{}, metadataWriteAudit{}, errNativeTemplateETagMismatch
		}
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeTemplateMutationResponse(current, requestID)
		after := stored.ResourceVersion + 1
		return response, metadataWriteAudit{ResourceKind: "device_access_template", ResourceID: id, BeforeVersion: &stored.ResourceVersion, AfterVersion: &after, RedactedChanges: []byte(`{"deleted":true}`)}, err
	})
}

//encore:api auth raw method=DELETE path=/api/v2/device-access-templates/:templateID
func (s *Service) DeleteNativeDeviceAccessTemplate(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateWriteIdentity()
	if err == nil {
		if data := authn.CurrentData(); data == nil || !authz.Can(data, "template", "delete") {
			err = &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to delete device access templates"}
		}
	}
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	var bodyByte [1]byte
	if n, readErr := http.MaxBytesReader(w, r.Body, 1).Read(bodyByte[:]); n != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("DELETE request body must be empty"), http.StatusBadRequest)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("templateID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	ifMatch := r.Header.Values("If-Match")
	if len(ifMatch) == 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match is required"}, http.StatusPreconditionRequired)
		return
	}
	if len(ifMatch) != 1 || !validDeviceGroupIfMatch(ifMatch[0]) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "If-Match must contain one strong template ETag"}, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := deleteNativeTemplate(r.Context(), identity, id, key, ifMatch[0], requestID)
	if err != nil {
		writeNativeTemplateMutationError(w, r, requestID, mapNativeTemplateWriteError(err), true)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

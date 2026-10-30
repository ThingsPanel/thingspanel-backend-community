package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"

	"encore.app/devicemetadata/accessconfig"
)

const copyNativeTemplateOperation = "copyDeviceAccessTemplate"

type nativeTemplateCopyRequest struct {
	Name string `json:"name"`
}

func decodeNativeTemplateCopy(body []byte) (nativeTemplateCopyRequest, error) {
	var req nativeTemplateCopyRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, invalid("name", "request body must be an object with only a name")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return req, invalid("name", "request body has trailing data")
	}
	if !validAccessTemplateText(req.Name, 1, 128, true) {
		return req, invalid("name", "template name must contain 1 to 128 characters without surrounding whitespace or control characters")
	}
	return req, nil
}

// copyNativeTemplate creates a new, enabled template from the current
// configuration of an existing one. The copy keeps the descriptive fields, tags,
// cover and the whole configuration (including the selected model version) and
// takes the new name; devices, history and references are not copied.
func copyNativeTemplate(ctx context.Context, identity nativeTemplateWriteIdentity, sourceID, key string, body []byte, requestID string, check nativeDeviceConfigurationCheck, checkAsset nativeTemplateAssetCheck) (*metadataWriteResponse, error) {
	req, err := decodeNativeTemplateCopy(body)
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(req)
	command := metadataWriteCommand{TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: copyNativeTemplateOperation, Method: "POST", Path: "/api/v2/device-access-templates/" + sourceID + "/copies", Key: key, RequestID: requestID, CanonicalBody: canonical}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		source, err := loadAccessTemplateConfigurationTx(ctx, tx, identity.tenantID, sourceID, false)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		configuration, err := accessconfig.DecodeConfiguration(source.Configuration)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "stored template configuration is invalid"}
		}
		// The selected model version must still resolve; a copy is a new save.
		if err := validateNativeTemplateConfigurationTx(ctx, tx, identity.tenantID, configuration, check); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		row := &accessTemplateConfigurationRecord{Name: req.Name, Description: source.Description, Brand: source.Brand, ProductName: source.ProductName, ModelNumber: source.ModelNumber, Tags: source.Tags, Configuration: source.Configuration, AdministrativeState: "enabled", ImageAssetID: source.ImageAssetID}
		if row.ImageAssetID != nil {
			if checkAsset == nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "template image asset validation is unavailable"}
			}
			if err := checkAsset(ctx, identity.tenantID, *row.ImageAssetID); err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, err
			}
		}
		created, err := insertAccessTemplateConfigurationTx(ctx, tx, identity.tenantID, identity.subjectID, row)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		template, err := assembleNativeDeviceAccessTemplate(created, 0)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		response, err := nativeTemplateMutationResponse(template, requestID)
		response.Status, response.Location = 201, "/api/v2/device-access-templates/"+created.ID
		changes, _ := json.Marshal(map[string]string{"copiedFrom": sourceID})
		return response, metadataWriteAudit{ResourceKind: "device_access_template", ResourceID: created.ID, AfterVersion: &created.ResourceVersion, RedactedChanges: changes}, err
	})
	return result, mapNativeTemplateWriteError(err)
}

//encore:api auth raw method=POST path=/api/v2/device-access-templates/:templateID/copies
func (s *Service) CopyNativeDeviceAccessTemplate(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateWriteIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("templateID"))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "copy request body is too large or unreadable"}, http.StatusBadRequest)
		return
	}
	result, err := copyNativeTemplate(r.Context(), identity, id, key, body, requestID, validateNativeDeviceConfiguration, validateNativeTemplateAsset)
	if err != nil {
		writeNativeTemplateMutationError(w, r, requestID, err, false)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

package devicemetadata

import (
	"errors"
	"io"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
)

const nativeTemplateWriteMaxBytes = 4 << 20

func writeNativeTemplateMutationError(w http.ResponseWriter, r *http.Request, requestID string, err error, patch bool) {
	var inputErr *NativeDeviceInputError
	if errors.As(err, &inputErr) {
		writeNativeMetadataInputProblem(w, requestID, r.URL.Path, "template", nativeTemplateInputSchemaPaths, err)
		return
	}
	var apiErr *errs.Error
	if !errors.As(err, &apiErr) {
		rlog.Error("native template mutation failed", "request_id", requestID, "code", "internal")
		err = &errs.Error{Code: errs.Internal, Message: "template persistence failed"}
	}
	status := 0
	if errs.Code(err) == errs.FailedPrecondition {
		status = http.StatusConflict
		if patch && errors.Is(err, errNativeTemplateETagMismatch) {
			status = http.StatusPreconditionFailed
		}
	}
	writeGroupProblem(w, requestID, r.URL.Path, err, status, true)
}

//encore:api auth raw method=POST path=/api/v2/device-access-templates
func (s *Service) CreateNativeDeviceAccessTemplate(w http.ResponseWriter, r *http.Request) {
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
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nativeTemplateWriteMaxBytes))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "template request body is too large or unreadable"}, http.StatusBadRequest)
		return
	}
	result, err := createNativeTemplate(r.Context(), identity, key, body, requestID, validateNativeDeviceConfiguration, validateNativeTemplateAsset)
	if err != nil {
		writeNativeTemplateMutationError(w, r, requestID, err, false)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=PATCH path=/api/v2/device-access-templates/:templateID
func (s *Service) UpdateNativeDeviceAccessTemplate(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateWriteIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nativeTemplateWriteMaxBytes))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "template request body is too large or unreadable"}, http.StatusBadRequest)
		return
	}
	result, err := updateNativeTemplate(r.Context(), identity, id, key, ifMatch[0], body, requestID, validateNativeDeviceConfiguration, validateNativeTemplateAsset)
	if err != nil {
		writeNativeTemplateMutationError(w, r, requestID, err, true)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

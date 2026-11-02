package devicemetadata

import (
	"errors"
	"io"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
)

const nativeDeviceWriteMaxBytes = 1 << 20

func nativeDeviceWriteKey(r *http.Request) (string, error) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !validIdempotencyKey(keys[0]) {
		return "", &errs.Error{Code: errs.InvalidArgument, Message: "Idempotency-Key must be 8 to 128 allowed ASCII characters"}
	}
	return keys[0], nil
}

func writeNativeDeviceMutationError(w http.ResponseWriter, r *http.Request, requestID string, err error, patch bool) {
	var inputErr *NativeDeviceInputError
	if errors.As(err, &inputErr) {
		writeNativeDeviceInputProblem(w, requestID, r.URL.Path, err)
		return
	}
	var apiErr *errs.Error
	if !errors.As(err, &apiErr) {
		rlog.Error("native device mutation failed", "request_id", requestID, "code", "internal")
		writeGroupProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.Internal, Message: "device metadata persistence failed"}, http.StatusInternalServerError, true)
		return
	}
	status := 0
	if apiErr.Code == errs.FailedPrecondition {
		status = http.StatusConflict
		if patch {
			status = http.StatusPreconditionFailed
		}
	}
	if apiErr.Code == errs.Internal || apiErr.Code == errs.Unavailable {
		rlog.Error("native device mutation failed", "request_id", requestID, "code", apiErr.Code.String())
	}
	writeGroupProblem(w, requestID, r.URL.Path, err, status, true)
}

func writeNativeDeviceMutationResponse(w http.ResponseWriter, result *metadataWriteResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", result.RequestID)
	if result.ETag != "" {
		w.Header().Set("ETag", result.ETag)
	}
	if result.Location != "" {
		w.Header().Set("Location", result.Location)
	}
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

//encore:api auth raw method=POST path=/api/v2/devices
func (s *Service) CreateNativeDevice(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceWriteIdentity(true)
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nativeDeviceWriteMaxBytes))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "device request body is too large or unreadable"}, http.StatusBadRequest)
		return
	}
	// Built-in MQTT validates locally. Connector configurations require their
	// authoritative adapter; accepted metadata is not runtime activation.
	result, err := createNativeDevice(r.Context(), identity, key, body, requestID, validateNativeDeviceConfiguration)
	if err != nil {
		writeNativeDeviceMutationError(w, r, requestID, err, false)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=PATCH path=/api/v2/devices/:deviceID
func (s *Service) UpdateNativeDevice(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
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
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "If-Match must contain one strong device ETag"}, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nativeDeviceWriteMaxBytes))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "device request body is too large or unreadable"}, http.StatusBadRequest)
		return
	}
	result, err := updateNativeDevice(r.Context(), identity, id, key, ifMatch[0], body, requestID)
	if err != nil {
		writeNativeDeviceMutationError(w, r, requestID, err, true)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

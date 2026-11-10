package devicemetadata

import (
	"bytes"
	"net/http"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// nativeConditionalDeviceWrite is the authorized, syntactically valid envelope
// shared by device sub-resource writes that require If-Match and an
// Idempotency-Key. Resource-specific validation happens after this step and
// inside the metadata write transaction.
type nativeConditionalDeviceWrite struct {
	identity nativeDeviceWriteIdentity
	deviceID string
	ifMatch  string
	key      string
	body     []byte
}

// readNativeConditionalDeviceWrite writes the Problem response itself and
// returns false when the request cannot proceed. etagLabel only names the
// validator in the error text. When wantBody is false any non-blank body is
// rejected, because the operation is fully described by the path.
func readNativeConditionalDeviceWrite(w http.ResponseWriter, r *http.Request, requestID, etagLabel string, wantBody bool) (*nativeConditionalDeviceWrite, bool) {
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return nil, false
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return nil, false
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return nil, false
	}
	ifMatch := r.Header.Values("If-Match")
	if len(ifMatch) == 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match is required"}, http.StatusPreconditionRequired)
		return nil, false
	}
	if len(ifMatch) != 1 || !validDeviceGroupIfMatch(ifMatch[0]) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "If-Match must contain one strong " + etagLabel + " ETag"}, http.StatusBadRequest)
		return nil, false
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return nil, false
	}
	body, err := nativeDeviceWriteBody(w, r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return nil, false
	}
	if !wantBody && len(bytes.TrimSpace(body)) != 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "this operation does not accept a request body"}, http.StatusBadRequest)
		return nil, false
	}
	return &nativeConditionalDeviceWrite{identity: identity, deviceID: deviceID, ifMatch: ifMatch[0], key: key, body: body}, true
}

// nativeDeviceConflict is a business-state conflict (HTTP 409). It is kept
// distinct from errs.FailedPrecondition, which the device write handlers map to
// 412 and which therefore means only "the If-Match validator is stale".
func nativeDeviceConflict(message string) error {
	return &errs.Error{Code: errs.Aborted, Message: message}
}

// nativeDeviceStaleValidator is the 412 outcome for a stale If-Match.
func nativeDeviceStaleValidator(message string) error {
	return &errs.Error{Code: errs.FailedPrecondition, Message: message}
}

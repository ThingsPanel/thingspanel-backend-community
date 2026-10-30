package devicemetadata

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestWriteNativeDeviceMutationErrorMapsProblemStatusesAndRedacts(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		patch      bool
		wantStatus int
		wantDetail string
		forbidden  []string
	}{
		{name: "structural input 400", err: &NativeDeviceInputError{Field: "$", Reason: "BODY_SENTINEL", Structural: true}, wantStatus: http.StatusBadRequest, wantDetail: "request body must be a valid JSON object", forbidden: []string{"BODY_SENTINEL"}},
		{name: "field input 422", err: &NativeDeviceInputError{Field: "deviceNumber", Reason: "invalid field value"}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "one or more device fields are invalid"},
		{name: "unexpected database error 500", err: errors.New("SQL_SENTINEL body=PRIVATE_INPUT"), wantStatus: http.StatusInternalServerError, wantDetail: "internal server error", forbidden: []string{"SQL_SENTINEL", "PRIVATE_INPUT", "persistence failed"}},
		{name: "typed unavailable 503", err: &errs.Error{Code: errs.Unavailable, Message: "binding snapshot unavailable"}, wantStatus: http.StatusServiceUnavailable, wantDetail: "internal server error", forbidden: []string{"binding snapshot unavailable"}},
		{name: "patch failed precondition 412", err: &errs.Error{Code: errs.FailedPrecondition, Message: "stale device ETag"}, patch: true, wantStatus: http.StatusPreconditionFailed, wantDetail: "stale device ETag"},
		{name: "create failed precondition 409", err: &errs.Error{Code: errs.FailedPrecondition, Message: "device number conflicts"}, wantStatus: http.StatusConflict, wantDetail: "device number conflicts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v2/devices/"+uuid.NewString(), nil)
			recorder := httptest.NewRecorder()
			writeNativeDeviceMutationError(recorder, req, "native-write-request", tc.err, tc.patch)
			if recorder.Code != tc.wantStatus || recorder.Header().Get("Content-Type") != "application/problem+json" || recorder.Header().Get("X-Request-Id") != "native-write-request" {
				t.Fatalf("status=%d headers=%v, want status=%d Problem", recorder.Code, recorder.Header(), tc.wantStatus)
			}
			var problem struct {
				Status    int    `json:"status"`
				Detail    string `json:"detail"`
				Instance  string `json:"instance"`
				RequestID string `json:"requestId"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode Problem: %v", err)
			}
			if problem.Status != tc.wantStatus || problem.Detail != tc.wantDetail || problem.Instance != req.URL.Path || problem.RequestID != "native-write-request" {
				t.Fatalf("Problem=%#v, want status/detail/request/path", problem)
			}
			for _, forbidden := range tc.forbidden {
				if strings.Contains(recorder.Body.String(), forbidden) {
					t.Fatalf("Problem leaked %q: %s", forbidden, recorder.Body.String())
				}
			}
		})
	}
}

func TestNativeDeviceWriteKeyRejectsDuplicateAndEmptyOnCreate(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	data := &authn.Data{UserID: "native-device-write-key-test", TenantID: uuid.NewString(), Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	for _, tc := range []struct {
		name   string
		values []string
	}{
		{name: "duplicate", values: []string{"native-key-first", "native-key-second"}},
		{name: "empty", values: []string{""}},
		{name: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v2/devices", strings.NewReader(`{"name":"Pump","deviceNumber":"pump-1","deviceKind":"standalone"}`))
			for _, value := range tc.values {
				req.Header.Add("Idempotency-Key", value)
			}
			recorder := httptest.NewRecorder()
			(&Service{}).CreateNativeDevice(recorder, req)
			if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Content-Type") != "application/problem+json" || recorder.Header().Get("X-Request-Id") == "" {
				t.Fatalf("invalid key status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "native-key-first") || strings.Contains(recorder.Body.String(), "native-key-second") {
				t.Fatalf("Problem echoed idempotency input: %s", recorder.Body.String())
			}
		})
	}
}

func TestNativeDevicePatchMissingIfMatchProblemIs428(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeGroupUpdateProblem(recorder, "native-write-precondition", "/api/v2/devices/"+uuid.NewString(), &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match is required"}, http.StatusPreconditionRequired)
	var problem struct {
		Status    int    `json:"status"`
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil || recorder.Code != http.StatusPreconditionRequired || recorder.Header().Get("Content-Type") != "application/problem+json" || recorder.Header().Get("X-Request-Id") != "native-write-precondition" || problem.Status != http.StatusPreconditionRequired || problem.RequestID != "native-write-precondition" {
		t.Fatalf("missing If-Match Problem status=%d headers=%v body=%#v err=%v", recorder.Code, recorder.Header(), problem, err)
	}
}

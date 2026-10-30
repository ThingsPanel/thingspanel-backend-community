package devicemetadata

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeCreateInputError(body string) error {
	_, err := DecodeNativeCreateDevice([]byte(body))
	return err
}

func decodeUpdateInputError(body string) error {
	_, err := DecodeNativeUpdateDevice([]byte(body))
	return err
}

func TestWriteNativeDeviceInputProblemUsesOAS422FieldErrors(t *testing.T) {
	err := decodeCreateInputError(`{"name":"Pump","deviceNumber":"pump-1","deviceKind":"invalid-kind","description":"BODY_SECRET"}`)
	if err == nil || err.Error() != "deviceKind: value is not in the allowed enum" {
		t.Fatalf("typed input error text changed: %v", err)
	}
	response := httptest.NewRecorder()
	writeNativeDeviceInputProblem(response, "request-123", "/api/v2/devices", err)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want 422: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/problem+json" || response.Header().Get("X-Request-Id") != "request-123" {
		t.Fatalf("problem headers missing: %#v", response.Header())
	}
	var problem struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		Status    int    `json:"status"`
		Detail    string `json:"detail"`
		Instance  string `json:"instance"`
		Code      string `json:"code"`
		RequestID string `json:"requestId"`
		Errors    []struct {
			Pointer string `json:"pointer"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode Problem: %v", err)
	}
	if problem.Type != "about:blank" || problem.Title != "Unprocessable Entity" || problem.Status != 422 ||
		problem.Instance != "/api/v2/devices" || problem.RequestID != "request-123" || problem.Code != "invalid_device_field" {
		t.Fatalf("Problem required fields mismatch: %#v", problem)
	}
	if len(problem.Errors) != 1 || problem.Errors[0].Pointer != "/deviceKind" ||
		problem.Errors[0].Code != "invalid_value" || problem.Errors[0].Message != "value is not in the allowed enum" {
		t.Fatalf("field error mismatch: %#v", problem.Errors)
	}
	if strings.Contains(response.Body.String(), "BODY_SECRET") || strings.Contains(response.Body.String(), "invalid-kind") {
		t.Fatalf("Problem leaked submitted values: %s", response.Body.String())
	}
}

func TestWriteNativeDeviceInputProblemMapsStructureErrorsToSafe400(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "malformed JSON", err: decodeCreateInputError(`{"secret":"BODY_SECRET"`)},
		{name: "non-object", err: decodeCreateInputError(`"BODY_SECRET"`)},
		{name: "trailing JSON", err: decodeUpdateInputError(`{} {"secret":"BODY_SECRET"}`)},
		{name: "duplicate property", err: decodeCreateInputError(`{"name":"BODY_SECRET","name":"other","deviceNumber":"pump-1","deviceKind":"standalone"}`)},
		{name: "nested duplicate property", err: decodeUpdateInputError(`{"labels":{"zone":"BODY_SECRET","zone":"other"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("expected structural input error")
			}
			response := httptest.NewRecorder()
			writeNativeDeviceInputProblem(response, "request-400", "/api/v2/devices", tc.err)
			if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != "application/problem+json" ||
				response.Header().Get("X-Request-Id") != "request-400" {
				t.Fatalf("400 response metadata mismatch: status=%d headers=%#v body=%s", response.Code, response.Header(), response.Body.String())
			}
			if strings.Contains(response.Body.String(), "BODY_SECRET") || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("400 Problem leaked request content: %s", response.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode Problem: %v", err)
			}
			if string(body["status"]) != "400" || string(body["code"]) != `"invalid_request"` || body["errors"] != nil {
				t.Fatalf("structural Problem should use safe constant shape: %s", response.Body.String())
			}
		})
	}
}

func TestNativeDeviceInputPointersEscapeAndBoundUnknownFields(t *testing.T) {
	if got := escapeNativeJSONPointerSegment("a/~b"); got != "a~1~0b" {
		t.Fatalf("RFC6901 segment escaping = %q", got)
	}
	if got, ok := nativeDeviceInputJSONPointer("directAccess.access.credentialPolicy.autoRegister"); !ok || got != "/directAccess/access/credentialPolicy/autoRegister" {
		t.Fatalf("nested schema pointer = %q, ok=%v", got, ok)
	}
	malicious := strings.Repeat("secret~/", 1000)
	input := `{"name":"Pump","deviceNumber":"pump-1","deviceKind":"standalone","` + malicious + `":"BODY_SECRET"}`
	err := decodeCreateInputError(input)
	if err == nil {
		t.Fatal("expected unknown property error")
	}
	response := httptest.NewRecorder()
	writeNativeDeviceInputProblem(response, "request-422", "/api/v2/devices", err)
	if response.Code != http.StatusUnprocessableEntity || len(response.Body.Bytes()) > 2048 ||
		strings.Contains(response.Body.String(), malicious) || strings.Contains(response.Body.String(), "BODY_SECRET") {
		t.Fatalf("unknown property was echoed or response was unbounded: status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Errors []nativeDeviceInputProblemItem `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 || body.Errors[0].Pointer != "/" {
		t.Fatalf("unknown property should map to a safe root pointer: errors=%#v err=%v", body.Errors, err)
	}
}

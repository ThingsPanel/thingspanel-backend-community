package devicemetadata

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// NativeDeviceInputError preserves a stable field path and safe validation
// reason for translating local request errors into the native Problem shape.
type NativeDeviceInputError struct {
	Field      string
	Reason     string
	Structural bool
}

func (e *NativeDeviceInputError) Error() string {
	if e == nil {
		return "invalid device input"
	}
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + ": " + e.Reason
}

type nativeDeviceInputProblem struct {
	Type      string                         `json:"type"`
	Title     string                         `json:"title"`
	Status    int                            `json:"status"`
	Detail    string                         `json:"detail"`
	Instance  string                         `json:"instance"`
	Code      string                         `json:"code"`
	RequestID string                         `json:"requestId"`
	Errors    []nativeDeviceInputProblemItem `json:"errors,omitempty"`
}

type nativeDeviceInputProblemItem struct {
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

var nativeDeviceInputSchemaPaths = map[string]struct{}{
	"name": {}, "deviceNumber": {}, "deviceKind": {}, "productId": {}, "description": {},
	"labels": {}, "location": {}, "location.latitude": {}, "location.longitude": {}, "location.address": {},
	"templateId": {}, "gatewayId": {}, "subdeviceAddress": {}, "initialAdministrativeState": {},
	"tags": {}, "customFields": {}, "directAccess": {}, "directAccess.access": {},
	"directAccess.externalDeviceId": {}, "directAccess.access.mode": {}, "directAccess.access.protocol": {},
	"directAccess.access.providerId": {}, "directAccess.access.providerVersion": {},
	"directAccess.access.schemaDigest": {}, "directAccess.access.parameters": {},
	"directAccess.access.credentialPolicy": {}, "directAccess.access.credentialPolicy.kind": {},
	"directAccess.access.credentialPolicy.autoRegister": {}, "directAccess.access.onlinePolicy": {},
	"directAccess.access.onlinePolicy.mode": {}, "directAccess.access.onlinePolicy.timeoutSeconds": {},
	"directAccess.access.connectionDirection": {}, "directAccess.access.serviceInstanceId": {},
	"initialGroupIds": {}, "batchNumber": {}, "deviceParameters": {},
}

// writeNativeDeviceInputProblem emits only safe, OAS-shaped descriptions. It
// never includes a request body, decoder detail, or an unrecognized property.
func writeNativeDeviceInputProblem(w http.ResponseWriter, requestID, instance string, err error) {
	writeNativeMetadataInputProblem(w, requestID, instance, "device", nativeDeviceInputSchemaPaths, err)
}

var nativeTemplateInputSchemaPaths = map[string]struct{}{
	"name": {}, "description": {}, "brand": {}, "productName": {}, "modelNumber": {},
	"tags": {}, "configuration": {}, "configuration.deviceKind": {}, "configuration.access": {}, "configuration.topicMappings": {},
	"administrativeState": {}, "imageAssetId": {},
}

func writeNativeMetadataInputProblem(w http.ResponseWriter, requestID, instance, resource string, paths map[string]struct{}, err error) {
	if requestID == "" {
		requestID = newID()
	}
	if len(requestID) > 128 {
		requestID = requestID[:128]
	}
	if instance == "" {
		instance = "/"
	}
	if len(instance) > 2048 {
		instance = instance[:2048]
	}
	problem := nativeDeviceInputProblem{
		Type: "about:blank", Instance: instance, RequestID: requestID,
	}
	var inputErr *NativeDeviceInputError
	if !errors.As(err, &inputErr) || inputErr.Structural {
		problem.Status = http.StatusBadRequest
		problem.Title = http.StatusText(problem.Status)
		problem.Detail = "request body must be a valid JSON object"
		problem.Code = "invalid_request"
	} else {
		problem.Status = http.StatusUnprocessableEntity
		problem.Title = http.StatusText(problem.Status)
		problem.Detail = "one or more " + resource + " fields are invalid"
		problem.Code = "invalid_" + resource + "_field"
		pointer, known := nativeMetadataInputJSONPointer(inputErr.Field, paths)
		message := inputErr.Reason
		if !known {
			message = "contains an invalid property"
		}
		if len(message) > 512 {
			message = message[:512]
		}
		problem.Errors = []nativeDeviceInputProblemItem{{Pointer: pointer, Code: "invalid_value", Message: message}}
	}
	body, marshalErr := json.Marshal(problem)
	if marshalErr != nil {
		body = []byte(`{"type":"about:blank","title":"Bad Request","status":400,"detail":"request body must be a valid JSON object","instance":"/","code":"invalid_request","requestId":"invalid-request"}`)
		problem.Status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Request-Id", requestID)
	w.WriteHeader(problem.Status)
	_, _ = w.Write(body)
}

func nativeDeviceInputJSONPointer(field string) (string, bool) {
	return nativeMetadataInputJSONPointer(field, nativeDeviceInputSchemaPaths)
}

func nativeMetadataInputJSONPointer(field string, paths map[string]struct{}) (string, bool) {
	if field == "$" || field == "" {
		return "/", false
	}
	if _, ok := paths[field]; !ok {
		return "/", false
	}
	parts := strings.Split(field, ".")
	for i, part := range parts {
		parts[i] = escapeNativeJSONPointerSegment(part)
	}
	return "/" + strings.Join(parts, "/"), true
}

func escapeNativeJSONPointerSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	return strings.ReplaceAll(segment, "/", "~1")
}

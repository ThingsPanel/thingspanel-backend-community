package devicemetadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/internal/communityclient"
	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Debug sessions over the Community per-device debug switch.
//
// Community keeps one switch per device (enabled + expire_at) and an unstructured log
// list. A native session therefore is that switch: its id is derived from the device
// and the expiry (so it can be verified without storing anything), enabling again
// replaces the previous session, and raw device payloads are never requested or
// returned. Log levels are derived from the Community outcome; "debug" level sessions
// are refused because Community cannot record a different verbosity.

const (
	debugDefaultSeconds = 300
	debugMaxSeconds     = 604800
)

type NativeDebugSession struct {
	ID        string `json:"id"`
	DeviceID  string `json:"deviceId"`
	ExpiresAt string `json:"expiresAt"`
	State     string `json:"state"`
}

type NativeDebugSessionResponse struct {
	Data NativeDebugSession `json:"data"`
	Meta RequestMeta        `json:"meta"`
}

type NativeDebugLog struct {
	ID        string  `json:"id"`
	DeviceID  string  `json:"deviceId"`
	Timestamp string  `json:"timestamp"`
	Direction string  `json:"direction"`
	Level     string  `json:"level"`
	Message   string  `json:"message"`
	RequestID *string `json:"requestId"`
}

type NativeDebugLogPage struct {
	Items    []NativeDebugLog `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
	Total    int64            `json:"total"`
}

type NativeDebugLogPageResponse struct {
	Data NativeDebugLogPage `json:"data"`
	Meta RequestMeta        `json:"meta"`
}

func debugSessionID(deviceID string, expireAt int64) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("tp-debug-session:"+deviceID+":"+strconv.FormatInt(expireAt, 10))).String()
}

func decodeNativeDebugSessionRequest(body []byte) (int, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return 0, invalidStructure("request body must be a JSON object")
	}
	seconds := debugDefaultSeconds
	for key, value := range raw {
		switch key {
		case "durationSeconds":
			if json.Unmarshal(value, &seconds) != nil || seconds < 1 || seconds > debugMaxSeconds {
				return 0, invalid("durationSeconds", "durationSeconds must be an integer from 1 to 604800")
			}
		case "level":
			var level string
			if json.Unmarshal(value, &level) != nil || (level != "info" && level != "debug") {
				return 0, invalid("level", "level must be info or debug")
			}
			if level == "debug" {
				return 0, &errs.Error{Code: errs.Unavailable, Message: "debug level sessions are not supported by the Community runtime; use info"}
			}
		default:
			return 0, invalid(key, "unknown field")
		}
	}
	if _, ok := raw["level"]; !ok {
		return 0, invalid("level", "field is required")
	}
	return seconds, nil
}

// debugLogFromEntry converts a Community entry. The message never carries the raw
// payload; level is derived from the Community outcome.
func debugLogFromEntry(deviceID string, entry communityclient.DebugEntry) NativeDebugLog {
	sum := sha256.Sum256([]byte(entry.Raw))
	level := "info"
	switch strings.ToLower(strings.TrimSpace(entry.Outcome)) {
	case "error":
		level = "error"
	case "deny", "drop":
		level = "warning"
	}
	direction := entry.Direction
	if direction != "uplink" && direction != "downlink" {
		direction = "internal"
	}
	parts := []string{}
	if entry.Action != "" {
		parts = append(parts, entry.Action)
	}
	if entry.Outcome != "" {
		parts = append(parts, entry.Outcome)
	}
	message := strings.Join(parts, " ")
	if entry.Error != "" {
		message += ": " + entry.Error
	}
	if message == "" {
		message = "(no detail)"
	}
	if utf8.RuneCountInString(message) > 500 {
		message = string([]rune(message)[:500])
	}
	return NativeDebugLog{
		ID: "legacy-debug:" + hex.EncodeToString(sum[:16]), DeviceID: deviceID,
		Timestamp: rfc3339Millis(entry.Time), Direction: direction, Level: level, Message: message,
	}
}

func debugSessionLocation(deviceID, sessionID string) string {
	return "/api/v2/devices/" + deviceID + "/debug-sessions/" + sessionID
}

func createNativeDebugSession(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	seconds, err := decodeNativeDebugSessionRequest(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "createDeviceDebugSession",
		Method: http.MethodPost, Path: "/api/v2/devices/" + deviceID + "/debug-sessions", Key: key,
		RequestID: requestID, CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		legacyID, err := resolveRuntimeDeviceID(device.RuntimeRef, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		expireAt := time.Now().Unix() + int64(seconds)
		state, err := nativeRuntimeClient().SetDebug(ctx, legacyID, true, expireAt)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, runtimeUpstreamError(err)
		}
		if !state.Enabled || state.ExpireAt != expireAt {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "device runtime did not confirm the debug session"}
		}
		sessionID := debugSessionID(deviceID, expireAt)
		encoded, err := json.Marshal(NativeDebugSessionResponse{
			Data: NativeDebugSession{ID: sessionID, DeviceID: deviceID, ExpiresAt: rfc3339Millis(time.Unix(expireAt, 0)), State: "active"},
			Meta: RequestMeta{RequestID: requestID},
		})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"debugSessionId": sessionID, "durationSeconds": seconds})
		version := device.ResourceVersion
		return metadataWriteResponse{Status: http.StatusCreated, Body: encoded, Location: debugSessionLocation(deviceID, sessionID)},
			metadataWriteAudit{ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &version, AfterVersion: &version, RedactedChanges: changes}, nil
	})
	if err != nil {
		return nil, mapNativeDeviceWriteError(err)
	}
	return result, nil
}

func stopNativeDebugSession(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, sessionID, key, requestID string) (*metadataWriteResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "stopDeviceDebugSession",
		Method: http.MethodDelete, Path: debugSessionLocation(deviceID, sessionID), Key: key,
		RequestID: requestID, CanonicalBody: []byte("null"),
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		device, err := loadNativeDeviceEncoreTx(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		legacyID, err := resolveRuntimeDeviceID(device.RuntimeRef, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		current, err := nativeRuntimeClient().GetDebugState(ctx, legacyID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, runtimeUpstreamError(err)
		}
		if current.ExpireAt == 0 || debugSessionID(deviceID, current.ExpireAt) != sessionID {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "debug session not found"}
		}
		state := "expired"
		if current.Enabled && time.Now().Unix() <= current.ExpireAt {
			off, err := nativeRuntimeClient().SetDebug(ctx, legacyID, false, 0)
			if err != nil {
				return metadataWriteResponse{}, metadataWriteAudit{}, runtimeUpstreamError(err)
			}
			if off.Enabled {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.Unavailable, Message: "device runtime did not confirm the debug session stop"}
			}
			state = "stopped"
		}
		encoded, err := json.Marshal(NativeDebugSessionResponse{
			Data: NativeDebugSession{ID: sessionID, DeviceID: deviceID, ExpiresAt: rfc3339Millis(time.Unix(current.ExpireAt, 0)), State: state},
			Meta: RequestMeta{RequestID: requestID},
		})
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"debugSessionId": sessionID, "state": state})
		version := device.ResourceVersion
		return metadataWriteResponse{Status: http.StatusOK, Body: encoded},
			metadataWriteAudit{ResourceKind: "device", ResourceID: deviceID, BeforeVersion: &version, AfterVersion: &version, RedactedChanges: changes}, nil
	})
	if err != nil {
		return nil, mapNativeDeviceWriteError(err)
	}
	return result, nil
}

var nativeDebugLogKeys = []string{"page", "pageSize", "from", "to", "direction"}

func listNativeDebugLogs(ctx context.Context, identity nativeDeviceReadIdentity, deviceID string, page, pageSize int, requestID string) (*NativeDebugLogPageResponse, error) {
	legacyID, err := loadRuntimeDeviceID(ctx, identity, deviceID)
	if err != nil {
		return nil, err
	}
	rows, total, err := nativeRuntimeClient().ListDebugLogs(ctx, legacyID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, runtimeUpstreamError(err)
	}
	out := NativeDebugLogPage{Items: []NativeDebugLog{}, Page: page, PageSize: pageSize, Total: total}
	for _, row := range rows {
		out.Items = append(out.Items, debugLogFromEntry(deviceID, row))
	}
	return &NativeDebugLogPageResponse{Data: out, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/debug-sessions
func (s *Service) CreateNativeDeviceDebugSession(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := nativeDeviceWriteBody(w, r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := createNativeDebugSession(r.Context(), identity, deviceID, key, body, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=DELETE path=/api/v2/devices/:deviceID/debug-sessions/:sessionID
func (s *Service) StopNativeDeviceDebugSession(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	sessionID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("sessionID"))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	key, err := nativeDeviceWriteKey(r)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := stopNativeDebugSession(r.Context(), identity, deviceID, sessionID, key, requestID)
	if err != nil {
		writeNativeDeviceOperationError(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/debug-logs
func (s *Service) ListNativeDeviceDebugLogs(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceControlIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	values, err := parseNativeReadValues(r, nativeDebugLogKeys)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	if values.Has("from") || values.Has("to") || values.Has("direction") {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "time range and direction filters are not supported by the Community debug log list"}, 0, true)
		return
	}
	page, pageSize, err := normalizeDeviceGroupPagination(values.Get("page"), values.Get("pageSize"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	response, err := listNativeDebugLogs(r.Context(), identity.nativeDeviceReadIdentity, deviceID, page, pageSize, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

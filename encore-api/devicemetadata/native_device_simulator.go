package devicemetadata

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"encore.app/internal/authz"
	"encore.app/internal/legacy"
	"encore.app/internal/nativehttp"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Controlled device simulator (DM-RT-019). Settings, sessions and run records
// live here; the publish itself is delegated to Community, which sends to its
// own configured broker as the device. A run is synchronous, so it is capped at
// simulatorMaxSyncMessages and sends back to back without honouring
// intervalSeconds; the profile still stores the interval for later pacing.

const (
	simulatorMaxSyncMessages = 20
	simulatorDefaultTTL      = 300
	simulatorProfileETagFmt  = `"sim-profile-%d"`
)

type SimulatorProfile struct {
	DeviceID        string         `json:"deviceId"`
	ResourceVersion int64          `json:"resourceVersion"`
	Kind            string         `json:"kind"`
	Payload         map[string]any `json:"payload"`
	TopicPurpose    string         `json:"topicPurpose"`
	IntervalSeconds int            `json:"intervalSeconds"`
	MessageLimit    int            `json:"messageLimit"`
}

type SimulationSession struct {
	ID        string `json:"id"`
	DeviceID  string `json:"deviceId"`
	ExpiresAt string `json:"expiresAt"`
	State     string `json:"state"`
}

type SimulationRun struct {
	ID        string          `json:"id"`
	DeviceID  string          `json:"deviceId"`
	State     string          `json:"state"`
	Sent      int             `json:"sent"`
	Failed    int             `json:"failed"`
	CreatedAt string          `json:"createdAt"`
	Error     json.RawMessage `json:"error"`
}

type simulatorEnvelope struct {
	Data any         `json:"data"`
	Meta RequestMeta `json:"meta"`
}

type updateSimulatorProfileInput struct {
	Kind            string         `json:"kind"`
	Payload         map[string]any `json:"payload"`
	TopicPurpose    string         `json:"topicPurpose"`
	IntervalSeconds *int           `json:"intervalSeconds"`
	MessageLimit    *int           `json:"messageLimit"`
}

func decodeStrict(body []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return invalidStructure("request body is not valid for this operation")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalidStructure("request body has trailing data")
	}
	return nil
}

func decodeUpdateSimulatorProfile(body []byte) (updateSimulatorProfileInput, error) {
	var in updateSimulatorProfileInput
	if err := decodeStrict(body, &in); err != nil {
		return in, err
	}
	oneOf := func(v string) bool { return v == "telemetry" || v == "attribute" || v == "event" }
	switch {
	case !oneOf(in.Kind) || !oneOf(in.TopicPurpose):
		return in, invalidStructure("kind and topicPurpose must be telemetry, attribute or event")
	case in.Payload == nil:
		return in, invalidStructure("payload must be an object")
	case in.IntervalSeconds == nil || *in.IntervalSeconds < 1 || *in.IntervalSeconds > 3600:
		return in, invalidStructure("intervalSeconds must be 1 to 3600")
	case in.MessageLimit == nil || *in.MessageLimit < 1 || *in.MessageLimit > 1000:
		return in, invalidStructure("messageLimit must be 1 to 1000")
	}
	return in, nil
}

func simulatorETag(version int64) string { return fmt.Sprintf(simulatorProfileETagFmt, version) }

func simulatorBody(data any, requestID string) (json.RawMessage, error) {
	return json.Marshal(simulatorEnvelope{Data: data, Meta: RequestMeta{RequestID: requestID}})
}

type simulatorQuerier interface {
	QueryRow(ctx context.Context, query string, args ...any) *sqldb.Row
}

func loadSimulatorProfile(ctx context.Context, q simulatorQuerier, tenantID, deviceID string) (*SimulatorProfile, error) {
	var p SimulatorProfile
	var payload []byte
	err := q.QueryRow(ctx, `SELECT device_id::text,resource_version,kind,payload,topic_purpose,interval_seconds,message_limit
		FROM device_simulator_profiles WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID).
		Scan(&p.DeviceID, &p.ResourceVersion, &p.Kind, &payload, &p.TopicPurpose, &p.IntervalSeconds, &p.MessageLimit)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "no simulator profile has been saved for this device"}
	}
	if err != nil {
		return nil, wrapDBErr(err, "read simulator profile failed")
	}
	if err := json.Unmarshal(payload, &p.Payload); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "stored simulator payload is invalid"}
	}
	return &p, nil
}

func requireSimulatorDevice(ctx context.Context, tenantID, deviceID string) error {
	var one int
	err := db.QueryRow(ctx, `SELECT 1 FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, deviceID).Scan(&one)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	return wrapDBErr(err, "read device failed")
}

func authorizeSimulator(identity nativeDeviceReadIdentity, deviceID string) error {
	if !identity.permits(deviceID) {
		return &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	if !authz.Can(authn.CurrentData(), "device", "simulate") {
		return &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to use the device simulator"}
	}
	return nil
}

func readSimulatorProfile(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*metadataWriteResponse, error) {
	if err := authorizeSimulator(identity, deviceID); err != nil {
		return nil, err
	}
	profile, err := loadSimulatorProfile(ctx, db, identity.tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	body, err := simulatorBody(profile, requestID)
	if err != nil {
		return nil, err
	}
	return &metadataWriteResponse{Status: http.StatusOK, Body: body, ETag: simulatorETag(profile.ResourceVersion), RequestID: requestID}, nil
}

func updateSimulatorProfile(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key, ifMatch string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if err := authorizeSimulator(identity.nativeDeviceReadIdentity, deviceID); err != nil {
		return nil, err
	}
	input, err := decodeUpdateSimulatorProfile(body)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "updateDeviceSimulatorProfile",
		Method: http.MethodPut, Path: "/api/v2/devices/" + deviceID + "/simulator-profile", Key: key, RequestID: requestID,
		IfMatch: ifMatch, CanonicalBody: canonical,
	}
	payload, _ := json.Marshal(input.Payload)
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var device string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
			identity.tenantID, deviceID).Scan(&device); err != nil {
			if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
				return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "device not found"}
			}
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		var current int64
		err := tx.QueryRow(ctx, `SELECT resource_version FROM device_simulator_profiles WHERE tenant_id=$1 AND device_id=$2 FOR UPDATE`,
			identity.tenantID, deviceID).Scan(&current)
		if err != nil && !errors.Is(err, sqldb.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		// A missing profile has version 0, so the first save sends If-Match "sim-profile-0".
		if simulatorETag(current) != ifMatch {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "simulator profile has changed; read it again before saving"}
		}
		next := current + 1
		if current == 0 {
			_, err = tx.Exec(ctx, `INSERT INTO device_simulator_profiles(tenant_id,device_id,resource_version,kind,payload,topic_purpose,interval_seconds,message_limit,updated_by)
				VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`, identity.tenantID, deviceID, next, input.Kind, payload, input.TopicPurpose,
				*input.IntervalSeconds, *input.MessageLimit, identity.subjectID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE device_simulator_profiles SET resource_version=$3,kind=$4,payload=$5::jsonb,topic_purpose=$6,
				interval_seconds=$7,message_limit=$8,updated_by=$9,updated_at=NOW() WHERE tenant_id=$1 AND device_id=$2`,
				identity.tenantID, deviceID, next, input.Kind, payload, input.TopicPurpose, *input.IntervalSeconds, *input.MessageLimit, identity.subjectID)
		}
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		profile := SimulatorProfile{DeviceID: deviceID, ResourceVersion: next, Kind: input.Kind, Payload: input.Payload,
			TopicPurpose: input.TopicPurpose, IntervalSeconds: *input.IntervalSeconds, MessageLimit: *input.MessageLimit}
		encoded, err := simulatorBody(profile, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"kind": input.Kind, "topicPurpose": input.TopicPurpose,
			"intervalSeconds": *input.IntervalSeconds, "messageLimit": *input.MessageLimit})
		return metadataWriteResponse{Status: http.StatusOK, Body: encoded, ETag: simulatorETag(next), RequestID: requestID},
			metadataWriteAudit{ResourceKind: "device_simulator_profile", ResourceID: deviceID, BeforeVersion: &current, AfterVersion: &next, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

type simulationSessionInput struct {
	ProfileVersion *int64 `json:"profileVersion"`
	TTLSeconds     *int   `json:"ttlSeconds"`
}

func createSimulationSession(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if err := authorizeSimulator(identity.nativeDeviceReadIdentity, deviceID); err != nil {
		return nil, err
	}
	var input simulationSessionInput
	if err := decodeStrict(body, &input); err != nil {
		return nil, err
	}
	ttl := simulatorDefaultTTL
	if input.TTLSeconds != nil {
		ttl = *input.TTLSeconds
	}
	if input.ProfileVersion == nil || *input.ProfileVersion < 1 || ttl < 60 || ttl > 3600 {
		return nil, invalidStructure("profileVersion is required and ttlSeconds must be 60 to 3600")
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "createDeviceSimulationSession",
		Method: http.MethodPost, Path: "/api/v2/devices/" + deviceID + "/simulator-sessions", Key: key, RequestID: requestID,
		CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		profile, err := loadSimulatorProfile(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if profile.ResourceVersion != *input.ProfileVersion {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "profileVersion is not the saved version; read the profile again"}
		}
		id := uuid.NewString()
		expires := time.Now().UTC().Add(time.Duration(ttl) * time.Second)
		if _, err := tx.Exec(ctx, `INSERT INTO device_simulation_sessions(id,tenant_id,device_id,profile_version,expires_at,created_by)
			VALUES($1,$2,$3,$4,$5,$6)`, id, identity.tenantID, deviceID, profile.ResourceVersion, expires, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		encoded, err := simulatorBody(SimulationSession{ID: id, DeviceID: deviceID, ExpiresAt: expires.Format("2006-01-02T15:04:05.000Z"), State: "ready"}, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"profileVersion": profile.ResourceVersion, "ttlSeconds": ttl})
		return metadataWriteResponse{Status: http.StatusCreated, Body: encoded, RequestID: requestID,
				Location: "/api/v2/devices/" + deviceID + "/simulator-sessions/" + id},
			metadataWriteAudit{ResourceKind: "device_simulation_session", ResourceID: id, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func closeSimulationSession(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, sessionID, requestID string) (*metadataWriteResponse, error) {
	if err := authorizeSimulator(identity.nativeDeviceReadIdentity, deviceID); err != nil {
		return nil, err
	}
	var expires time.Time
	var state string
	err := db.QueryRow(ctx, `UPDATE device_simulation_sessions SET state='closed',closed_at=COALESCE(closed_at,NOW())
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3 RETURNING expires_at,state`, identity.tenantID, deviceID, sessionID).Scan(&expires, &state)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "simulation session not found"}
	}
	if err != nil {
		return nil, wrapDBErr(err, "close simulation session failed")
	}
	body, err := simulatorBody(SimulationSession{ID: sessionID, DeviceID: deviceID, ExpiresAt: expires.UTC().Format("2006-01-02T15:04:05.000Z"), State: state}, requestID)
	if err != nil {
		return nil, err
	}
	return &metadataWriteResponse{Status: http.StatusOK, Body: body, RequestID: requestID}, nil
}

// simulationSender publishes one message as the device. Tests replace it.
type simulationSender interface {
	Send(ctx context.Context, tenantID, deviceID, dataJSON string) error
}

type communitySimulationSender struct{}

func (communitySimulationSender) Send(ctx context.Context, tenantID, deviceID, dataJSON string) error {
	legacyID, err := lookupRuntimeDeviceID(ctx, tenantID, deviceID)
	if err != nil {
		return err
	}
	return nativeRuntimeLegacy().SimulationSend(ctx, legacyID, dataJSON)
}

var activeSimulationSender simulationSender = communitySimulationSender{}

type simulationRunInput struct {
	SessionID    string `json:"sessionId"`
	MessageCount *int   `json:"messageCount"`
}

func runDeviceSimulation(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key string, body []byte, requestID string) (*metadataWriteResponse, error) {
	if err := authorizeSimulator(identity.nativeDeviceReadIdentity, deviceID); err != nil {
		return nil, err
	}
	var input simulationRunInput
	if err := decodeStrict(body, &input); err != nil {
		return nil, err
	}
	if _, err := nativeReadUUID(input.SessionID); err != nil {
		return nil, invalidStructure("sessionId must be a UUID")
	}
	count := 1
	if input.MessageCount != nil {
		count = *input.MessageCount
	}
	if count < 1 || count > 1000 {
		return nil, invalidStructure("messageCount must be 1 to 1000")
	}
	if count > simulatorMaxSyncMessages {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "runs are synchronous and limited to " + strconv.Itoa(simulatorMaxSyncMessages) + " messages"}
	}
	canonical, err := canonicalNativeWriteBody(body)
	if err != nil {
		return nil, invalidStructure("request body must be a valid JSON object")
	}
	command := metadataWriteCommand{
		TenantID: identity.tenantID, SubjectID: identity.subjectID, Operation: "runDeviceSimulation",
		Method: http.MethodPost, Path: "/api/v2/devices/" + deviceID + "/simulation-runs", Key: key, RequestID: requestID,
		CanonicalBody: canonical,
	}
	result, err := runMetadataWriteTx(ctx, command, func(tx *sqldb.Tx) (metadataWriteResponse, metadataWriteAudit, error) {
		var state string
		var expires time.Time
		var version int64
		err := tx.QueryRow(ctx, `SELECT state,expires_at,profile_version FROM device_simulation_sessions
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3 FOR UPDATE`, identity.tenantID, deviceID, input.SessionID).Scan(&state, &expires, &version)
		if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.NotFound, Message: "simulation session not found"}
		}
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if state != "ready" || !expires.After(time.Now()) {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "simulation session is closed or expired"}
		}
		profile, err := loadSimulatorProfile(ctx, tx, identity.tenantID, deviceID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if profile.ResourceVersion != version {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.FailedPrecondition, Message: "the profile changed after this session was created; create a new session"}
		}
		if count > profile.MessageLimit {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.InvalidArgument, Message: "messageCount exceeds the profile messageLimit"}
		}
		if profile.TopicPurpose != "telemetry" || profile.Kind != "telemetry" {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.InvalidArgument, Message: "the Community runtime adapter can simulate telemetry only"}
		}
		data, _ := json.Marshal(profile.Payload)
		sent, failed := 0, 0
		var firstFailure string
		for i := 0; i < count; i++ {
			if err := activeSimulationSender.Send(ctx, identity.tenantID, deviceID, string(data)); err != nil {
				failed++
				if firstFailure == "" {
					firstFailure = "RUNTIME_REJECTED"
					if !errors.Is(err, legacy.ErrRejected) {
						firstFailure = "RUNTIME_UNAVAILABLE"
					}
				}
				continue
			}
			sent++
		}
		runState := "succeeded"
		var problem []byte
		if failed > 0 {
			runState = "failed"
			problem, _ = json.Marshal(map[string]any{"type": "about:blank", "title": "Simulated messages were not all accepted",
				"status": 502, "detail": strconv.Itoa(failed) + " of " + strconv.Itoa(count) + " messages were not accepted by the runtime.",
				"instance": "/api/v2/devices/" + deviceID + "/simulation-runs", "code": firstFailure, "requestId": requestID})
		}
		runID := uuid.NewString()
		created := time.Now().UTC()
		if _, err := tx.Exec(ctx, `INSERT INTO device_simulation_runs(id,tenant_id,device_id,session_id,state,sent,failed,error,created_at,created_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)`, runID, identity.tenantID, deviceID, input.SessionID, runState, sent, failed,
			nullableJSON(problem), created, identity.subjectID); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		runErr := json.RawMessage("null")
		if problem != nil {
			runErr = problem
		}
		encoded, err := simulatorBody(SimulationRun{ID: runID, DeviceID: deviceID, State: runState, Sent: sent, Failed: failed,
			CreatedAt: created.Format("2006-01-02T15:04:05.000Z"), Error: runErr}, requestID)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		changes, _ := json.Marshal(map[string]any{"messageCount": count, "sent": sent, "failed": failed})
		return metadataWriteResponse{Status: http.StatusCreated, Body: encoded, RequestID: requestID,
				Location: "/api/v2/devices/" + deviceID + "/simulation-runs/" + runID},
			metadataWriteAudit{ResourceKind: "device_simulation_run", ResourceID: runID, RedactedChanges: changes}, nil
	})
	return result, mapNativeDeviceWriteError(err)
}

func nullableJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

func readSimulationRun(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, runID, requestID string) (*metadataWriteResponse, error) {
	if err := authorizeSimulator(identity, deviceID); err != nil {
		return nil, err
	}
	var run SimulationRun
	var created time.Time
	var problem []byte
	err := db.QueryRow(ctx, `SELECT id::text,device_id::text,state,sent,failed,created_at,error FROM device_simulation_runs
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3`, identity.tenantID, deviceID, runID).
		Scan(&run.ID, &run.DeviceID, &run.State, &run.Sent, &run.Failed, &created, &problem)
	if errors.Is(err, sqldb.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "simulation run not found"}
	}
	if err != nil {
		return nil, wrapDBErr(err, "read simulation run failed")
	}
	run.CreatedAt = created.UTC().Format("2006-01-02T15:04:05.000Z")
	run.Error = json.RawMessage("null")
	if len(problem) > 0 {
		run.Error = problem
	}
	body, err := simulatorBody(run, requestID)
	if err != nil {
		return nil, err
	}
	return &metadataWriteResponse{Status: http.StatusOK, Body: body, RequestID: requestID}, nil
}

// ---- HTTP ----

func simulatorPathUUID(name string) (string, error) {
	return nativeReadUUID(encore.CurrentRequest().PathParams.Get(name))
}

func writeSimulatorProblem(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	writeNativeDeviceOperationError(w, r, requestID, err)
}

func serveSimulatorWrite(w http.ResponseWriter, r *http.Request, needIfMatch bool,
	run func(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key, ifMatch string, body []byte, requestID string) (*metadataWriteResponse, error)) {
	requestID := newID()
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := simulatorPathUUID("deviceID")
	if err != nil {
		writeSimulatorProblem(w, r, requestID, err)
		return
	}
	ifMatch := ""
	if needIfMatch {
		values := r.Header.Values("If-Match")
		if len(values) != 1 || !strings.HasPrefix(values[0], `"sim-profile-`) {
			writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match with the simulator profile ETag is required"}, http.StatusPreconditionRequired)
			return
		}
		ifMatch = values[0]
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
	result, err := run(r.Context(), identity, deviceID, key, ifMatch, body, requestID)
	if err != nil {
		writeSimulatorProblem(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

func serveSimulatorRead(w http.ResponseWriter, r *http.Request, run func(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*metadataWriteResponse, error)) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := simulatorPathUUID("deviceID")
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	result, err := run(r.Context(), identity, deviceID, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/simulator-profile
func (s *Service) GetDeviceSimulatorProfile(w http.ResponseWriter, r *http.Request) {
	serveSimulatorRead(w, r, func(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*metadataWriteResponse, error) {
		return readSimulatorProfile(ctx, identity, deviceID, requestID)
	})
}

//encore:api auth raw method=PUT path=/api/v2/devices/:deviceID/simulator-profile
func (s *Service) UpdateDeviceSimulatorProfile(w http.ResponseWriter, r *http.Request) {
	serveSimulatorWrite(w, r, true, updateSimulatorProfile)
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/simulator-sessions
func (s *Service) CreateDeviceSimulationSession(w http.ResponseWriter, r *http.Request) {
	serveSimulatorWrite(w, r, false, func(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key, _ string, body []byte, requestID string) (*metadataWriteResponse, error) {
		return createSimulationSession(ctx, identity, deviceID, key, body, requestID)
	})
}

//encore:api auth raw method=DELETE path=/api/v2/devices/:deviceID/simulator-sessions/:sessionID
func (s *Service) CloseDeviceSimulationSession(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := simulatorPathUUID("deviceID")
	var sessionID string
	if err == nil {
		sessionID, err = simulatorPathUUID("sessionID")
	}
	if err != nil {
		writeSimulatorProblem(w, r, requestID, err)
		return
	}
	result, err := closeSimulationSession(r.Context(), identity, deviceID, sessionID, requestID)
	if err != nil {
		writeSimulatorProblem(w, r, requestID, err)
		return
	}
	writeNativeDeviceMutationResponse(w, result)
}

//encore:api auth raw method=POST path=/api/v2/devices/:deviceID/simulation-runs
func (s *Service) RunDeviceSimulation(w http.ResponseWriter, r *http.Request) {
	serveSimulatorWrite(w, r, false, func(ctx context.Context, identity nativeDeviceWriteIdentity, deviceID, key, _ string, body []byte, requestID string) (*metadataWriteResponse, error) {
		return runDeviceSimulation(ctx, identity, deviceID, key, body, requestID)
	})
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/simulation-runs/:runID
func (s *Service) GetDeviceSimulationRun(w http.ResponseWriter, r *http.Request) {
	serveSimulatorRead(w, r, func(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*metadataWriteResponse, error) {
		runID, err := simulatorPathUUID("runID")
		if err != nil {
			return nil, err
		}
		return readSimulationRun(ctx, identity, deviceID, runID, requestID)
	})
}

// NativeSimulatorConfig is the read-only runtime configuration a simulator
// needs. Topics stay empty until the runtime owner publishes per-purpose topics
// and fixed-model sample payloads; nothing is guessed. No secret is included.
type NativeSimulatorConfig struct {
	DeviceID     string               `json:"deviceId"`
	Connection   NativeConnectionInfo `json:"connection"`
	Topics       []struct{}           `json:"topics"`
	ModelBinding *ThingModelBinding   `json:"modelBinding"`
}

type NativeSimulatorConfigResponse struct {
	Data NativeSimulatorConfig `json:"data"`
	Meta RequestMeta           `json:"meta"`
}

func readNativeSimulatorConfig(ctx context.Context, identity nativeDeviceReadIdentity, canDiagnose bool, deviceID, requestID string) (*NativeSimulatorConfigResponse, error) {
	if !canDiagnose {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read simulator configuration"}
	}
	connection, err := readNativeConnectionInfo(ctx, identity, deviceID, requestID)
	if err != nil {
		return nil, err
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read simulator configuration failed")
	}
	defer tx.Rollback()
	device, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	return &NativeSimulatorConfigResponse{
		Data: NativeSimulatorConfig{DeviceID: deviceID, Connection: connection.Data, Topics: []struct{}{}, ModelBinding: device.ModelBinding},
		Meta: RequestMeta{RequestID: requestID},
	}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/simulator-config
func (s *Service) GetDeviceSimulatorConfig(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	response, err := readNativeSimulatorConfig(r.Context(), identity, authz.Can(authn.CurrentData(), "device", "diagnose"), deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

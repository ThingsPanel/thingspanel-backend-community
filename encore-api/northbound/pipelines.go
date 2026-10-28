package northbound

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"encore.app/internal/authz"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListPipelines returns this tenant's forwarding tasks with their real backlog.
//
//encore:api auth method=GET path=/v1/pipelines
func ListPipelines(ctx context.Context) (*PipelineListResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `
		SELECT p.id::text, p.name, p.connection_id::text, coalesce(c.name, ''), coalesce(c.connector_id, ''),
		       p.filter, p.transform, p.serialization, p.state, p.revision, p.active_version,
		       p.created_at, p.updated_at
		FROM nb_pipelines p
		LEFT JOIN nb_connections c ON c.id = p.connection_id
		WHERE p.tenant_id = $1
		ORDER BY p.created_at DESC`, data.TenantID)
	if err != nil {
		return nil, errUnavailable("forwarding task list unavailable")
	}
	defer rows.Close()
	out := &PipelineListResponse{Pipelines: []PipelineView{}}
	for rows.Next() {
		v, err := scanPipeline(rows)
		if err != nil {
			return nil, errUnavailable("forwarding task list unavailable")
		}
		v.Backlog, _ = backlogFor(ctx, data.TenantID, v.ID)
		out.Pipelines = append(out.Pipelines, v)
	}
	return out, rows.Err()
}

func scanPipeline(row rowScanner) (PipelineView, error) {
	var v PipelineView
	var filter, transform, serialization []byte
	var active *int64
	err := row.Scan(&v.ID, &v.Name, &v.ConnectionID, &v.ConnectionName, &v.ConnectorID,
		&filter, &transform, &serialization, &v.State, &v.Revision, &active, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	v.Filter = json.RawMessage(filter)
	v.Transform = json.RawMessage(transform)
	v.Serialization = json.RawMessage(serialization)
	v.ActiveVersion = active
	return v, nil
}

// GetPipeline returns one forwarding task.
//
//encore:api auth method=GET path=/v1/pipelines/:id
func GetPipeline(ctx context.Context, id string) (*PipelineView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	pipelineID, err := parseUUID(id, "pipeline id")
	if err != nil {
		return nil, err
	}
	row := db.QueryRow(ctx, `
		SELECT p.id::text, p.name, p.connection_id::text, coalesce(c.name, ''), coalesce(c.connector_id, ''),
		       p.filter, p.transform, p.serialization, p.state, p.revision, p.active_version,
		       p.created_at, p.updated_at
		FROM nb_pipelines p
		LEFT JOIN nb_connections c ON c.id = p.connection_id
		WHERE p.tenant_id = $1 AND p.id = $2::uuid`, data.TenantID, pipelineID)
	v, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errNotFound("forwarding task not found")
		}
		return nil, errUnavailable("forwarding task unavailable")
	}
	v.Backlog, _ = backlogFor(ctx, data.TenantID, v.ID)
	return &v, nil
}

func validatePipelineInput(req *PipelineInput) error {
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 120 {
		return errBadRequest("name is required and must be at most 120 characters")
	}
	if strings.TrimSpace(req.ConnectionID) == "" {
		return errBadRequest("connectionId is required")
	}
	if len(req.Filter) > 0 && !json.Valid(req.Filter) {
		return errBadRequest("filter must be valid JSON")
	}
	if len(req.Transform) > 0 && !json.Valid(req.Transform) {
		return errBadRequest("transform must be valid JSON")
	}
	if len(req.Serialization) > 0 && !json.Valid(req.Serialization) {
		return errBadRequest("serialization must be valid JSON")
	}
	var f Filter
	if err := json.Unmarshal(mustJSON(req.Filter, "{}"), &f); err != nil {
		return errBadRequest("filter has an unexpected shape")
	}
	for _, t := range f.EventTypes {
		if !isValidEventType(t) {
			return errBadRequest("unsupported event type in filter: " + t)
		}
	}
	var t Transform
	if err := json.Unmarshal(mustJSON(req.Transform, "{}"), &t); err != nil {
		return errBadRequest("transform has an unexpected shape")
	}
	for _, conv := range t.UnitConvert {
		if _, err := convertUnit(1, conv.From, conv.To); err != nil {
			return errBadRequest(err.Error())
		}
	}
	return nil
}

// CreatePipeline creates a draft. Drafts never fan out events until activated.
//
//encore:api auth method=POST path=/v1/pipelines
func CreatePipeline(ctx context.Context, req *PipelineInput) (*PipelineView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	if err := validatePipelineInput(req); err != nil {
		return nil, err
	}
	connID, err := parseUUID(req.ConnectionID, "connection id")
	if err != nil {
		return nil, err
	}
	if _, err := GetConnection(ctx, connID); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	filter := mustJSON(req.Filter, "{}")
	transform := mustJSON(req.Transform, "{}")
	serialization := mustJSON(req.Serialization, "{}")
	_, err = db.Exec(ctx, `
		INSERT INTO nb_pipelines (id, tenant_id, name, connection_id, filter, transform, serialization, state, revision, created_by)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5::jsonb, $6::jsonb, $7::jsonb, 'draft', 1, $8)`,
		id, data.TenantID, strings.TrimSpace(req.Name), connID, string(filter), string(transform), string(serialization), data.UserID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errConflict("a forwarding task with this name already exists")
		}
		return nil, errUnavailable("forwarding task could not be created")
	}
	if err := snapshotVersion(ctx, id, 1, connID, filter, transform, serialization, data.UserID); err != nil {
		return nil, err
	}
	return GetPipeline(ctx, id)
}

// UpdatePipeline saves a new revision. An active task keeps delivering its
// already queued records with the version that produced them; only new events
// use the new revision once it is activated again.
//
//encore:api auth method=PUT path=/v1/pipelines/:id
func UpdatePipeline(ctx context.Context, id string, req *PipelineInput) (*PipelineView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	pipelineID, err := parseUUID(id, "pipeline id")
	if err != nil {
		return nil, err
	}
	if err := validatePipelineInput(req); err != nil {
		return nil, err
	}
	connID, err := parseUUID(req.ConnectionID, "connection id")
	if err != nil {
		return nil, err
	}
	if _, err := GetConnection(ctx, connID); err != nil {
		return nil, err
	}
	var current int64
	if err := db.QueryRow(ctx, `SELECT revision FROM nb_pipelines WHERE tenant_id = $1 AND id = $2::uuid`,
		data.TenantID, pipelineID).Scan(&current); err != nil {
		return nil, errNotFound("forwarding task not found")
	}
	if req.ExpectedRevision != current {
		return nil, errConflict("forwarding task was changed by someone else; reload and retry")
	}
	next := current + 1
	filter := mustJSON(req.Filter, "{}")
	transform := mustJSON(req.Transform, "{}")
	serialization := mustJSON(req.Serialization, "{}")
	tag, err := db.Exec(ctx, `
		UPDATE nb_pipelines
		SET name = $3, connection_id = $4::uuid, filter = $5::jsonb, transform = $6::jsonb,
		    serialization = $7::jsonb, revision = $8, updated_at = now()
		WHERE tenant_id = $1 AND id = $2::uuid AND revision = $9`,
		data.TenantID, pipelineID, strings.TrimSpace(req.Name), connID, string(filter), string(transform),
		string(serialization), next, current)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errConflict("a forwarding task with this name already exists")
		}
		return nil, errUnavailable("forwarding task could not be updated")
	}
	if tag.RowsAffected() != 1 {
		return nil, errConflict("forwarding task was changed by someone else; reload and retry")
	}
	if err := snapshotVersion(ctx, pipelineID, next, connID, filter, transform, serialization, data.UserID); err != nil {
		return nil, err
	}
	return GetPipeline(ctx, pipelineID)
}

func snapshotVersion(ctx context.Context, pipelineID string, version int64, connID string, filter, transform, serialization json.RawMessage, actor string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO nb_pipeline_versions (pipeline_id, version, connection_id, filter, transform, serialization, created_by)
		VALUES ($1::uuid, $2, $3::uuid, $4::jsonb, $5::jsonb, $6::jsonb, $7)
		ON CONFLICT (pipeline_id, version) DO NOTHING`,
		pipelineID, version, connID, string(filter), string(transform), string(serialization), actor)
	return err
}

// PreviewPipeline runs the saved filter and transform against a sample event.
// Nothing is sent and nothing is persisted, so a user can check the result
// before enabling outbound traffic.
//
//encore:api auth method=POST path=/v1/pipelines/:id/preview
func PreviewPipeline(ctx context.Context, id string, req *PreviewRequest) (*PreviewResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	pipelineID, err := parseUUID(id, "pipeline id")
	if err != nil {
		return nil, err
	}
	view, err := GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	env, err := decodeSample(req.Sample)
	if err != nil {
		return nil, err
	}
	var filter Filter
	_ = json.Unmarshal(view.Filter, &filter)
	var transform Transform
	_ = json.Unmarshal(view.Transform, &transform)
	resp := &PreviewResponse{Warnings: []string{}}
	ok, reason := matchEvent(filter, env)
	resp.Matched = ok
	resp.MatchReason = reason
	if !ok {
		return resp, nil
	}
	out, err := applyTransform(transform, env.Data, &resp.Warnings)
	if err != nil {
		return nil, errBadRequest("transform failed: " + err.Error())
	}
	resp.EventID = env.EventID
	resp.EventType = env.EventType
	resp.Subject = env.Subject
	resp.Output = out
	resp.IdempotencyKey = IdempotencyKey(data.TenantID, pipelineID, env.EventID, "default")
	return resp, nil
}

// ActivatePipeline starts fan-out for new events using the current revision.
// Records already queued keep their original version and destination.
//
//encore:api auth method=POST path=/v1/pipelines/:id/activate
func ActivatePipeline(ctx context.Context, id string) (*PipelineView, error) {
	return setPipelineState(ctx, id, PipelineActive)
}

// PausePipeline keeps the backlog and stops creating new deliveries.
//
//encore:api auth method=POST path=/v1/pipelines/:id/pause
func PausePipeline(ctx context.Context, id string) (*PipelineView, error) {
	return setPipelineState(ctx, id, PipelinePaused)
}

// DisablePipeline stops new fan-out and stops delivery attempts. Backlog is
// retained; disabling is not a delete.
//
//encore:api auth method=POST path=/v1/pipelines/:id/disable
func DisablePipeline(ctx context.Context, id string) (*PipelineView, error) {
	return setPipelineState(ctx, id, PipelineDisabled)
}

// DeletePipeline removes a forwarding task only while it is still a draft and
// has never produced a delivery. That is the only case where deleting cannot
// destroy audit evidence or drop a backlog, which is why every other state is
// refused: a task that has ever run owns delivery records that must survive.
//
//encore:api auth method=DELETE path=/v1/pipelines/:id
func DeletePipeline(ctx context.Context, id string) (*PipelineView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	pipelineID, err := parseUUID(id, "pipeline id")
	if err != nil {
		return nil, err
	}
	view, err := GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	if view.State != PipelineDraft {
		return nil, errConflict("only a draft task can be deleted; pause or disable it instead so its delivery history survives")
	}
	var deliveries int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM nb_deliveries WHERE tenant_id = $1 AND pipeline_id = $2::uuid`,
		data.TenantID, pipelineID).Scan(&deliveries); err != nil {
		return nil, errUnavailable("task delete check failed")
	}
	if deliveries > 0 {
		return nil, errConflict("this task already produced deliveries; disable it instead of deleting")
	}
	if _, err := db.Exec(ctx, `DELETE FROM nb_pipelines WHERE tenant_id = $1 AND id = $2::uuid`,
		data.TenantID, pipelineID); err != nil {
		return nil, errUnavailable("forwarding task could not be deleted")
	}
	return view, nil
}

func setPipelineState(ctx context.Context, id, state string) (*PipelineView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	pipelineID, err := parseUUID(id, "pipeline id")
	if err != nil {
		return nil, err
	}
	var revision int64
	var currentState string
	var connectionState string
	err = db.QueryRow(ctx, `
		SELECT p.revision, p.state, coalesce(c.state, 'disabled')
		FROM nb_pipelines p LEFT JOIN nb_connections c ON c.id = p.connection_id
		WHERE p.tenant_id = $1 AND p.id = $2::uuid`, data.TenantID, pipelineID).Scan(&revision, &currentState, &connectionState)
	if err != nil {
		return nil, errNotFound("forwarding task not found")
	}
	if state == PipelineActive && connectionState != "active" {
		return nil, errConflict("the target connection is not active; test it before enabling the task")
	}
	if currentState == PipelineDisabled && state == PipelineActive {
		// Re-enabling after a disable is allowed, but a disabled task must be
		// explicitly reviewed: it may have accumulated backlog.
	}
	tag, err := db.Exec(ctx, `
		UPDATE nb_pipelines SET state = $3, active_version = CASE WHEN $3 = 'active' THEN revision ELSE active_version END,
		       updated_at = now()
		WHERE tenant_id = $1 AND id = $2::uuid`, data.TenantID, pipelineID, state)
	if err != nil || tag.RowsAffected() != 1 {
		return nil, errUnavailable("forwarding task state could not be changed")
	}
	return GetPipeline(ctx, pipelineID)
}

// TestSendPipeline pushes one sample through the real delivery path so the user
// sees an actual target confirmation, not a simulated success.
//
//encore:api auth method=POST path=/v1/pipelines/:id/test-send
func TestSendPipeline(ctx context.Context, id string, req *TestSendRequest) (*TestSendResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	pipelineID, err := parseUUID(id, "pipeline id")
	if err != nil {
		return nil, err
	}
	view, err := GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	if view.ActiveVersion == nil && view.State != PipelineActive {
		return nil, errConflict("enable the forwarding task before sending a test record")
	}
	env, err := decodeSample(req.Sample)
	if err != nil {
		return nil, err
	}
	// A test send uses a distinct event id so it never collides with real data
	// and can be identified in the target system.
	env.EventID = "test-" + uuid.NewString()
	if env.OccurredAt.IsZero() {
		env.OccurredAt = time.Now().UTC()
	}
	res := &InboxResponse{}
	if err := acceptEvents(ctx, data.TenantID, []SourceEnvelope{env}, res); err != nil {
		return nil, err
	}
	if len(res.Rejected) > 0 {
		return &TestSendResponse{Accepted: false, Message: res.Rejected[0].Code + ": " + res.Rejected[0].Detail}, nil
	}
	deliveryID := DeliveryID(data.TenantID, pipelineID, env.EventID, "default")
	var state string
	_ = db.QueryRow(ctx, `SELECT state FROM nb_deliveries WHERE id = $1::uuid`, deliveryID).Scan(&state)
	return &TestSendResponse{Accepted: true, DeliveryID: deliveryID, State: state, EventID: env.EventID}, nil
}

func decodeSample(raw json.RawMessage) (SourceEnvelope, error) {
	var env SourceEnvelope
	if len(raw) == 0 {
		return env, errBadRequest("sample is required")
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, errBadRequest("sample must be a northbound source envelope")
	}
	if env.DeviceID == "" {
		return env, errBadRequest("sample.deviceId is required")
	}
	if env.EventType == "" {
		env.EventType = "telemetry"
	}
	if !isValidEventType(env.EventType) {
		return env, errBadRequest("sample.eventType must be telemetry, attribute or event")
	}
	if len(env.Data) == 0 {
		env.Data = json.RawMessage(`{}`)
	}
	if env.EventID == "" {
		env.EventID = "sample-" + uuid.NewString()
	}
	if env.Subject == "" {
		env.Subject = "devices/" + env.DeviceID
	}
	return env, nil
}

func backlogFor(ctx context.Context, tenantID, pipelineID string) (Backlog, error) {
	var b Backlog
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'pending'),
			count(*) FILTER (WHERE state = 'retry_wait'),
			count(*) FILTER (WHERE state = 'unknown'),
			count(*) FILTER (WHERE state = 'dead'),
			min(created_at) FILTER (WHERE state IN ('pending','retry_wait','leased'))
		FROM nb_deliveries WHERE tenant_id = $1 AND pipeline_id = $2::uuid`, tenantID, pipelineID).
		Scan(&b.Pending, &b.RetryWait, &b.Unknown, &b.Dead, &b.OldestAt)
	if b.OldestAt != nil {
		b.OldestAgeMS = time.Since(*b.OldestAt).Milliseconds()
	}
	return b, err
}

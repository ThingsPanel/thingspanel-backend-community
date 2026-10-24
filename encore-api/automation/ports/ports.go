// Package ports contains the narrow outbound contracts to the modules this
// service does not own: device commands, notifications and the device
// directory. Nothing here writes another module's tables.
package ports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrNotConfigured means the upstream owner is not reachable from this
// deployment. Callers must surface it as "unknown", never as success.
var ErrNotConfigured = errors.New("upstream owner is not configured")

// ErrLabelNotExposed means the directory answered but does not carry the field a
// label target needs. It is distinct from "no device matched": treating the two
// as the same is how a target silently becomes an empty set.
var ErrLabelNotExposed = errors.New("device directory does not expose device labels")

// Config is resolved from the environment so no secret is baked into code.
type Config struct {
	CommunityBaseURL  string
	CommunityToken    string
	NotificationURL   string
	NotificationToken string
	DeploymentID      string
	Timeout           time.Duration
}

// FromEnv builds the port configuration.
func FromEnv() Config {
	timeout := 15 * time.Second
	return Config{
		CommunityBaseURL:  strings.TrimRight(os.Getenv("TP_COMMUNITY_BASE_URL"), "/"),
		CommunityToken:    os.Getenv("TP_COMMUNITY_TOKEN"),
		NotificationURL:   strings.TrimRight(os.Getenv("TP_NOTIFICATION_URL"), "/"),
		NotificationToken: os.Getenv("TP_NOTIFICATION_TOKEN"),
		DeploymentID:      envOr("TP_SOURCE_DEPLOYMENT_ID", "automation-rewrite"),
		Timeout:           timeout,
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func newClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// ---------------------------------------------------------------------------
// Device commands
// ---------------------------------------------------------------------------

// CommandKind mirrors the legacy TEL/ATTR/CMD split.
type CommandKind string

const (
	CommandTelemetry CommandKind = "telemetry"
	CommandAttribute CommandKind = "attribute"
	CommandCommand   CommandKind = "command"
)

// CommandRequest is one action intent. IdempotencyKey is stable across
// transport retries; Generation lets the acceptance side reject a command that
// belongs to a superseded run.
//
// Value must be a JSON document (for example `{"switch":1}`) because the
// community control APIs validate the payload with json.Valid before routing it
// to the device. Sending a bare scalar is rejected by the owner, which is the
// behaviour the legacy scene action had as well.
type CommandRequest struct {
	Kind           CommandKind
	TenantID       string
	DeviceID       string
	Identifier     string
	Value          string
	IdempotencyKey string
	ScopeKey       string
	Generation     int64
	Deadline       time.Time
}

// Command result states. They describe what the owner did with the intent, not
// what the device did with the command.
const (
	// CommandAccepted means the owner took responsibility and returned an id.
	CommandAccepted = "accepted"
	// CommandFailed means the owner refused it and nothing will happen.
	CommandFailed = "failed"
	// CommandUnknown means nobody knows whether it took effect.
	CommandUnknown = "unknown"
	// CommandSuperseded means a newer trigger in the same scope already won, so
	// this action was refused before it could reach a device.
	CommandSuperseded = "superseded"
	// CommandConflict means the idempotency key was reused for different
	// content. It is a programming error, not a retry.
	CommandConflict = "conflict"
	// CommandInFlight means an identical attempt is still being processed. The
	// answer is "ask again shortly", never "send it again".
	CommandInFlight = "in_flight"
)

// CommandResult is the acceptance outcome, not the physical effect.
type CommandResult struct {
	State   string // accepted | failed | unknown | superseded | conflict | in_flight
	Ref     string
	Message string
}

// CommandOwner is the existing device command module.
type CommandOwner interface {
	Dispatch(ctx context.Context, req CommandRequest) (CommandResult, error)
	Status(ctx context.Context, ref string) (string, error)
}

// HTTPCommandOwner talks to the community backend's existing control APIs.
type HTTPCommandOwner struct {
	cfg    Config
	client *http.Client
}

func NewHTTPCommandOwner(cfg Config) *HTTPCommandOwner {
	return &HTTPCommandOwner{cfg: cfg, client: newClient(cfg.Timeout)}
}

func (h *HTTPCommandOwner) Dispatch(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if h.cfg.CommunityBaseURL == "" {
		return CommandResult{State: "unknown", Message: "community command owner not configured"}, ErrNotConfigured
	}
	var path string
	body := map[string]any{"device_id": req.DeviceID, "value": req.Value}
	switch req.Kind {
	case CommandTelemetry:
		path = "/api/v1/telemetry/datas/pub"
	case CommandAttribute:
		path = "/api/v1/attribute/datas/pub"
	case CommandCommand:
		path = "/api/v1/command/datas/pub"
		body["identify"] = req.Identifier
	default:
		return CommandResult{State: "failed", Message: "unsupported command kind: " + string(req.Kind)}, nil
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.CommunityBaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return CommandResult{State: "failed", Message: err.Error()}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if h.cfg.CommunityToken != "" {
		httpReq.Header.Set("x-token", h.cfg.CommunityToken)
	}
	// The command owner must be able to reject a superseded generation. The
	// legacy API has no such field yet, so the intent is carried on the headers
	// and the idempotency key until the cross-module field lands (A10).
	httpReq.Header.Set("X-Idempotency-Key", req.IdempotencyKey)
	httpReq.Header.Set("X-Automation-Scope", req.ScopeKey)
	httpReq.Header.Set("X-Automation-Generation", fmt.Sprintf("%d", req.Generation))

	resp, err := h.client.Do(httpReq)
	if err != nil {
		// A transport error after the request left the process is unknown, not
		// failed: the device may still act.
		return CommandResult{State: "unknown", Message: err.Error()}, nil
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(payload, &envelope)

	// The acceptance guard answers with its own codes and, for supersession, a
	// header the caller can read without parsing a message. Both are checked
	// before the generic HTTP mapping so a refusal is never mistaken for a
	// transport failure.
	if resp.Header.Get("X-Automation-Superseded") == "true" || envelope.Code == codeGuardSuperseded {
		return CommandResult{
			State:   CommandSuperseded,
			Ref:     guardRef(payload),
			Message: "已被更新的触发取代，未下发: " + envelope.Message,
		}, nil
	}
	switch envelope.Code {
	case codeGuardKeyConflict:
		return CommandResult{State: CommandConflict, Message: "同一幂等键被用于不同内容: " + envelope.Message}, nil
	case codeGuardInFlight:
		return CommandResult{State: CommandInFlight, Message: "同一动作正在处理中: " + envelope.Message}, nil
	case codeGuardUnknown:
		// The previous attempt died while holding the key. Retrying cannot
		// resolve it, so it must be surfaced instead of retried forever.
		return CommandResult{State: CommandUnknown, Message: "结果未知，需人工核对: " + envelope.Message}, nil
	}

	switch {
	case resp.StatusCode >= 500:
		return CommandResult{State: CommandUnknown, Message: fmt.Sprintf("http %d: %s", resp.StatusCode, envelope.Message)}, nil
	case resp.StatusCode >= 400 || envelope.Code != 200:
		return CommandResult{State: CommandFailed, Message: fmt.Sprintf("http %d code %d: %s", resp.StatusCode, envelope.Code, envelope.Message)}, nil
	default:
		ref := ""
		if len(envelope.Data) > 0 {
			var data map[string]any
			if json.Unmarshal(envelope.Data, &data) == nil {
				for _, k := range []string{"message_id", "command_id", "id"} {
					if v, ok := data[k].(string); ok && v != "" {
						ref = v
						break
					}
				}
			}
		}
		return CommandResult{State: CommandAccepted, Ref: ref, Message: "accepted by command owner"}, nil
	}
}

// Guard refusal codes, mirrored from internal/automationguard. They are
// duplicated rather than imported because this package must stay independent of
// the community backend's internal tree.
const (
	codeGuardSuperseded  = 4090
	codeGuardKeyConflict = 4091
	codeGuardInFlight    = 4092
	codeGuardUnknown     = 4093
)

// guardRef keeps the owner's id on a refusal when it was reported, so a run can
// still be correlated with whatever the first attempt may have created.
func guardRef(payload []byte) string {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil || len(envelope.Data) == 0 {
		return ""
	}
	var data map[string]any
	if json.Unmarshal(envelope.Data, &data) != nil {
		return ""
	}
	if v, ok := data["accepted_ref"].(string); ok {
		return v
	}
	return ""
}

func (h *HTTPCommandOwner) Status(ctx context.Context, ref string) (string, error) {
	if h.cfg.CommunityBaseURL == "" || ref == "" {
		return "", ErrNotConfigured
	}
	u := h.cfg.CommunityBaseURL + "/api/v1/command/datas/status/" + url.PathEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	if h.cfg.CommunityToken != "" {
		req.Header.Set("x-token", h.cfg.CommunityToken)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return "unknown", nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "unknown", nil
	}
	var envelope struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "unknown", nil
	}
	if envelope.Data.Status == "" {
		return "unknown", nil
	}
	return envelope.Data.Status, nil
}

// SceneActivator is the scene owner. A scene step is only "accepted" when that
// owner confirms intake; there is no local fallback that pretends to have run
// another module's scene.
type SceneActivator interface {
	ActivateScene(ctx context.Context, tenantID, sceneID string) (state, ref, message string, err error)
}

// ActivateScene reports honestly that the legacy API exposes no "run this scene
// now" endpoint. Returning unknown keeps the intent visible and retryable
// instead of fabricating a success (see B21 for the same defect class).
func (h *HTTPCommandOwner) ActivateScene(ctx context.Context, tenantID, sceneID string) (string, string, string, error) {
	if h.cfg.CommunityBaseURL == "" {
		return "unknown", "", "community scene owner not configured", ErrNotConfigured
	}
	return "unknown", "", "legacy backend exposes no scene-run endpoint; scene activation stays unverified (A21)", ErrNotConfigured
}

// ---------------------------------------------------------------------------
// Notification
// ---------------------------------------------------------------------------

// NotificationRequest is a business notification intent.
type NotificationRequest struct {
	TenantID       string
	PolicyID       string
	GroupRevision  int64
	SourceEventID  string
	ActionID       string
	Subject        string
	Text           string
	Variables      map[string]any
	OccurredAt     time.Time
	ExpiresAt      time.Time
	IdempotencyKey string
}

// NotificationResult is the intake outcome. "accepted" means the notification
// core took responsibility; it is not proof that a phone received anything.
type NotificationResult struct {
	State          string
	NotificationID string
	DeliveryIDs    []string
	Message        string
}

// NotificationOwner is the existing notification core.
type NotificationOwner interface {
	Submit(ctx context.Context, req NotificationRequest) (NotificationResult, error)
}

// HTTPNotificationOwner calls the existing ingress contract.
type HTTPNotificationOwner struct {
	cfg    Config
	client *http.Client
}

func NewHTTPNotificationOwner(cfg Config) *HTTPNotificationOwner {
	return &HTTPNotificationOwner{cfg: cfg, client: newClient(cfg.Timeout)}
}

func (h *HTTPNotificationOwner) Submit(ctx context.Context, req NotificationRequest) (NotificationResult, error) {
	base := h.cfg.NotificationURL
	if base == "" {
		base = h.cfg.CommunityBaseURL
	}
	if base == "" {
		return NotificationResult{State: "unknown", Message: "notification owner not configured"}, ErrNotConfigured
	}
	occurred := req.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	expires := req.ExpiresAt
	if expires.IsZero() {
		expires = occurred.Add(24 * time.Hour)
	}
	body := map[string]any{
		"schemaVersion":       "1",
		"sourceDeploymentId":  h.cfg.DeploymentID,
		"sourceEventId":       req.SourceEventID,
		"actionId":            req.ActionID,
		"tenantId":            req.TenantID,
		"notificationGroupId": req.PolicyID,
		"groupRevision":       req.GroupRevision,
		"occurredAt":          occurred.UTC().Format(time.RFC3339),
		"expiresAt":           expires.UTC().Format(time.RFC3339),
		"payload": map[string]any{
			"subject":   req.Subject,
			"text":      req.Text,
			"variables": req.Variables,
		},
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v2/notification-ingress/events", bytes.NewReader(raw))
	if err != nil {
		return NotificationResult{State: "failed", Message: err.Error()}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if h.cfg.NotificationToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+h.cfg.NotificationToken)
	}
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return NotificationResult{State: "unknown", Message: err.Error()}, nil
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Accepted       bool     `json:"accepted"`
			NotificationID string   `json:"notificationId"`
			DeliveryIDs    []string `json:"deliveryIds"`
			IntakeStatus   string   `json:"intakeStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return NotificationResult{State: "unknown", Message: "unreadable intake response"}, nil
	}
	switch {
	case resp.StatusCode == http.StatusAccepted && envelope.Data.Accepted:
		return NotificationResult{
			State: "accepted", NotificationID: envelope.Data.NotificationID,
			DeliveryIDs: envelope.Data.DeliveryIDs, Message: envelope.Data.IntakeStatus,
		}, nil
	case resp.StatusCode >= 500:
		return NotificationResult{State: "unknown", Message: fmt.Sprintf("http %d", resp.StatusCode)}, nil
	default:
		return NotificationResult{State: "failed", Message: fmt.Sprintf("http %d code %d", resp.StatusCode, envelope.Code)}, nil
	}
}

// ---------------------------------------------------------------------------
// Device directory
// ---------------------------------------------------------------------------

// Device is the directory projection used for target resolution.
//
// The community device list returns is_online as 0/1, not as a boolean, and it
// exposes no capability list. Both are represented faithfully here instead of
// being coerced into a convenient but wrong shape.
type Device struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenant_id"`
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	DeviceConfigID string   `json:"device_config_id"`
	DeviceType     string   `json:"device_type"`
	Online         int      `json:"is_online"`
	Capabilities   []string `json:"capabilities"`
}

// Directory resolves devices for a target specification.
type Directory interface {
	ByIDs(ctx context.Context, tenantID string, ids []string) ([]Device, error)
	ByDeviceConfig(ctx context.Context, tenantID string, configIDs []string) ([]Device, error)
	ByLabels(ctx context.Context, tenantID string, labels []string, matchAll bool) ([]Device, error)
	ByType(ctx context.Context, tenantID, deviceType string) ([]Device, error)
}

// HTTPDirectory reads the community device list.
type HTTPDirectory struct {
	cfg    Config
	client *http.Client
}

func NewHTTPDirectory(cfg Config) *HTTPDirectory {
	return &HTTPDirectory{cfg: cfg, client: newClient(cfg.Timeout)}
}

func (h *HTTPDirectory) ByIDs(ctx context.Context, tenantID string, ids []string) ([]Device, error) {
	all, err := h.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []Device
	for _, d := range all {
		if want[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}

func (h *HTTPDirectory) ByDeviceConfig(ctx context.Context, tenantID string, configIDs []string) ([]Device, error) {
	all, err := h.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(configIDs))
	for _, id := range configIDs {
		want[id] = true
	}
	var out []Device
	for _, d := range all {
		if want[d.DeviceConfigID] {
			out = append(out, d)
		}
	}
	return out, nil
}

// LabelTokens splits the community directory's single label field into the
// individual labels it carries.
//
// The field is free text that people fill with several tags separated by commas
// or spaces. Matching it with strings.Contains makes "floor-1" match "floor-10",
// which silently targets the wrong devices — so the field is tokenised and
// compared exactly. Both the HTTP directory and the test fake use this function
// so they cannot drift apart.
func LabelTokens(label string) []string {
	fields := strings.FieldsFunc(label, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '|', '/', ' ', '\t', '\n':
			return true
		}
		return false
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (h *HTTPDirectory) ByLabels(ctx context.Context, tenantID string, labels []string, matchAll bool) ([]Device, error) {
	all, err := h.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var out []Device
	labelled := 0
	for _, d := range all {
		tokens := LabelTokens(d.Label)
		if len(tokens) > 0 {
			labelled++
		}
		have := map[string]bool{}
		for _, t := range tokens {
			have[t] = true
		}
		hits := 0
		for _, l := range labels {
			if have[l] {
				hits++
			}
		}
		if (matchAll && hits == len(labels)) || (!matchAll && hits > 0) {
			out = append(out, d)
		}
	}
	if len(out) == 0 && len(all) > 0 && labelled == 0 {
		// Every device came back without a label: the directory simply does not
		// expose the field, so "no match" would be a lie.
		return nil, ErrLabelNotExposed
	}
	return out, nil
}

func (h *HTTPDirectory) ByType(ctx context.Context, tenantID, deviceType string) ([]Device, error) {
	all, err := h.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, d := range all {
		if d.DeviceType == deviceType {
			out = append(out, d)
		}
	}
	return out, nil
}

// list pages through the community device list.
//
// Two contracts matter here and both were verified against the running backend:
//   - page_size is capped at 1000 by the API (a larger value is rejected with
//     code 100002), so the reader must page instead of asking for everything;
//   - a business error comes back as HTTP 200 with code != 200, so the envelope
//     code must be checked or a failure looks like an empty directory.
func (h *HTTPDirectory) list(ctx context.Context, tenantID string) ([]Device, error) {
	if h.cfg.CommunityBaseURL == "" {
		return nil, ErrNotConfigured
	}
	const pageSize = 200
	const maxPages = 50

	var out []Device
	for page := 1; page <= maxPages; page++ {
		u := fmt.Sprintf("%s/api/v1/device?page=%d&page_size=%d", h.cfg.CommunityBaseURL, page, pageSize)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if h.cfg.CommunityToken != "" {
			req.Header.Set("x-token", h.cfg.CommunityToken)
		}
		resp, err := h.client.Do(req)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				List  []Device `json:"list"`
				Total int      `json:"total"`
			} `json:"data"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&envelope)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("device directory http %d", resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("device directory response unreadable: %w", decodeErr)
		}
		if envelope.Code != 200 {
			return nil, fmt.Errorf("device directory rejected the request: code %d %s",
				envelope.Code, envelope.Message)
		}
		out = append(out, envelope.Data.List...)
		if len(envelope.Data.List) < pageSize || len(out) >= envelope.Data.Total {
			break
		}
	}

	if tenantID != "" {
		filtered := out[:0]
		for _, d := range out {
			if d.TenantID == "" || d.TenantID == tenantID {
				filtered = append(filtered, d)
			}
		}
		out = filtered
	}
	return out, nil
}

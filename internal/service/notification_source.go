package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"project/internal/dal"
	"project/internal/model"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

const (
	sourceEventPath   = "/api/v2/notification-ingress/events"
	sourceProjectPath = "/api/v2/notification-group-projections"
	maxSourceBody     = 256 << 10
)

type SourceBridgeConfig struct {
	Enabled        bool
	BaseURL        string
	DeploymentID   string
	BearerToken    string
	PollInterval   time.Duration
	RequestTimeout time.Duration
}

type SourceGroupProjectionRequest struct {
	SourceDeploymentID  string `json:"sourceDeploymentId"`
	TenantID            string `json:"tenantId"`
	LegacyGroupID       string `json:"legacyGroupId"`
	NotificationGroupID string `json:"notificationGroupId"`
	GroupRevision       int64  `json:"groupRevision"`
}

type SourceGroupProjection struct {
	SourceDeploymentID  string `json:"sourceDeploymentId"`
	TenantID            string `json:"tenantId"`
	LegacyGroupID       string `json:"legacyGroupId"`
	NotificationGroupID string `json:"notificationGroupId"`
	GroupRevision       int64  `json:"groupRevision"`
	CreatedAt           string `json:"createdAt"`
}

type sourceEnvelope[T any] struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	Data      T      `json:"data"`
}

type sourceAccepted struct {
	Accepted       bool     `json:"accepted"`
	NotificationID string   `json:"notificationId"`
	DeliveryIDs    []string `json:"deliveryIds"`
	IntakeStatus   string   `json:"intakeStatus"`
}

type sourceEvent struct {
	SchemaVersion       string             `json:"schemaVersion"`
	SourceDeploymentID  string             `json:"sourceDeploymentId"`
	SourceEventID       string             `json:"sourceEventId"`
	ActionID            string             `json:"actionId"`
	TenantID            string             `json:"tenantId"`
	NotificationGroupID string             `json:"notificationGroupId"`
	GroupRevision       int64              `json:"groupRevision"`
	OccurredAt          string             `json:"occurredAt"`
	ExpiresAt           string             `json:"expiresAt"`
	Payload             sourceEventPayload `json:"payload"`
}

type sourceEventPayload struct {
	Subject         string                     `json:"subject"`
	Text            string                     `json:"text"`
	Variables       map[string]json.RawMessage `json:"variables"`
	LegacyAlertJSON map[string]json.RawMessage `json:"legacyAlertJson,omitempty"`
}

type SourceCompatibilityChecker interface {
	// CheckEncoreCompatibility validates every active legacy recipient, its
	// tenant-controlled resolution path, and an exact target binding. It must
	// fail when any member cannot be represented; partial fan-out is forbidden.
	CheckEncoreCompatibility(context.Context, *model.NotificationGroup, string) error
}

type SourceBridge struct {
	config SourceBridgeConfig
	base   *url.URL
	client *http.Client
	check  SourceCompatibilityChecker
}

func NewSourceBridge(config SourceBridgeConfig, checker SourceCompatibilityChecker) (*SourceBridge, error) {
	if !config.Enabled {
		return &SourceBridge{config: config}, nil
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("source bridge configuration invalid")
	}
	if config.DeploymentID == "" || config.BearerToken == "" || checker == nil {
		return nil, errors.New("source bridge configuration incomplete")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 10 * time.Second
	}
	return &SourceBridge{
		config: config,
		base:   parsed,
		check:  checker,
		client: &http.Client{Timeout: config.RequestTimeout, Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (s *SourceBridge) Enabled() bool { return s != nil && s.config.Enabled }

func (s *SourceBridge) PollInterval() time.Duration {
	if !s.Enabled() || s.config.PollInterval <= 0 {
		return time.Second
	}
	return s.config.PollInterval
}

func (s *SourceBridge) DeploymentID() string {
	if !s.Enabled() {
		return ""
	}
	return s.config.DeploymentID
}

func (s *SourceBridge) endpoint(path string) string {
	copyURL := *s.base
	copyURL.Path = strings.TrimRight(copyURL.Path, "/") + path
	return copyURL.String()
}

func (s *SourceBridge) postJSON(ctx context.Context, path, idempotencyKey string, body []byte) (*http.Response, []byte, error) {
	if !s.Enabled() || len(body) == 0 || len(body) > maxSourceBody || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 {
		return nil, nil, errors.New("source request unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(path), bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("source request unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+s.config.BearerToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, nil, errors.New("source request failed")
	}
	limited := io.LimitReader(response.Body, maxSourceBody+1)
	responseBody, readErr := io.ReadAll(limited)
	_ = response.Body.Close()
	if readErr != nil || len(responseBody) > maxSourceBody {
		return response, nil, errors.New("source response invalid")
	}
	return response, responseBody, nil
}

func (s *SourceBridge) RegisterProjection(ctx context.Context, request SourceGroupProjectionRequest, idempotencyKey string) (SourceGroupProjection, error) {
	var empty SourceGroupProjection
	if !s.Enabled() || request.SourceDeploymentID != s.config.DeploymentID || request.TenantID == "" || request.LegacyGroupID == "" || request.NotificationGroupID == "" || request.GroupRevision < 1 {
		return empty, errors.New("source projection invalid")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return empty, errors.New("source projection invalid")
	}
	response, responseBody, err := s.postJSON(ctx, sourceProjectPath, idempotencyKey, body)
	if err != nil || response == nil || response.StatusCode != http.StatusOK {
		return empty, errors.New("source projection unavailable")
	}
	var envelope sourceEnvelope[SourceGroupProjection]
	if !isJSONResponse(response) || json.Unmarshal(responseBody, &envelope) != nil || envelope.Code != 200 || strings.TrimSpace(envelope.Message) == "" || strings.TrimSpace(envelope.RequestID) == "" {
		return empty, errors.New("source projection response invalid")
	}
	projection := envelope.Data
	if projection.SourceDeploymentID != request.SourceDeploymentID || projection.TenantID != request.TenantID || projection.LegacyGroupID != request.LegacyGroupID || projection.NotificationGroupID != request.NotificationGroupID || projection.GroupRevision != request.GroupRevision || strings.TrimSpace(projection.CreatedAt) == "" {
		return empty, errors.New("source projection response mismatch")
	}
	return projection, nil
}

// SwitchToEncore first asks Encore to persist and validate the exact published
// projection, then CAS-updates the old source route. Replaying the same key and
// tuple after a lost response is safe; a failed local CAS leaves legacy routing.
func (s *SourceBridge) SwitchToEncore(ctx context.Context, request SourceGroupProjectionRequest, idempotencyKey string, expectedRouteVersion int64) error {
	if !s.Enabled() {
		return errors.New("source bridge disabled")
	}
	group, err := dal.GetNotificationGroupById(request.LegacyGroupID)
	if err != nil || group == nil || group.TenantID != request.TenantID || group.Status != "OPEN" || !sourceGroupTypesSupported(group.NotificationType) {
		return errors.New("source group unavailable")
	}
	if err := s.check.CheckEncoreCompatibility(ctx, group, request.NotificationGroupID); err != nil {
		return errors.New("source group is not compatible")
	}
	projection, err := s.RegisterProjection(ctx, request, idempotencyKey)
	if err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, projection.CreatedAt); err != nil {
		return errors.New("source projection response invalid")
	}
	key := dal.SourceRouteKey{DeploymentID: request.SourceDeploymentID, TenantID: request.TenantID, LegacyGroup: request.LegacyGroupID}
	if err := dal.SwitchSourceRoute(ctx, key, expectedRouteVersion, projection.GroupRevision, projection.NotificationGroupID, idempotencyKey); err != nil {
		return errors.New("source route update failed")
	}
	return nil
}

func sourceGroupTypesSupported(raw string) bool {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return false
	}
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		typeName := strings.TrimSpace(part)
		if typeName == "" || seen[typeName] {
			return false
		}
		seen[typeName] = true
		switch typeName {
		case model.NoticeType_Email, model.NoticeType_SME_CODE, model.NoticeType_Member, model.NoticeType_Webhook:
		default:
			// APP fan-out, VOICE, and unknown historical types stay wholly legacy.
			return false
		}
	}
	return true
}

func (s *SourceBridge) Deliver(ctx context.Context, record dal.SourceOutboxRecord) dal.SourceAttemptResult {
	if !validOutboxRecord(record) {
		return dal.SourceAttemptResult{StatusCode: http.StatusUnprocessableEntity, SafeCode: "invalid_source_event"}
	}
	response, body, err := s.postJSON(ctx, sourceEventPath, record.IdempotencyKey, record.RequestBody)
	if err != nil {
		return dal.SourceAttemptResult{SafeCode: "relay_retry"}
	}
	if response == nil {
		return dal.SourceAttemptResult{SafeCode: "relay_retry"}
	}
	if response.StatusCode == http.StatusAccepted {
		var envelope sourceEnvelope[sourceAccepted]
		if isJSONResponse(response) && json.Unmarshal(body, &envelope) == nil && envelope.Code == 200 && strings.TrimSpace(envelope.Message) != "" && strings.TrimSpace(envelope.RequestID) != "" && envelope.Data.Accepted && envelope.Data.NotificationID != "" && (envelope.Data.IntakeStatus == "ready" || envelope.Data.IntakeStatus == "blocked") && envelope.Data.DeliveryIDs != nil {
			return dal.SourceAttemptResult{StatusCode: response.StatusCode, Accepted: true}
		}
		return dal.SourceAttemptResult{StatusCode: http.StatusUnprocessableEntity, SafeCode: "unexpected_response"}
	}
	code := "relay_retry"
	if response.StatusCode == 401 {
		code = "authentication_failed"
	}
	if response.StatusCode == 403 {
		code = "source_scope_mismatch"
	}
	if response.StatusCode == 400 || response.StatusCode == 422 {
		code = "invalid_source_event"
	}
	return dal.SourceAttemptResult{StatusCode: response.StatusCode, SafeCode: code}
}

func isJSONResponse(response *http.Response) bool {
	if response == nil {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func validOutboxRecord(record dal.SourceOutboxRecord) bool {
	if len(record.RequestBody) == 0 || len(record.RequestBody) > maxSourceBody || record.SourceDeploymentID == "" || record.TenantID == "" || record.SourceEventID == "" || record.SourceActionID == "" || record.IdempotencyKey != sourceKey(record.SourceEventID, record.SourceActionID) {
		return false
	}
	digest := sha256.Sum256(record.RequestBody)
	if hex.EncodeToString(digest[:]) != record.BodySHA256 {
		return false
	}
	var event sourceEvent
	if json.Unmarshal(record.RequestBody, &event) != nil {
		return false
	}
	if !validateSourceEvent(event) {
		return false
	}
	occurred, occurredErr := time.Parse(time.RFC3339Nano, event.OccurredAt)
	expires, expiresErr := time.Parse(time.RFC3339Nano, event.ExpiresAt)
	return occurredErr == nil && expiresErr == nil &&
		event.SchemaVersion == "1.0" && event.SourceDeploymentID == record.SourceDeploymentID &&
		event.TenantID == record.TenantID && event.SourceEventID == record.SourceEventID &&
		event.ActionID == record.SourceActionID && event.NotificationGroupID == record.NotificationGroupID &&
		event.GroupRevision == record.GroupRevision && occurred.Equal(record.OccurredAt) && expires.Equal(record.ExpiresAt) && expires.After(occurred)
}

var sourceVariableName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

func validateSourceEvent(event sourceEvent) bool {
	ids := []string{event.SourceDeploymentID, event.SourceEventID, event.ActionID, event.TenantID, event.NotificationGroupID}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || utf8.RuneCountInString(id) > 256 {
			return false
		}
	}
	if event.SchemaVersion != "1.0" || event.GroupRevision < 1 || utf8.RuneCountInString(event.Payload.Subject) > 512 || utf8.RuneCountInString(event.Payload.Text) > 65536 || event.Payload.Variables == nil || len(event.Payload.Variables) > 256 || len(event.Payload.LegacyAlertJSON) > 256 {
		return false
	}
	occurred, occurredErr := time.Parse(time.RFC3339Nano, event.OccurredAt)
	expires, expiresErr := time.Parse(time.RFC3339Nano, event.ExpiresAt)
	if occurredErr != nil || expiresErr != nil || !expires.After(occurred) {
		return false
	}
	for key, raw := range event.Payload.Variables {
		if !sourceVariableName.MatchString(key) || !validateSourceJSONValue(raw) {
			return false
		}
	}
	for _, raw := range event.Payload.LegacyAlertJSON {
		if !validateSourceJSONValue(raw) {
			return false
		}
	}
	return true
}

func validateSourceJSONValue(raw json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if decoder.Decode(&value) != nil {
		return false
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		return false
	}
	return validateSourceJSONNode(value, 0)
}

func validateSourceJSONNode(value interface{}, depth int) bool {
	if depth > 64 {
		return false
	}
	switch typed := value.(type) {
	case nil, bool, json.Number:
		return true
	case string:
		return utf8.RuneCountInString(typed) <= 65536
	case []interface{}:
		if len(typed) > 256 {
			return false
		}
		for _, child := range typed {
			if !validateSourceJSONNode(child, depth+1) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		if len(typed) > 256 {
			return false
		}
		for _, child := range typed {
			if !validateSourceJSONNode(child, depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (s *SourceBridge) Close() {
	if s != nil && s.client != nil {
		if transport, ok := s.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

func (s *SourceBridge) buildOutbox(route dal.SourceRouteSnapshot, tenantID, legacyGroupID, eventID, actionID string, occurred time.Time, subject, text, legacyJSON string) (*dal.SourceOutboxRecord, error) {
	if !s.Enabled() || route.Engine != "encore" || route.NotificationGroupID == "" || route.GroupRevision < 1 {
		return nil, errors.New("source route invalid")
	}
	var legacy map[string]json.RawMessage
	if legacyJSON != "" {
		if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil || legacy == nil {
			return nil, errors.New("source payload invalid")
		}
	}
	if legacy == nil {
		legacy = map[string]json.RawMessage{}
	}
	payload := sourceEventPayload{Subject: subject, Text: text, Variables: map[string]json.RawMessage{}, LegacyAlertJSON: legacy}
	event := sourceEvent{SchemaVersion: "1.0", SourceDeploymentID: s.config.DeploymentID, SourceEventID: eventID, ActionID: actionID, TenantID: tenantID, NotificationGroupID: route.NotificationGroupID, GroupRevision: route.GroupRevision, OccurredAt: occurred.UTC().Format(time.RFC3339Nano), ExpiresAt: occurred.UTC().Add(24 * time.Hour).Format(time.RFC3339Nano), Payload: payload}
	if !validateSourceEvent(event) {
		return nil, errors.New("source payload invalid")
	}
	body, err := json.Marshal(event)
	if err != nil || len(body) == 0 || len(body) > maxSourceBody {
		return nil, errors.New("source payload invalid")
	}
	digest := sha256.Sum256(body)
	return &dal.SourceOutboxRecord{ID: uuid.New(), SourceDeploymentID: s.config.DeploymentID, TenantID: tenantID, SourceEventID: eventID, SourceActionID: actionID, LegacyGroupID: legacyGroupID, NotificationGroupID: route.NotificationGroupID, GroupRevision: route.GroupRevision, IdempotencyKey: "alarm-" + eventID + "-" + actionID, RequestBody: body, BodySHA256: hex.EncodeToString(digest[:]), OccurredAt: occurred.UTC(), ExpiresAt: occurred.UTC().Add(24 * time.Hour), State: "pending", Attempts: 0, NextAttemptAt: occurred.UTC(), CreatedAt: occurred.UTC(), UpdatedAt: occurred.UTC()}, nil
}

func (s *SourceBridge) RunOnce(ctx context.Context, maxBatch, maxAttempts int) error {
	if !s.Enabled() {
		return nil
	}
	claimed, err := dal.ClaimSourceOutbox(ctx, maxBatch, 30*time.Second, maxAttempts, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, record := range claimed {
		result := s.Deliver(ctx, record)
		if err := dal.FinishSourceOutboxAttempt(ctx, record, result, time.Now().UTC(), maxAttempts); err != nil {
			logrus.WithField("source_event_id", record.SourceEventID).Warn("source relay state update failed")
		}
	}
	return nil
}

func sourceKey(eventID, actionID string) string {
	return "alarm-" + eventID + "-" + actionID
}

var sourceBridgeMu sync.RWMutex
var activeSourceBridge *SourceBridge

func SetActiveSourceBridge(bridge *SourceBridge) {
	sourceBridgeMu.Lock()
	activeSourceBridge = bridge
	sourceBridgeMu.Unlock()
}

func currentSourceBridge() *SourceBridge {
	sourceBridgeMu.RLock()
	defer sourceBridgeMu.RUnlock()
	return activeSourceBridge
}

func SwitchNotificationGroupToEncore(ctx context.Context, request SourceGroupProjectionRequest, idempotencyKey string, expectedRouteVersion int64) error {
	bridge := currentSourceBridge()
	if bridge == nil || !bridge.Enabled() {
		return errors.New("source bridge disabled")
	}
	return bridge.SwitchToEncore(ctx, request, idempotencyKey, expectedRouteVersion)
}

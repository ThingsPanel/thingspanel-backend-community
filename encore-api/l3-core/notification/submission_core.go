package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
)

type CoreFailure struct {
	Status int
	Reason string
}

func (e *CoreFailure) Error() string { return "notification: " + e.Reason }
func coreFailure(status int, reason string) error {
	return &CoreFailure{Status: status, Reason: reason}
}

type coreSubmissionStore interface {
	SubmissionStore
	FindSubmissionReplay(context.Context, string, string, string) (contract.AcceptedData, bool, error)
	FindSourceReplay(context.Context, string, string, string, string, string, string) (contract.AcceptedData, bool, error)
	AcceptSourcePlan(context.Context, string, string, string, SourceAcceptancePlan) (contract.AcceptedData, error)
}

type Core struct {
	Store          coreSubmissionStore
	Planner        SourceEventPlanner
	ValidateTarget func(context.Context, string, contract.SubmitDelivery) error
	Now            func() time.Time
}

func requireNotificationScope(principal UserPrincipal, scope string) error {
	if principal.UserID == "" {
		return coreFailure(401, "authentication_required")
	}
	if principal.TenantID == "" {
		if containsRole(principal.Roles, "SYS_ADMIN") {
			return coreFailure(403, "tenant_context_required")
		}
		return coreFailure(401, "authentication_required")
	}
	for _, allowed := range principal.Scopes {
		if allowed == scope {
			return nil
		}
	}
	return coreFailure(403, "permission_denied")
}

func validIdempotencyKey(key string) bool {
	return len(key) >= 8 && len(key) <= 128 && strings.TrimSpace(key) == key && !strings.ContainsAny(key, "\r\n\x00")
}

func (c *Core) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Core) Submit(ctx context.Context, p UserPrincipal, key string, request contract.NotificationSubmit) (contract.AcceptedData, error) {
	if err := requireNotificationScope(p, "notification:send"); err != nil {
		return contract.AcceptedData{}, err
	}
	accepted, err := c.acceptUserIntent(ctx, p.TenantID, key, request)
	var storeError *StoreError
	if errors.As(err, &storeError) && storeError.Kind == StoreErrorNotFound {
		// An explicit multi-target intent is rejected as a whole when a target
		// disappears or is outside this tenant. Direct resource APIs keep 404.
		return contract.AcceptedData{}, coreFailure(422, "target_unavailable")
	}
	return accepted, err
}

func (c *Core) TestSend(ctx context.Context, p UserPrincipal, instanceID, key string, request contract.TestSendRequest) (contract.AcceptedData, error) {
	for _, scope := range []string{"notification:manage", "notification:send_test"} {
		if err := requireNotificationScope(p, scope); err != nil {
			return contract.AcceptedData{}, err
		}
	}
	if strings.TrimSpace(instanceID) == "" {
		return contract.AcceptedData{}, coreFailure(422, "invalid_instance")
	}
	return c.acceptUserIntent(ctx, p.TenantID, key, contract.NotificationSubmit{Source: contract.SourceRef{Type: "test", ID: instanceID}, ExpiresAt: request.ExpiresAt, Deliveries: []contract.SubmitDelivery{{InstanceID: instanceID, Recipient: request.Recipient, Content: request.Content}}})
}

func (c *Core) acceptUserIntent(ctx context.Context, tenant, key string, request contract.NotificationSubmit) (contract.AcceptedData, error) {
	if c.Store == nil || c.ValidateTarget == nil {
		return contract.AcceptedData{}, coreFailure(503, "notification_unavailable")
	}
	if !validIdempotencyKey(key) {
		return contract.AcceptedData{}, coreFailure(400, "invalid_idempotency_key")
	}
	normalized, err := normalizeNotificationIntent(request)
	if err != nil {
		return contract.AcceptedData{}, err
	}
	digest, err := NotificationIntentDigest(normalized)
	if err != nil {
		return contract.AcceptedData{}, coreFailure(422, "invalid_intent")
	}
	// Authorize every call, but replay before expiry/current resource checks.
	// A paused instance or elapsed deadline cannot turn an accepted retry into
	// a new send or make its original notification identity disappear.
	if replay, found, err := c.Store.FindSubmissionReplay(ctx, tenant, key, digest); err != nil {
		return contract.AcceptedData{}, err
	} else if found {
		return replay, nil
	}
	expires, _ := time.Parse(time.RFC3339Nano, normalized.ExpiresAt)
	if !expires.After(c.now()) {
		return contract.AcceptedData{}, coreFailure(422, "intent_expired")
	}
	for _, delivery := range normalized.Deliveries {
		if err := c.ValidateTarget(ctx, tenant, delivery); err != nil {
			return contract.AcceptedData{}, err
		}
	}
	return c.Store.AcceptSubmission(ctx, tenant, key, digest, normalized)
}

func (c *Core) ReceiveSourceEvent(ctx context.Context, p SourcePrincipal, key string, event contract.SourceEventV1) (contract.AcceptedData, error) {
	if c.Store == nil || c.Planner == nil {
		return contract.AcceptedData{}, coreFailure(503, "notification_unavailable")
	}
	allowed := false
	for _, tenant := range p.TenantIDs {
		if tenant == event.TenantID {
			allowed = true
			break
		}
	}
	if p.DeploymentID == "" || p.DeploymentID != event.SourceDeploymentID || !allowed {
		return contract.AcceptedData{}, coreFailure(403, "source_scope_mismatch")
	}
	if !validIdempotencyKey(key) {
		return contract.AcceptedData{}, coreFailure(400, "invalid_idempotency_key")
	}
	if event.SchemaVersion != "1.0" || strings.TrimSpace(event.SourceEventID) == "" || strings.TrimSpace(event.ActionID) == "" || strings.TrimSpace(event.NotificationGroupID) == "" || event.GroupRevision < 1 || event.Payload.Variables == nil {
		return contract.AcceptedData{}, coreFailure(422, "invalid_source_event")
	}
	occurred, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if err != nil {
		return contract.AcceptedData{}, coreFailure(422, "invalid_source_event")
	}
	expires, err := time.Parse(time.RFC3339Nano, event.ExpiresAt)
	if err != nil || !expires.After(occurred) {
		return contract.AcceptedData{}, coreFailure(422, "invalid_source_event")
	}
	digest, err := NotificationIntentDigest(event)
	if err != nil {
		return contract.AcceptedData{}, coreFailure(422, "invalid_source_event")
	}
	if replay, found, err := c.Store.FindSourceReplay(ctx, event.TenantID, key, digest, event.SourceDeploymentID, event.SourceEventID, event.ActionID); err != nil {
		return contract.AcceptedData{}, err
	} else if found {
		return replay, nil
	}
	plan, err := c.Planner.PlanSourceEvent(ctx, event.TenantID, event)
	if err != nil {
		return contract.AcceptedData{}, err
	}
	return c.Store.AcceptSourcePlan(ctx, event.TenantID, key, digest, plan)
}

func normalizeNotificationIntent(request contract.NotificationSubmit) (contract.NotificationSubmit, error) {
	switch request.Source.Type {
	case "alarm", "automation", "manual", "test":
	default:
		return request, coreFailure(422, "invalid_source")
	}
	if strings.TrimSpace(request.Source.ID) == "" || len(request.Deliveries) < 1 || len(request.Deliveries) > 500 {
		return request, coreFailure(422, "invalid_intent")
	}
	if _, err := time.Parse(time.RFC3339Nano, request.ExpiresAt); err != nil {
		return request, coreFailure(422, "invalid_expiry")
	}
	result := request
	result.Deliveries = make([]contract.SubmitDelivery, 0, len(request.Deliveries))
	seen := map[string]bool{}
	for _, delivery := range request.Deliveries {
		if strings.TrimSpace(delivery.InstanceID) == "" || strings.TrimSpace(delivery.Recipient.Address) == "" {
			return request, coreFailure(422, "invalid_target")
		}
		switch delivery.Recipient.Kind {
		case "email", "phone", "chat_id", "user_id", "webhook":
		default:
			return request, coreFailure(422, "invalid_target")
		}
		switch delivery.Content.Kind {
		case "text":
			if delivery.Content.Text == nil || strings.TrimSpace(*delivery.Content.Text) == "" || delivery.Content.Template != nil {
				return request, coreFailure(422, "invalid_content")
			}
		case "template":
			if delivery.Content.Text != nil || delivery.Content.Template == nil || strings.TrimSpace(delivery.Content.Template.ID) == "" || delivery.Content.Template.Params == nil {
				return request, coreFailure(422, "invalid_content")
			}
		default:
			return request, coreFailure(422, "invalid_content")
		}
		wire, err := canonicalNotificationJSON(delivery)
		if err != nil {
			return request, coreFailure(422, "invalid_intent")
		}
		if !seen[string(wire)] {
			result.Deliveries = append(result.Deliveries, delivery)
			seen[string(wire)] = true
		}
	}
	return result, nil
}

// NotificationIntentDigest sorts object keys, including dynamic JSON subtrees.
// It contains the declared intent, never mutable current configuration or auth.
func NotificationIntentDigest(value any) (string, error) {
	wire, err := canonicalNotificationJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(wire)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalNotificationJSON(value any) ([]byte, error) {
	wire, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("invalid_notification_intent")
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	var decoded any
	if decoder.Decode(&decoded) != nil {
		return nil, errors.New("invalid_notification_intent")
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, errors.New("invalid_notification_intent")
	}
	return canonical, nil
}

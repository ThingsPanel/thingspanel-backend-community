package contract

import "encoding/json"

type Channel string

const (
	ChannelEmail   Channel = "email"
	ChannelSMS     Channel = "sms"
	ChannelVoice   Channel = "voice"
	ChannelIM      Channel = "im"
	ChannelWebhook Channel = "webhook"
)

type DispatchStatus string

const (
	DispatchQueued   DispatchStatus = "queued"
	DispatchSending  DispatchStatus = "sending"
	DispatchAccepted DispatchStatus = "accepted"
	DispatchFailed   DispatchStatus = "failed"
	DispatchUnknown  DispatchStatus = "unknown"
)

type DeliveryStatus string

const (
	DeliveryUnsupported DeliveryStatus = "unsupported"
	DeliveryPending     DeliveryStatus = "pending"
	DeliveryDelivered   DeliveryStatus = "delivered"
	DeliveryFailed      DeliveryStatus = "failed"
	DeliveryUnknown     DeliveryStatus = "unknown"
)

type IntakeStatus string

const (
	IntakeReady   IntakeStatus = "ready"
	IntakeBlocked IntakeStatus = "blocked"
)

type SourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Recipient struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
}

// Content uses Kind as a tagged union discriminator. Validation must ensure
// only the matching payload is present and reject explicit nulls.
type Content struct {
	Kind     string           `json:"kind"`
	Title    *string          `json:"title,omitempty"`
	Text     *string          `json:"text,omitempty"`
	Template *TemplateContent `json:"template,omitempty"`
}

type TemplateContent struct {
	ID     string                     `json:"id"`
	Locale *string                    `json:"locale,omitempty"`
	Params map[string]json.RawMessage `json:"params" strictjson:"dynamic-values"`
}

type SubmitDelivery struct {
	InstanceID string    `json:"instanceId"`
	Recipient  Recipient `json:"recipient"`
	Content    Content   `json:"content"`
}

type NotificationSubmit struct {
	Source     SourceRef        `json:"source"`
	Deliveries []SubmitDelivery `json:"deliveries"`
	ExpiresAt  string           `json:"expiresAt"`
}

type PluginRegistration struct {
	PluginID       string `json:"pluginId"`
	PluginVersion  string `json:"pluginVersion"`
	Origin         string `json:"origin"`
	AuthSecret     string `json:"authSecret" encore:"sensitive"`
	ManifestDigest string `json:"manifestDigest"`
}

type PluginUpdate struct {
	ExpectedVersion int64 `json:"expectedVersion"`
	Enabled         bool  `json:"enabled"`
}

// PluginGrantUpdate is an explicit SYS_ADMIN action on one tenant's grant.
// ExpectedVersion zero creates a missing grant; it never selects an identity.
type PluginGrantUpdate struct {
	PluginRegistrationID string `json:"pluginRegistrationId"`
	TenantID             string `json:"tenantId"`
	Enabled              bool   `json:"enabled"`
	ExpectedVersion      int64  `json:"expectedVersion"`
}

type PluginGrantView struct {
	PluginRegistrationID string `json:"pluginRegistrationId"`
	TenantID             string `json:"tenantId"`
	Enabled              bool   `json:"enabled"`
	Version              int64  `json:"version"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

type PluginView struct {
	ID             string                `json:"id"`
	PluginID       string                `json:"pluginId"`
	PluginVersion  string                `json:"pluginVersion"`
	Origin         string                `json:"origin"`
	ManifestDigest string                `json:"manifestDigest"`
	Manifest       PluginManifestSummary `json:"manifest"`
	Enabled        bool                  `json:"enabled"`
	Health         string                `json:"health"`
	Version        int64                 `json:"version"`
}

type PluginManifestSummary struct {
	PluginID        string             `json:"pluginId"`
	Name            string             `json:"name"`
	PluginVersion   string             `json:"pluginVersion"`
	IdentityFields  []string           `json:"identityFields,omitempty"`
	Channels        []Channel          `json:"channels"`
	ContentModes    []string           `json:"contentModes"`
	ConfigSchema    json.RawMessage    `json:"configSchema"`
	RecipientSchema json.RawMessage    `json:"recipientSchema"`
	SecretFields    []string           `json:"secretFields"`
	Capabilities    PluginCapabilities `json:"capabilities"`
}

type PluginCapabilities struct {
	DeliveryReceipts bool `json:"deliveryReceipts"`
}

// SecretPatch deliberately distinguishes an omitted field from an explicit
// set or clear. A duplicate-key-aware request decoder must run before this DTO.
type SecretPatch struct {
	Set   map[string]string `json:"set,omitempty" encore:"sensitive"`
	Clear []string          `json:"clear,omitempty"`
}

type InstanceCreate struct {
	PluginRegistrationID string            `json:"pluginRegistrationId"`
	Name                 string            `json:"name"`
	Channel              Channel           `json:"channel"`
	Config               json.RawMessage   `json:"config" encore:"sensitive" strictjson:"dynamic-object"`
	ProviderIdentity     map[string]string `json:"providerIdentity"`
}

type InstanceUpdate struct {
	ExpectedVersion int64           `json:"expectedVersion"`
	Name            *string         `json:"name,omitempty"`
	ConfigPatch     json.RawMessage `json:"configPatch,omitempty" encore:"sensitive" strictjson:"dynamic-object"`
	Secrets         *SecretPatch    `json:"secrets,omitempty" encore:"sensitive"`
	Enabled         *bool           `json:"enabled,omitempty"`
}

type InstanceValidate struct {
	InstanceID      *string         `json:"instanceId,omitempty"`
	ExpectedVersion *int64          `json:"expectedVersion,omitempty"`
	ConfigPatch     json.RawMessage `json:"configPatch,omitempty" encore:"sensitive" strictjson:"dynamic-object"`
	Draft           *InstanceDraft  `json:"draft,omitempty"`
}

type InstanceDraft struct {
	PluginRegistrationID string          `json:"pluginRegistrationId"`
	Channel              Channel         `json:"channel"`
	Config               json.RawMessage `json:"config" encore:"sensitive" strictjson:"dynamic-object"`
}

type InstanceView struct {
	ID                   string                     `json:"id"`
	PluginRegistrationID string                     `json:"pluginRegistrationId"`
	Name                 string                     `json:"name"`
	Channel              Channel                    `json:"channel"`
	Config               map[string]json.RawMessage `json:"config" strictjson:"dynamic-values"`
	ProviderIdentity     map[string]string          `json:"providerIdentity"`
	Enabled              bool                       `json:"enabled"`
	Version              int64                      `json:"version"`
	ConfigVersion        int64                      `json:"configVersion"`
	SecretState          map[string]bool            `json:"secretState"`
}

type TestSendRequest struct {
	Recipient Recipient `json:"recipient"`
	Content   Content   `json:"content"`
	ExpiresAt string    `json:"expiresAt"`
}

type ReceiptCredentialResponse struct {
	Credential string `json:"credential" encore:"sensitive"`
	ExpiresAt  string `json:"expiresAt"`
}

type GroupCreate struct {
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Bindings []GroupBinding `json:"bindings"`
}

type GroupUpdate struct {
	ExpectedVersion int64 `json:"expectedVersion"`
	GroupCreate
}

type GroupBinding struct {
	BindingID       string          `json:"bindingId"`
	InstanceID      string          `json:"instanceId"`
	RecipientSource json.RawMessage `json:"recipientSource"`
	ContentBinding  json.RawMessage `json:"contentBinding"`
}

type GroupView struct {
	ID                string                  `json:"id"`
	Name              string                  `json:"name"`
	Enabled           bool                    `json:"enabled"`
	Bindings          []GroupBinding          `json:"bindings"`
	SourceProjections []SourceGroupProjection `json:"sourceProjections,omitempty"`
	Revision          int64                   `json:"revision"`
	Version           int64                   `json:"version"`
	MigrationState    string                  `json:"migrationState"`
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

const MaxSourceGroupProjectionRevisions = 128

type ValidationResult struct {
	Valid  bool         `json:"valid"`
	Errors []FieldError `json:"errors"`
}

type NotificationView struct {
	ID            string            `json:"id"`
	Source        SourceRef         `json:"source"`
	IntakeStatus  IntakeStatus      `json:"intakeStatus"`
	BlockedReason string            `json:"blockedReason,omitempty"`
	CreatedAt     string            `json:"createdAt"`
	Deliveries    []DeliverySummary `json:"deliveries"`
}

type DeliverySummary struct {
	DeliveryID        string         `json:"deliveryId"`
	DispatchStatus    DispatchStatus `json:"dispatchStatus"`
	DeliveryStatus    DeliveryStatus `json:"deliveryStatus"`
	AttemptCount      int            `json:"attemptCount"`
	ProviderMessageID string         `json:"providerMessageId,omitempty"`
}

type DeliveryView struct {
	DeliverySummary
	NotificationID string            `json:"notificationId"`
	InstanceID     string            `json:"instanceId"`
	ConfigVersion  int64             `json:"configVersion"`
	Recipient      RedactedRecipient `json:"recipient"`
	Source         SourceRef         `json:"source"`
	CreatedAt      string            `json:"createdAt"`
	Error          *RedactedError    `json:"error,omitempty"`
}

type RedactedRecipient struct {
	Kind    string `json:"kind"`
	Display string `json:"display"`
}

type RedactedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

type SourceEventV1 struct {
	SchemaVersion       string             `json:"schemaVersion"`
	SourceDeploymentID  string             `json:"sourceDeploymentId"`
	SourceEventID       string             `json:"sourceEventId"`
	ActionID            string             `json:"actionId"`
	TenantID            string             `json:"tenantId"`
	NotificationGroupID string             `json:"notificationGroupId"`
	GroupRevision       int64              `json:"groupRevision"`
	OccurredAt          string             `json:"occurredAt"`
	ExpiresAt           string             `json:"expiresAt"`
	Payload             SourceEventPayload `json:"payload"`
}

type SourceEventPayload struct {
	Subject         string                     `json:"subject"`
	Text            string                     `json:"text"`
	Variables       map[string]json.RawMessage `json:"variables" strictjson:"dynamic-values"`
	LegacyAlertJSON map[string]json.RawMessage `json:"legacyAlertJson,omitempty" strictjson:"dynamic-values"`
}

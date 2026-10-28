package northbound

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

// ContractVersion is the single wire contract identifier. The management API
// version and the plugin contract version move independently, but both are
// frozen strings so a mismatch is detectable instead of silently tolerated.
const (
	ContractVersion = "northbound-connector/v1"
	// SourceContractVersion identifies the Community -> Encore handoff payload.
	SourceContractVersion = "northbound-source/v1"
)

// Delivery states. These are the only values written to nb_deliveries.state.
const (
	StatePending   = "pending"
	StateLeased    = "leased"
	StateRetryWait = "retry_wait"
	StateDelivered = "delivered"
	StateUnknown   = "unknown"
	StateDead      = "dead"
)

// Pipeline states. Pausing keeps backlog and stops new fan-out; disabling stops
// new fan-out and stops delivery attempts. Deleting never removes backlog.
const (
	PipelineDraft    = "draft"
	PipelineActive   = "active"
	PipelinePaused   = "paused"
	PipelineDisabled = "disabled"
)

// Ack levels distinguish what the platform actually knows.
const (
	AckAccepted         = "accepted"
	AckDestinationAcked = "destination_acked"
	AckEffectConfirmed  = "effect_confirmed"
	AckNone             = "none"
)

// Plugin-reported outcome classes. The plugin classifies against its target
// semantics; the substrate owns the retry policy.
const (
	OutcomeDelivered = "delivered"
	OutcomeRetryable = "retryable_failure"
	OutcomePermanent = "permanent_failure"
	OutcomeUnknown   = "unknown"
)

// ---------------------------------------------------------------------------
// Connector descriptor (what a plugin declares about itself)
// ---------------------------------------------------------------------------

// Descriptor is returned by GET /v1/describe on a plugin.
type Descriptor struct {
	ContractVersion string          `json:"contractVersion"`
	ID              string          `json:"id"`
	Version         string          `json:"version"`
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description,omitempty"`
	Capabilities    Capabilities    `json:"capabilities"`
	ConfigSchema    json.RawMessage `json:"configSchema"`
	SecretFields    []string        `json:"secretFields"`
	Limits          Limits          `json:"limits"`
}

// Capabilities states what a connector can honestly claim.
type Capabilities struct {
	EventTypes     []string `json:"eventTypes"`
	Batch          bool     `json:"batch"`
	AckLevel       string   `json:"ackLevel"`
	Idempotency    string   `json:"idempotency"` // none | conditional | strong
	Verify         bool     `json:"verify"`
	MaxBatchEvents int      `json:"maxBatchEvents"`
}

// Limits are the plugin's own constraints, enforced before sending.
type Limits struct {
	MaxBatchEvents  int `json:"maxBatchEvents"`
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	MaxInFlight     int `json:"maxInFlight"`
}

// ---------------------------------------------------------------------------
// Management API transport types
// ---------------------------------------------------------------------------

// ConnectorView is the catalog entry the page shows. It includes real health,
// not a static "installed" flag.
type ConnectorView struct {
	ID              string          `json:"id"`
	Version         string          `json:"version"`
	ContractVersion string          `json:"contractVersion"`
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description"`
	Capabilities    Capabilities    `json:"capabilities"`
	ConfigSchema    json.RawMessage `json:"configSchema"`
	SecretFields    []string        `json:"secretFields"`
	State           string          `json:"state"`
	ArtifactDigest  string          `json:"artifactDigest"`
	LastSeenAt      *time.Time      `json:"lastSeenAt,omitempty"`
	LastError       string          `json:"lastError,omitempty"`
}

type ConnectorListResponse struct {
	ContractVersion string          `json:"contractVersion"`
	Connectors      []ConnectorView `json:"connectors"`
}

// ConnectionInput is write-only for secrets: the request may carry them, the
// response never echoes them.
type ConnectionInput struct {
	Name        string            `json:"name"`
	ConnectorID string            `json:"connectorId"`
	Config      json.RawMessage   `json:"config"`
	Secrets     map[string]string `json:"secrets,omitempty"`
	// ExpectedRevision is required on update. Zero means "create".
	ExpectedRevision int64 `json:"expectedRevision,omitempty"`
}

type ConnectionView struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	ConnectorID   string          `json:"connectorId"`
	ConnectorName string          `json:"connectorName,omitempty"`
	Config        json.RawMessage `json:"config"`
	SecretFields  []string        `json:"secretFieldsSet"`
	Revision      int64           `json:"revision"`
	State         string          `json:"state"`
	FailureCode   string          `json:"failureCode,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type ConnectionListResponse struct {
	Connections []ConnectionView `json:"connections"`
}

type ConnectionTestRequest struct {
	// Optional inline overrides so a user can test before saving.
	Config  json.RawMessage   `json:"config,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

type ConnectionTestResponse struct {
	OK          bool    `json:"ok"`
	State       string  `json:"state"`
	FailureCode string  `json:"failureCode,omitempty"`
	Message     string  `json:"message,omitempty"`
	Checks      []Check `json:"checks"`
}

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// PipelineInput carries the whole configuration so the preview and the saved
// version are computed from exactly the same bytes.
type PipelineInput struct {
	Name             string          `json:"name"`
	ConnectionID     string          `json:"connectionId"`
	Filter           json.RawMessage `json:"filter"`
	Transform        json.RawMessage `json:"transform"`
	Serialization    json.RawMessage `json:"serialization,omitempty"`
	ExpectedRevision int64           `json:"expectedRevision,omitempty"`
}

type PipelineView struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	ConnectionID   string          `json:"connectionId"`
	ConnectionName string          `json:"connectionName,omitempty"`
	ConnectorID    string          `json:"connectorId,omitempty"`
	Filter         json.RawMessage `json:"filter"`
	Transform      json.RawMessage `json:"transform"`
	Serialization  json.RawMessage `json:"serialization"`
	State          string          `json:"state"`
	Revision       int64           `json:"revision"`
	ActiveVersion  *int64          `json:"activeVersion,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	Backlog        Backlog         `json:"backlog"`
}

type PipelineListResponse struct {
	Pipelines []PipelineView `json:"pipelines"`
}

type Backlog struct {
	Pending     int64      `json:"pending"`
	RetryWait   int64      `json:"retryWait"`
	Unknown     int64      `json:"unknown"`
	Dead        int64      `json:"dead"`
	OldestAt    *time.Time `json:"oldestAt,omitempty"`
	OldestAgeMS int64      `json:"oldestAgeMs"`
}

type PreviewRequest struct {
	// Sample is a raw source event envelope, the same shape the Community
	// runtime sends to /inbox/events.
	Sample json.RawMessage `json:"sample"`
}

type PreviewResponse struct {
	Matched        bool            `json:"matched"`
	MatchReason    string          `json:"matchReason"`
	EventID        string          `json:"eventId,omitempty"`
	EventType      string          `json:"eventType,omitempty"`
	Subject        string          `json:"subject,omitempty"`
	Output         json.RawMessage `json:"output,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Warnings       []string        `json:"warnings"`
}

type TestSendRequest struct {
	Sample json.RawMessage `json:"sample,omitempty"`
}

type TestSendResponse struct {
	Accepted   bool   `json:"accepted"`
	DeliveryID string `json:"deliveryId,omitempty"`
	State      string `json:"state,omitempty"`
	EventID    string `json:"eventId,omitempty"`
	Message    string `json:"message,omitempty"`
}

type DeliveryView struct {
	ID             string          `json:"id"`
	EventID        string          `json:"eventId"`
	PipelineID     string          `json:"pipelineId"`
	PipelineName   string          `json:"pipelineName,omitempty"`
	ConnectionID   string          `json:"connectionId"`
	ConnectionName string          `json:"connectionName,omitempty"`
	DeviceID       string          `json:"deviceId"`
	EventType      string          `json:"eventType"`
	State          string          `json:"state"`
	AckLevel       string          `json:"ackLevel,omitempty"`
	Attempts       int             `json:"attempts"`
	NextAttemptAt  time.Time       `json:"nextAttemptAt"`
	FailureCode    string          `json:"failureCode,omitempty"`
	FailureDetail  string          `json:"failureDetail,omitempty"`
	Receipt        json.RawMessage `json:"receipt,omitempty"`
	OccurredAt     time.Time       `json:"occurredAt"`
	CreatedAt      time.Time       `json:"createdAt"`
	DeliveredAt    *time.Time      `json:"deliveredAt,omitempty"`
}

type DeliveryListResponse struct {
	Deliveries []DeliveryView `json:"deliveries"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type DeliveryListRequest struct {
	PipelineID string `json:"pipelineId,omitempty"`
	State      string `json:"state,omitempty"`
	DeviceID   string `json:"deviceId,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
}

type ReplayRequest struct {
	// Reason is recorded; replay keeps the original idempotency key.
	Reason string `json:"reason"`
	// AcknowledgeRisk must be set to replay a delivery whose previous result is
	// unknown and whose connector cannot deduplicate.
	AcknowledgeRisk bool `json:"acknowledgeRisk,omitempty"`
}

type ReplayResponse struct {
	DeliveryID string `json:"deliveryId"`
	State      string `json:"state"`
	Message    string `json:"message,omitempty"`
}

type OverviewResponse struct {
	ContractVersion string          `json:"contractVersion"`
	TenantID        string          `json:"tenantId"`
	Capabilities    OverviewCaps    `json:"capabilities"`
	Connectors      []ConnectorView `json:"connectors"`
	Connections     int64           `json:"connections"`
	Pipelines       int64           `json:"pipelines"`
	ActivePipelines int64           `json:"activePipelines"`
	Deliveries      OverviewCounts  `json:"deliveries"`
	Rejects         RejectCounts    `json:"rejects"`
	GeneratedAt     time.Time       `json:"generatedAt"`
}

// OverviewCaps tells the page what this deployment can honestly do, so the UI
// never claims a boundary the runtime does not have.
type OverviewCaps struct {
	SourceProfile    string `json:"sourceProfile"` // ingress_v1 | legacy | unknown
	GuaranteeStart   string `json:"guaranteeStart"`
	MaxBacklogEvents int64  `json:"maxBacklogEvents"`
	RetentionDays    int    `json:"retentionDays"`
}

type OverviewCounts struct {
	Pending     int64      `json:"pending"`
	RetryWait   int64      `json:"retryWait"`
	Delivered   int64      `json:"delivered"`
	Unknown     int64      `json:"unknown"`
	Dead        int64      `json:"dead"`
	OldestAt    *time.Time `json:"oldestAt,omitempty"`
	OldestAgeMS int64      `json:"oldestAgeMs"`
}

// RejectCounts separates "enrolled and waiting" from "never enrolled".
type RejectCounts struct {
	CapacityRejected int64      `json:"capacityRejected"`
	LastRejectAt     *time.Time `json:"lastRejectAt,omitempty"`
	LastRejectCode   string     `json:"lastRejectCode,omitempty"`
}

// ---------------------------------------------------------------------------
// Source inbox transport types (Community runtime -> Encore)
// ---------------------------------------------------------------------------

// SourceEnvelope is the frozen handoff shape. `eventId` is generated at the
// source durable boundary and never changes, so replay and dedup are stable.
type SourceEnvelope struct {
	ContractVersion string          `json:"contractVersion"`
	EventID         string          `json:"eventId"`
	TenantID        string          `json:"tenantId"`
	DeviceID        string          `json:"deviceId"`
	EventType       string          `json:"eventType"`
	OccurredAt      time.Time       `json:"occurredAt"`
	ReceivedAt      time.Time       `json:"receivedAt"`
	Source          string          `json:"source"`
	Subject         string          `json:"subject"`
	Data            json.RawMessage `json:"data"`
}

type InboxRequest struct {
	// ServiceToken is the Community runtime credential. It is carried in a
	// header so it never becomes part of a stored event or a log line.
	ServiceToken    string           `header:"X-Northbound-Token"`
	ContractVersion string           `json:"contractVersion"`
	SourceProfile   string           `json:"sourceProfile,omitempty"`
	Events          []SourceEnvelope `json:"events"`
}

type InboxResponse struct {
	Accepted  int              `json:"accepted"`
	Duplicate int              `json:"duplicate"`
	Rejected  []InboxRejection `json:"rejected"`
	Fanout    int              `json:"fanout"`
}

type InboxRejection struct {
	EventID string `json:"eventId"`
	Code    string `json:"code"`
	Detail  string `json:"detail,omitempty"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func errBadRequest(msg string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: msg}
}

func errNotFound(msg string) error {
	return &errs.Error{Code: errs.NotFound, Message: msg}
}

// errConflict reports a state conflict: a stale revision, a record that is
// already in flight, or a state that does not allow the action. It maps to
// HTTP 409. errs.FailedPrecondition is deliberately not used because gRPC maps
// it to HTTP 400, which would tell the caller "your request was malformed"
// instead of "the resource changed, reload it".
func errConflict(msg string) error {
	return &errs.Error{Code: errs.Aborted, Message: msg}
}

func errDenied(msg string) error {
	return &errs.Error{Code: errs.PermissionDenied, Message: msg}
}

func errUnavailable(msg string) error {
	return &errs.Error{Code: errs.Unavailable, Message: msg}
}

// IdempotencyKey is stable across retries, plugin upgrades and pipeline
// version bumps. It deliberately excludes the pipeline version and the attempt
// counter: changing those must not create a second business write.
func IdempotencyKey(tenantID, pipelineID, eventID, branch string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenantID, pipelineID, eventID, branch}, "\x00")))
	return "nb1:" + hex.EncodeToString(sum[:])
}

// DeliveryID is deterministic for the same logical branch, so a duplicate
// inbox delivery cannot create a second ledger row even under concurrency.
func DeliveryID(tenantID, pipelineID, eventID, branch string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("nb-delivery\x00"+strings.Join([]string{tenantID, pipelineID, eventID, branch}, "\x00"))).String()
}

func sha256Bytes(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func isValidEventType(t string) bool {
	switch t {
	case "telemetry", "attribute", "event":
		return true
	}
	return false
}

func parseUUID(raw, field string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errBadRequest(field + " must be a UUID")
	}
	return id.String(), nil
}

var errNoServiceToken = errors.New("northbound service token is not configured")

func redact(s string) string {
	if len(s) > 512 {
		return s[:512] + "...(truncated)"
	}
	return s
}

func mustJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(fallback)
	}
	return raw
}

func formatDuration(d time.Duration) string { return fmt.Sprintf("%.3fs", d.Seconds()) }

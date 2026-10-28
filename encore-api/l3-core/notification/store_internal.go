package notification

import (
	"context"
	"time"

	"encore.app/l3-core/notification/contract"
)

// GroupManagementRepository is the storage boundary for immutable group
// revisions. The L3 service validates bindings before calling it; SQL also
// enforces tenant ownership with composite foreign keys.
type GroupManagementRepository interface {
	CreateGroup(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.GroupCreate) (contract.GroupView, error)
	UpdateGroup(ctx context.Context, tenantID, groupID, idempotencyKey, requestDigest string, request contract.GroupUpdate) (contract.GroupView, error)
	GetGroup(ctx context.Context, tenantID, groupID string) (contract.GroupView, error)
	ListGroups(ctx context.Context, tenantID string) ([]contract.GroupView, error)
}

// ManagementReplay identifies the immutable result recorded for a management
// operation. The caller must still enforce the principal's current scope.
type ManagementReplay struct {
	ResourceType string
	ResourceID   string
	Version      int64
}

type SubmissionReplayStore interface {
	FindSubmissionReplay(ctx context.Context, tenantID, idempotencyKey, intentDigest string) (contract.AcceptedData, bool, error)
	FindSourceReplay(ctx context.Context, tenantID, idempotencyKey, intentDigest, deploymentID, sourceEventID, actionID string) (contract.AcceptedData, bool, error)
}

// DeliveryQueryStore keeps every list and count scoped to the verified tenant.
type DeliveryQueryStore interface {
	ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) (DeliveryPage, error)
	GetDelivery(ctx context.Context, tenantID, deliveryID string) (contract.DeliveryView, error)
}

type DeliveryFilter struct {
	Page           int
	PageSize       int
	InstanceID     string
	SourceType     string
	SourceID       string
	DispatchStatus contract.DispatchStatus
	DeliveryStatus contract.DeliveryStatus
	From           *time.Time
	To             *time.Time
}

type DeliveryPage struct {
	Items    []contract.DeliveryView `json:"items"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"pageSize"`
	Total    int64                   `json:"total"`
}

// SourceAcceptancePlan is prepared by the notification domain service after
// source principal checks, published-group resolution, and all-or-nothing
// recipient expansion. A blocked plan carries a stable reason code and no
// delivery targets.
type SourceAcceptancePlan struct {
	Event         contract.SourceEventV1
	Deliveries    []contract.SubmitDelivery
	BlockedReason string
}

// SourceEventPlanner is a domain-owned dependency. It must not infer identity
// from the event body; Store independently checks source deployment/tenant
// scope and pins the group revision in its transaction.
type SourceEventPlanner interface {
	PlanSourceEvent(ctx context.Context, tenantID string, event contract.SourceEventV1) (SourceAcceptancePlan, error)
}

// PluginRuntime is returned only to the worker after a delivery claim. It is
// deliberately not a wire or API DTO and must not be logged or serialized.
type PluginRuntime struct {
	RegistrationID      string
	RegistrationVersion int64
	Origin              string
	AuthSecret          string
}

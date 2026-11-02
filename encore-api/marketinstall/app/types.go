// Package app is the sole durable A08 coordinator. Providers own business
// resources; this package owns only installation intent and step progression.
package app

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrInvalid   = errors.New("marketinstall: invalid request")
	ErrConflict  = errors.New("marketinstall: request identity conflict")
	ErrNotFound  = errors.New("marketinstall: not found")
	ErrLeaseLost = errors.New("marketinstall: execution lease lost")
)

const (
	Queued      = "queued"
	Running     = "running"
	Paused      = "paused"
	Completed   = "completed"
	Rejected    = "rejected"
	Confirmed   = "confirmed"
	NotObserved = "not_observed"
	Expired     = "expired"
	Unknown     = "unknown"
)

type Identity struct {
	TenantID         string `json:"tenantId"`
	SubjectID        string `json:"subjectId"`
	AuthorizationRef string `json:"authorizationRef"`
}
type Binding struct {
	BindingKey    string `json:"bindingKey"`
	LocalDeviceID string `json:"localDeviceId"`
}
type Request struct {
	BundleKey       string    `json:"bundleKey"`
	Version         string    `json:"version"`
	OverwritePolicy string    `json:"overwritePolicy,omitempty"`
	DeviceBindings  []Binding `json:"deviceBindings,omitempty"`
}
type PrepareRequest struct {
	ProtocolVersion int `json:"protocolVersion"`
	Identity
	Request Request `json:"request"`
}
type Step struct {
	ID          string          `json:"id"`
	Owner       string          `json:"owner"`
	Action      string          `json:"action"`
	ResourceKey string          `json:"resourceKey"`
	Payload     json.RawMessage `json:"payload"`
}
type Plan struct {
	ArtifactDigest string `json:"artifactDigest"`
	Steps          []Step `json:"steps"`
}
type Receipt struct {
	Owner       string          `json:"owner"`
	ResourceID  string          `json:"resourceId"`
	PayloadHash string          `json:"payloadHash"`
	Data        json.RawMessage `json:"data"`
}
type PriorResult struct {
	StepID  string  `json:"stepId"`
	Receipt Receipt `json:"receipt"`
}
type StepRequest struct {
	ProtocolVersion int `json:"protocolVersion"`
	Identity
	OperationID    string        `json:"operationId"`
	Generation     int64         `json:"generation"`
	StepID         string        `json:"stepId"`
	IdempotencyKey string        `json:"idempotencyKey"`
	PayloadHash    string        `json:"payloadHash"`
	Step           Step          `json:"step"`
	PriorResults   []PriorResult `json:"priorResults"`
}
type Observation struct {
	State     string   `json:"state"`
	Receipt   *Receipt `json:"receipt,omitempty"`
	ErrorCode string   `json:"errorCode,omitempty"`
}
type Provider interface {
	Prepare(context.Context, PrepareRequest) (Plan, error)
	Query(context.Context, StepRequest) (Observation, error)
	Execute(context.Context, StepRequest) (Observation, error)
}
type Operation struct {
	ID               string `json:"id"`
	TenantID         string `json:"-"`
	SubjectID        string `json:"-"`
	AuthorizationRef string `json:"-"`
	RequestKey       string `json:"-"`
	RequestDigest    string `json:"-"`
	State            string `json:"state"`
	Generation       int64  `json:"generation"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ArtifactDigest   string `json:"artifactDigest"`
	Plan             Plan   `json:"-"`
}
type Work struct {
	Operation      Operation
	Step           Step
	Index          int
	IdempotencyKey string
	PayloadHash    string
	PriorResults   []PriorResult
}
type Store interface {
	Create(context.Context, Operation) (Operation, error)
	Get(context.Context, string, string, string) (Operation, error)
	Claim(context.Context, int) (*Work, error)
	Dispatch(context.Context, Work) error
	Confirm(context.Context, Work, Receipt) error
	Hold(context.Context, Work, string, string) error
	Resume(context.Context, string, string, string) error
}

package automation

import (
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

// CreateRuleRequest is the authoring payload. The definition is validated and
// normalized before it is stored, so an invalid rule can never be published.
type CreateRuleRequest struct {
	Name       string             `json:"name"`
	Definition domain.Definition  `json:"definition"`
	Enabled    bool               `json:"enabled"`
	LegacyID   string             `json:"legacy_id,omitempty"`
}

// UpdateRuleRequest carries the optimistic version the client last saw.
type UpdateRuleRequest struct {
	Name       string            `json:"name"`
	Definition domain.Definition `json:"definition"`
	Version    int               `json:"version"`
}

// SetEnabledRequest flips the switch with the same optimistic check.
type SetEnabledRequest struct {
	Enabled bool `json:"enabled"`
	Version int  `json:"version"`
}

// RuleResponse is the stable read model of a rule.
type RuleResponse struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id"`
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	Enabled        bool              `json:"enabled"`
	Revision       int               `json:"revision"`
	Version        int               `json:"version"`
	DefinitionHash string            `json:"definition_hash"`
	LegacyID       string            `json:"legacy_id,omitempty"`
	Definition     domain.Definition `json:"definition"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// ListRulesRequest filters the tenant's rules.
type ListRulesRequest struct {
	Kind   string `json:"kind" query:"kind"`
	Limit  int    `json:"limit" query:"limit"`
	Offset int    `json:"offset" query:"offset"`
}

// ListRulesResponse is a page of rules.
type ListRulesResponse struct {
	Rules []RuleResponse `json:"rules"`
	Total int            `json:"total"`
}

// ValidateRuleRequest validates a definition without storing it.
type ValidateRuleRequest struct {
	Definition domain.Definition `json:"definition"`
}

// ValidationIssue is one readable reason a definition is rejected.
type ValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidateRuleResponse reports whether the body may be published.
type ValidateRuleResponse struct {
	Valid          bool              `json:"valid"`
	Issues         []ValidationIssue `json:"issues"`
	DefinitionHash string            `json:"definition_hash"`
	StepCount      int               `json:"step_count"`
}

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// SimulateRequest replays samples against a stored rule revision.
type SimulateRequest struct {
	Samples []SimSampleInput `json:"samples"`
}

// SimSampleInput is one synthetic observation. Value is a string so the wire
// format stays simple; the evaluator coerces it the same way it coerces a
// telemetry value.
type SimSampleInput struct {
	At       string `json:"at"`
	DeviceID string `json:"device_id,omitempty"`
	Signal   string `json:"signal"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Unit     string `json:"unit,omitempty"`
	Quality  string `json:"quality,omitempty"`
}

// SimulateResponse wraps the trace plus the explicit zero side-effect count.
type SimulateResponse struct {
	RuleID         string             `json:"rule_id"`
	Revision       int                `json:"revision"`
	DefinitionHash string             `json:"definition_hash"`
	Steps          []runtime.SimStep  `json:"steps"`
	WouldRun       int                `json:"would_run"`
	SideEffects    int                `json:"side_effects"`
	Notes          []string           `json:"notes"`
}

// ---------------------------------------------------------------------------
// Ingress
// ---------------------------------------------------------------------------

// IngestRequest is one normalized uplink event.
type IngestRequest struct {
	EventID    string         `json:"event_id"`
	DeviceID   string         `json:"device_id"`
	Signal     string         `json:"signal"`
	Key        string         `json:"key"`
	Value      string         `json:"value"`
	Unit       string         `json:"unit,omitempty"`
	Quality    string         `json:"quality,omitempty"`
	ObservedAt time.Time      `json:"observed_at,omitempty"`
	// Params is the event parameter object as JSON text. Encore API schemas do
	// not allow untyped maps, so the wire format stays a string.
	Params     string `json:"params,omitempty"`
}

// IngestResponse reports the receipt. Duplicate=true means the event was
// already applied and no second action was produced.
type IngestResponse struct {
	EventID   string `json:"event_id"`
	Seq       int64  `json:"seq"`
	Duplicate bool   `json:"duplicate"`
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

// RunResponse is the explain view of one run.
type RunResponse struct {
	ID             string           `json:"id"`
	TenantID       string           `json:"tenant_id"`
	RuleID         string           `json:"rule_id"`
	Revision       int              `json:"revision"`
	DefinitionHash string           `json:"definition_hash"`
	ScopeKey       string           `json:"scope_key"`
	Generation     int64            `json:"generation"`
	DeviceID       string           `json:"device_id,omitempty"`
	Status         string           `json:"status"`
	ReasonCode     string           `json:"reason_code,omitempty"`
	Reason         string           `json:"reason,omitempty"`
	StartedAt      time.Time        `json:"started_at"`
	FinishedAt     *time.Time       `json:"finished_at,omitempty"`
	Deadline       *time.Time       `json:"deadline,omitempty"`
	Snapshot       string           `json:"snapshot,omitempty"`
	Steps          []StepResponse   `json:"steps"`
	Targets        []TargetResponse `json:"targets"`
	Outbox         []OutboxResponse `json:"outbox"`
}

// StepResponse is one persisted step.
type StepResponse struct {
	ID             string `json:"id"`
	Idx            int    `json:"idx"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key"`
	Attempt        int    `json:"attempt"`
	Detail         string `json:"detail,omitempty"`
}

// TargetResponse is one resolved or excluded device.
type TargetResponse struct {
	DeviceID   string `json:"device_id"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// OutboxResponse is one outbound intent with its exact proof level.
type OutboxResponse struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	Attempt        int    `json:"attempt"`
	IdempotencyKey string `json:"idempotency_key"`
	AcceptedRef    string `json:"accepted_ref,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

// ListRunsResponse is a page of runs.
type ListRunsResponse struct {
	Runs []RunResponse `json:"runs"`
}

// ---------------------------------------------------------------------------
// Alarms
// ---------------------------------------------------------------------------

// ListAlarmsRequest filters episodes.
type ListAlarmsRequest struct {
	PolicyID  string `json:"policy_id" query:"policy_id"`
	SubjectID string `json:"subject_id" query:"subject_id"`
	State     string `json:"state" query:"state"`
	Limit     int    `json:"limit" query:"limit"`
}

// AlarmResponse is one alarm episode.
type AlarmResponse struct {
	ID        string     `json:"id"`
	PolicyID  string     `json:"policy_id"`
	SubjectID string     `json:"subject_id"`
	Dimension string     `json:"dimension,omitempty"`
	State     string     `json:"state"`
	Revision  int        `json:"revision"`
	Epoch     int64      `json:"epoch"`
	OpenedAt  time.Time  `json:"opened_at"`
	LastSeq   int64      `json:"last_seq"`
	AckedAt   *time.Time `json:"acked_at,omitempty"`
	AckedBy   string     `json:"acked_by,omitempty"`
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
}

// ListAlarmsResponse is a page of episodes.
type ListAlarmsResponse struct {
	Alarms []AlarmResponse `json:"alarms"`
}

// AckAlarmRequest acknowledges an episode.
type AckAlarmRequest struct {
	Actor string `json:"actor,omitempty"`
}

// AckAlarmResponse reports whether the state actually changed.
type AckAlarmResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

// TransitionResponse is one lifecycle fact.
type TransitionResponse struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Seq      int64     `json:"seq"`
	Revision int       `json:"revision"`
	Epoch    int64     `json:"epoch"`
	Evidence string    `json:"evidence,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ListTransitionsResponse is the history of an episode.
type ListTransitionsResponse struct {
	Transitions []TransitionResponse `json:"transitions"`
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

// TickResponse summarises one scheduler pass.
type TickResponse struct {
	PendingExamined int `json:"pending_examined"`
	PendingFired    int `json:"pending_fired"`
	PendingDeferred int `json:"pending_deferred"`
	PendingDropped  int `json:"pending_dropped"`
	RunTimers       int `json:"run_timers"`
	OutboxDrained   int `json:"outbox_drained"`
}

// RecoverResponse reports how many receipts were replayed.
type RecoverResponse struct {
	Replayed int `json:"replayed"`
}

// BacklogResponse is the queue depth used by readiness checks.
type BacklogResponse struct {
	UnprocessedEvents int        `json:"unprocessed_events"`
	DuePending        int        `json:"due_pending"`
	PendingOutbox     int        `json:"pending_outbox"`
	ActiveRuns        int        `json:"active_runs"`
	OldestUnprocessed *time.Time `json:"oldest_unprocessed,omitempty"`
}

// EventsRequest reads the transition feed after a sequence.
type EventsRequest struct {
	After int64 `json:"after" query:"after"`
	Limit int   `json:"limit" query:"limit"`
}

// EventResponse is one business transition.
type EventResponse struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Subject   string    `json:"subject"`
	Payload   string    `json:"payload,omitempty"`
	Seq       int64     `json:"seq"`
	CreatedAt time.Time `json:"created_at"`
}

// EventsResponse is the feed page.
type EventsResponse struct {
	Events []EventResponse `json:"events"`
}

// HealthResponse proves the process booted with a usable database handle.
type HealthResponse struct {
	Status     string `json:"status"`
	Database   string `json:"database"`
	Clock      string `json:"clock"`
	Deployment string `json:"deployment"`
}

// toRuleResponse converts a stored rule.
func toRuleResponse(r *store.Rule) RuleResponse {
	return RuleResponse{
		ID: r.ID, TenantID: r.TenantID, Name: r.Name, Kind: string(r.Kind),
		Enabled: r.Enabled, Revision: r.Revision, Version: r.Version,
		DefinitionHash: r.DefinitionHash, LegacyID: r.LegacyID,
		Definition: r.Definition, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

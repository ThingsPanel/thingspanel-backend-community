package automation

import (
	"time"

	"encore.app/automation/blueprint"
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
	Name       string            `json:"name"`
	Definition domain.Definition `json:"definition"`
	Enabled    bool              `json:"enabled"`
	LegacyID   string            `json:"legacy_id,omitempty"`
}

// UpdateRuleRequest carries the optimistic version the client last saw.
type UpdateRuleRequest struct {
	Name       string            `json:"name"`
	Definition domain.Definition `json:"definition"`
	Version    int               `json:"version"`
	// PreviewToken is the token a change preview returned. When present it is
	// checked: a preview that no longer describes this rule is refused rather
	// than applied. The legacy edit path sends none, which is why it stays
	// optional — the check is an improvement, not a new gate on old pages.
	PreviewToken string `json:"previewToken,omitempty"`
	// Force saves even though existing bindings cannot carry their values into
	// the new body. Like the delete override in A20, it has to be said out loud.
	Force bool `json:"force,omitempty"`
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
//
// Every field mirrors an entry point the old UI already had: the device and
// template tabs pass their own id, the alarm tab passes has_alarm_step, and the
// search box passes name. Without them those four tabs would show the whole
// tenant instead of their own context.
type ListRulesRequest struct {
	Kind           string `json:"kind" query:"kind"`
	DeviceID       string `json:"device_id" query:"device_id"`
	DeviceConfigID string `json:"device_config_id" query:"device_config_id"`
	Name           string `json:"name" query:"name"`
	Enabled        string `json:"enabled" query:"enabled"`
	HasAlarmStep   bool   `json:"has_alarm_step" query:"has_alarm_step"`
	Limit          int    `json:"limit" query:"limit"`
	Offset         int    `json:"offset" query:"offset"`
}

// RunRuleRequest names the subject for a manual run. It is optional: a
// rule-scoped scene does not need one, and a device-scoped rule with a single
// target falls back to that target.
type RunRuleRequest struct {
	DeviceID string `json:"device_id" query:"device_id"`
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
	RuleID         string            `json:"rule_id"`
	Revision       int               `json:"revision"`
	DefinitionHash string            `json:"definition_hash"`
	Steps          []runtime.SimStep `json:"steps"`
	WouldRun       int               `json:"would_run"`
	SideEffects    int               `json:"side_effects"`
	Notes          []string          `json:"notes"`
}

// ---------------------------------------------------------------------------
// Ingress
// ---------------------------------------------------------------------------

// IngestRequest is one normalized uplink event.
type IngestRequest struct {
	EventID    string    `json:"event_id"`
	DeviceID   string    `json:"device_id"`
	Signal     string    `json:"signal"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Unit       string    `json:"unit,omitempty"`
	Quality    string    `json:"quality,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	// Params is the event parameter object as JSON text. Encore API schemas do
	// not allow untyped maps, so the wire format stays a string.
	Params string `json:"params,omitempty"`
}

// UplinkRequest is one raw uplink message forwarded by the owner modules.
//
// Values are already post-transform: the uplink pipeline applied unit
// conversion and the thing-model mapping before this call, so the adapter never
// re-interprets a raw payload and the pipeline is never asked to transform the
// same sample twice.
type UplinkRequest struct {
	// Class selects the adapter: telemetry, attribute, event, status, alarm,
	// gateway_telemetry, gateway_attribute or gateway_event.
	Class      string    `json:"class"`
	DeviceID   string    `json:"device_id"`
	GatewayID  string    `json:"gateway_id,omitempty"`
	MessageID  string    `json:"message_id,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	// Values is the decoded payload as JSON text. Encore API schemas do not
	// allow untyped maps, so the wire format stays a string.
	Values     string            `json:"values"`
	Params     string            `json:"params,omitempty"`
	Units      map[string]string `json:"units,omitempty"`
	Quality    string            `json:"quality,omitempty"`
	Source     string            `json:"source,omitempty"`
	CausalRoot string            `json:"causal_root,omitempty"`
	CausalHop  int               `json:"causal_hop,omitempty"`
}

// UplinkResponse reports what the adapter produced. Duplicates counts the
// events that were already in the inbox, which is the redelivery evidence.
type UplinkResponse struct {
	Class      string   `json:"class"`
	Accepted   int      `json:"accepted"`
	Duplicates int      `json:"duplicates"`
	EventIDs   []string `json:"event_ids"`
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
	DeviceID string `json:"device_id"`
	// Selected is the frozen selection decision; Status is the dispatch result.
	// They are separate so "was this device part of the run" stays answerable
	// after the action has run.
	Selected   bool   `json:"selected"`
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
// ListRunsRequest narrows a run listing.
//
// `status` is a comma-separated set rather than a single value because the
// question is almost always "what went wrong", and "wrong" is five statuses.
type ListRunsRequest struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Status string `json:"status"`
	// Before/After bound the run's start time, in RFC3339.
	Before string `json:"before"`
	After  string `json:"after"`
}

type ListRunsResponse struct {
	Runs   []RunResponse `json:"runs"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
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
	ID       string `json:"id"`
	PolicyID string `json:"policy_id"`
	// PolicyKey is the stable business identity. Two episodes of the same key,
	// subject and dimension can never be active at the same time.
	PolicyKey    string     `json:"policy_key"`
	SubjectID    string     `json:"subject_id"`
	Dimension    string     `json:"dimension,omitempty"`
	State        string     `json:"state"`
	Level        string     `json:"level,omitempty"`
	Source       string     `json:"source,omitempty"`
	Revision     int        `json:"revision"`
	Epoch        int64      `json:"epoch"`
	OpenedAt     time.Time  `json:"opened_at"`
	FirstSeq     int64      `json:"first_seq"`
	LastSeq      int64      `json:"last_seq"`
	SampleCount  int64      `json:"sample_count"`
	LastEvidence string     `json:"last_evidence,omitempty"`
	AckedAt      *time.Time `json:"acked_at,omitempty"`
	AckedBy      string     `json:"acked_by,omitempty"`
	ClearedAt    *time.Time `json:"cleared_at,omitempty"`
}

// AlarmEventRequest submits an authorised alarm action on the event channel.
//
// It is deliberately not a "set verdict" endpoint: only qualified and clear are
// accepted, only for policies that declare the event channel, and only from the
// callers the policy whitelists. There is no way to express "unknown" or "error"
// here because only the evaluator can know those.
type AlarmEventRequest struct {
	ActionID  string `json:"action_id"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	SubjectID string `json:"subject_id"`
	Dimension string `json:"dimension,omitempty"`
	Payload   string `json:"payload,omitempty"`
}

// AlarmEventResponse reports what the alarm owner decided.
type AlarmEventResponse struct {
	Outcome      string `json:"outcome"`
	ReasonCode   string `json:"reason_code,omitempty"`
	EpisodeID    string `json:"episode_id,omitempty"`
	TransitionID string `json:"transition_id,omitempty"`
	Replayed     bool   `json:"replayed,omitempty"`
}

// AlarmReconcileResponse is the reconciliation contract with the legacy alarm
// tables. The legacy side compares these counters with its own alarm_info and
// alarm_history rows; this service cannot read that database, so the comparison
// is an explicit hand-off rather than a silent assumption.
type AlarmReconcileResponse struct {
	ActiveEpisodes int64            `json:"active_episodes"`
	TotalEpisodes  int64            `json:"total_episodes"`
	Transitions    int64            `json:"transitions"`
	ByPolicy       map[string]int64 `json:"by_policy"`
	ByState        map[string]int64 `json:"by_state"`
	ByLevel        map[string]int64 `json:"by_level"`
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
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Seq       int64     `json:"seq"`
	Revision  int       `json:"revision"`
	Epoch     int64     `json:"epoch"`
	Evidence  string    `json:"evidence,omitempty"`
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

	ScheduleMaterialized int `json:"schedule_materialized"`
	ScheduleDuplicates   int `json:"schedule_duplicates"`
	ScheduleMisfired     int `json:"schedule_misfired"`
	ScheduleExecuted     int `json:"schedule_executed"`
	ScheduleSkipped      int `json:"schedule_skipped"`
	ScheduleFailed       int `json:"schedule_failed"`
	ScheduleRecovered    int `json:"schedule_recovered"`
	NoDataNoted          int `json:"no_data_noted"`

	RunsExpired int `json:"runs_expired"`
	RunsResumed int `json:"runs_resumed"`
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

// ---------------------------------------------------------------------------
// Schedules (A07)
// ---------------------------------------------------------------------------

// ScheduleRequest creates or replaces a timed trigger.
//
// Kind is "once" (needs RunAt) or "cron" (needs Cron). Timezone is an IANA name;
// leaving it empty uses the deployment's local zone, which is what the legacy
// scene timer did.
type ScheduleRequest struct {
	RuleID        string     `json:"rule_id"`
	Kind          string     `json:"kind"`
	Cron          string     `json:"cron,omitempty"`
	Timezone      string     `json:"timezone,omitempty"`
	RunAt         *time.Time `json:"run_at,omitempty"`
	MisfirePolicy string     `json:"misfire_policy,omitempty"`
	Enabled       *bool      `json:"enabled,omitempty"`
}

// ScheduleResponse is one stored schedule.
type ScheduleResponse struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	RuleID        string     `json:"rule_id"`
	Kind          string     `json:"kind"`
	Cron          string     `json:"cron,omitempty"`
	Timezone      string     `json:"timezone"`
	RunAt         *time.Time `json:"run_at,omitempty"`
	MisfirePolicy string     `json:"misfire_policy"`
	Enabled       bool       `json:"enabled"`
	NextFireAt    *time.Time `json:"next_fire_at,omitempty"`
	LastFireAt    *time.Time `json:"last_fire_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ListSchedulesResponse is the schedule list.
type ListSchedulesResponse struct {
	Schedules []ScheduleResponse `json:"schedules"`
}

// ListSchedulesRequest filters the schedule list.
type ListSchedulesRequest struct {
	RuleID string `json:"rule_id" query:"rule_id"`
	Limit  int    `json:"limit" query:"limit"`
}

// ListFiresRequest pages the occurrence history of one schedule.
type ListFiresRequest struct {
	Limit int `json:"limit" query:"limit"`
}

// PreviewScheduleRequest asks what an expression means without storing it.
type PreviewScheduleRequest struct {
	Kind          string     `json:"kind"`
	Cron          string     `json:"cron,omitempty"`
	Timezone      string     `json:"timezone,omitempty"`
	RunAt         *time.Time `json:"run_at,omitempty"`
	MisfirePolicy string     `json:"misfire_policy,omitempty"`
	From          *time.Time `json:"from,omitempty"`
	Count         int        `json:"count,omitempty"`
}

// PreviewScheduleResponse lists the next occurrences in the schedule's zone.
type PreviewScheduleResponse struct {
	Timezone string   `json:"timezone"`
	Next     []string `json:"next"`
}

// FireResponse is one attempted occurrence.
type FireResponse struct {
	ID           string    `json:"id"`
	ScheduleID   string    `json:"schedule_id"`
	RuleID       string    `json:"rule_id"`
	ScheduledFor time.Time `json:"scheduled_for"`
	State        string    `json:"state"`
	RunID        string    `json:"run_id,omitempty"`
	Attempt      int       `json:"attempt"`
	Missed       int       `json:"missed,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListFiresResponse is the occurrence history of a schedule.
type ListFiresResponse struct {
	Fires []FireResponse `json:"fires"`
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

// ExplainRequest asks why a rule did or did not run for one device in a window.
type ExplainRequest struct {
	DeviceID string `query:"device_id"`
	From     string `query:"from"`
	To       string `query:"to"`
}

// ExplainResponse is the answer, including what it is based on.
type ExplainResponse struct {
	RuleID   string    `json:"rule_id"`
	DeviceID string    `json:"device_id"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	// Verdict is fired | waiting_for_duration | condition_not_met |
	// undetermined | no_data | not_fired.
	Verdict string `json:"verdict"`
	// Reasons are the evaluator's own reasons, not a second opinion.
	Reasons []domain.Reason  `json:"reasons"`
	Runs    []RunResponse    `json:"runs"`
	Pending *PendingResponse `json:"pending,omitempty"`
	Input   *InputResponse   `json:"input,omitempty"`
	// Evidence says how much history the answer rests on. When it is not
	// complete the caller must show that instead of a confident reason.
	Evidence EvidenceResponse `json:"evidence"`
}

type PendingResponse struct {
	ScopeKey        string    `json:"scope_key"`
	StartedAt       time.Time `json:"started_at"`
	Deadline        time.Time `json:"deadline"`
	LastEvidenceAt  time.Time `json:"last_evidence_at"`
	RequireBefore   time.Time `json:"require_processed_before"`
	DeferCount      int       `json:"defer_count"`
	RemainingMillis int64     `json:"remaining_ms"`
}

type InputResponse struct {
	DeviceID string `json:"device_id"`
	Signal   string `json:"signal"`
	Key      string `json:"key"`
	// Value is the sample encoded as JSON text, the same shape the command
	// payload uses, so no interface type leaks into the API schema.
	Value      string    `json:"value"`
	Quality    string    `json:"quality"`
	ObservedAt time.Time `json:"observed_at"`
	AgeMillis  int64     `json:"age_ms"`
}

type EvidenceResponse struct {
	RunsInWindow  int    `json:"runs_in_window"`
	StateObserved bool   `json:"state_observed"`
	Complete      bool   `json:"complete"`
	Note          string `json:"note"`
}

// ---------------------------------------------------------------------------
// A13 legacy scene migration
// ---------------------------------------------------------------------------

// LegacyActionInput is one scene_action_info row as the migration tool read it.
type LegacyActionInput struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Type      string `json:"type"`
	ParamType string `json:"param_type,omitempty"`
	Param     string `json:"param,omitempty"`
	Value     string `json:"value,omitempty"`
	// ContinueOnFailure is the legacy "keep going after an error" switch.
	ContinueOnFailure bool `json:"continue_on_failure,omitempty"`
}

// LegacySceneInput is one legacy scene with its ordered actions.
type LegacySceneInput struct {
	ID      string              `json:"id"`
	Name    string              `json:"name,omitempty"`
	Actions []LegacyActionInput `json:"actions"`
}

// LegacyScenePlanRequest is a dry run of the migration for one scene.
//
// The graph and the known device ids are supplied by the caller because the
// legacy rows live in another database: this service cannot read them, and
// pretending otherwise would turn "not found" into "assume it exists".
type LegacyScenePlanRequest struct {
	Scene         LegacySceneInput   `json:"scene"`
	Graph         []LegacySceneInput `json:"graph,omitempty"`
	Devices       []string           `json:"devices,omitempty"`
	DeviceConfigs []string           `json:"device_configs,omitempty"`
}

// LegacyStepMappingResponse is what one legacy action became.
type LegacyStepMappingResponse struct {
	Index             int    `json:"index"`
	LegacyActionID    string `json:"legacy_action_id"`
	LegacyType        string `json:"legacy_type"`
	StepKind          string `json:"step_kind"`
	Target            string `json:"target"`
	ContinueOnFailure bool   `json:"continue_on_failure"`
	Note              string `json:"note,omitempty"`
}

// LegacyIssueResponse is one reason a scene cannot be migrated as it stands.
type LegacyIssueResponse struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// LegacyScenePlanResponse is the plan plus the report that explains it.
type LegacyScenePlanResponse struct {
	SceneID    string `json:"scene_id"`
	Name       string `json:"name,omitempty"`
	Shape      string `json:"shape"`
	Migratable bool   `json:"migratable"`
	// Definition is the rule body the migration would publish.
	Definition domain.Definition `json:"definition"`
	// Mapping preserves the original action order so before and after can be
	// compared one row at a time.
	Mapping        []LegacyStepMappingResponse `json:"mapping"`
	ContinuedSteps []string                    `json:"continued_steps,omitempty"`
	Issues         []LegacyIssueResponse       `json:"issues,omitempty"`
	// CyclePath is set when the activation graph loops.
	CyclePath []string `json:"cycle_path,omitempty"`
}

// ---------------------------------------------------------------------------
// A18: rule templates
// ---------------------------------------------------------------------------

// BlueprintResponse is one template version as a form renders it.
//
// The parameter list is the whole contract: Web, CLI and MCP all read it and all
// validate against it, which is what makes "the same values are accepted
// everywhere" true instead of aspirational.
type BlueprintResponse struct {
	Key      string `json:"key"`
	Version  int    `json:"version"`
	Revision string `json:"revision"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	// Guidance is the one sentence shown above the form.
	Guidance string               `json:"guidance"`
	Params   []blueprint.Param    `json:"params"`
	Requires []blueprint.Capability `json:"requires,omitempty"`
	// Versions lists every published version so an upgrade can name one.
	Versions []int `json:"versions"`
}

// ListBlueprintsResponse lists the built-in templates.
type ListBlueprintsResponse struct {
	Blueprints []BlueprintResponse `json:"blueprints"`
}

// BlueprintPreviewRequest asks what a set of values would produce.
type BlueprintPreviewRequest struct {
	Key     string            `json:"key"`
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

// BlueprintPreviewResponse is the definition plus everything we could and could
// not check about it. It has no side effects: nothing is created, no device is
// touched.
type BlueprintPreviewResponse struct {
	Valid        bool                   `json:"valid"`
	BlueprintKey string                 `json:"blueprint_key"`
	Version      int                    `json:"version"`
	Revision     string                 `json:"revision"`
	Values       map[string]string      `json:"values,omitempty"`
	Overrides    map[string]string      `json:"overrides,omitempty"`
	Capabilities []blueprint.Capability `json:"capabilities,omitempty"`
	Definition   *domain.Definition     `json:"definition,omitempty"`
	// Issues carries both errors (which make Valid false) and warnings such as
	// "the device directory does not expose capabilities, so we could not verify
	// this metric exists".
	Issues []blueprint.Issue `json:"issues,omitempty"`
}

// InstantiateBlueprintRequest creates a real rule from a template.
type InstantiateBlueprintRequest struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	// Disabled is opt-in rather than Enabled opt-out: a customer who just picked
	// a template and filled the form expects it to work, and "created but
	// silently paused" is the worst of both worlds. A caller that wants a draft
	// says so.
	Disabled bool              `json:"disabled"`
	Values   map[string]string `json:"values"`
}

// BlueprintOriginResponse says where a rule came from and what an upgrade would
// do to it.
type BlueprintOriginResponse struct {
	RuleID            string            `json:"rule_id"`
	BlueprintKey      string            `json:"blueprint_key"`
	BlueprintVersion  int               `json:"blueprint_version"`
	BlueprintRevision string            `json:"blueprint_revision"`
	Params            map[string]string `json:"params,omitempty"`
	Overrides         map[string]string `json:"overrides,omitempty"`
	Capabilities      []string          `json:"capabilities,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpgradedAt        *time.Time        `json:"upgraded_at,omitempty"`
	// LatestVersion is the newest published version of this template.
	LatestVersion int `json:"latest_version"`
	// UpdateAvailable is true when a newer version exists. It is informational:
	// nothing moves until the customer asks.
	UpdateAvailable bool `json:"update_available"`
	// Diff and Issues are filled in only when an update is available, so the
	// caller can see what the move would cost before asking for it.
	Diff    *blueprint.Diff    `json:"diff,omitempty"`
	Blocked bool               `json:"blocked,omitempty"`
	Issues  []blueprint.Issue  `json:"issues,omitempty"`
}

// GetRuleBlueprintResponse is the origin plus the rule it belongs to.
type GetRuleBlueprintResponse struct {
	RuleID string                   `json:"rule_id"`
	Origin *BlueprintOriginResponse `json:"origin,omitempty"`
	// Note explains why Origin is absent instead of leaving the caller to guess.
	Note string `json:"note,omitempty"`
}

// UpgradeBlueprintRequest moves an instance to another template version.
type UpgradeBlueprintRequest struct {
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
	// DryRun reports what would happen without publishing anything.
	DryRun bool `json:"dry_run"`
}

// UpgradeBlueprintResponse is the plan, and the rule when the plan was applied.
type UpgradeBlueprintResponse struct {
	RuleID  string              `json:"rule_id"`
	Blocked bool                `json:"blocked"`
	Applied bool                `json:"applied"`
	Diff    *blueprint.Diff     `json:"diff,omitempty"`
	Issues  []blueprint.Issue   `json:"issues,omitempty"`
	Rule    *RuleResponse       `json:"rule,omitempty"`
	Origin  *BlueprintOriginResponse `json:"origin,omitempty"`
}

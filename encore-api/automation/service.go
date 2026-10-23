// Package automation is the single Encore host for the automation, scene and
// alarm owners. It owns the rules, the runtime state, the alarm episodes and the
// outbound intents. Device commands, notifications and the device directory stay
// in their existing modules and are reached only through narrow ports.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/automation/domain"
	"encore.app/automation/ingress"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"encore.app/l1-access/authn"
)

// db is the automation owner's own database. Every migration lives next to this
// service, so the owner can be deployed and rolled back independently.
var db = sqldb.NewDatabase("automation", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

// Service is the Encore service struct.
//
//encore:service
type Service struct {
	rt *runtime.Runtime
}

var (
	svcOnce sync.Once
	svc     *Service
	svcErr  error
)

func initService() (*Service, error) { return getService() }

// getService builds the runtime once. Ports that are not configured stay nil,
// which makes the corresponding side effect "unknown" instead of a fake success.
func getService() (*Service, error) {
	svcOnce.Do(func() {
		cfg := ports.FromEnv()
		st := store.New(db.Stdlib())
		svc = &Service{rt: runtime.New(
			st,
			ports.NewHTTPCommandOwner(cfg),
			ports.NewHTTPNotificationOwner(cfg),
			ports.NewHTTPDirectory(cfg),
			nil,
		)}
	})
	return svc, svcErr
}

// rt returns the runtime or a clear internal error.
func (s *Service) rtOrErr() (*runtime.Runtime, error) {
	if s == nil || s.rt == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "automation runtime not initialised"}
	}
	return s.rt, nil
}

// mustTenant returns the caller's tenant. Every tenant-scoped endpoint goes
// through it, so a missing identity can never fall back to a default tenant.
func mustTenant() (string, error) {
	d := authn.CurrentData()
	if d == nil || strings.TrimSpace(d.TenantID) == "" {
		return "", &errs.Error{Code: errs.Unauthenticated, Message: "missing tenant identity"}
	}
	return d.TenantID, nil
}

func internal(err error) error {
	if err == nil {
		return nil
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return err
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &errs.Error{Code: errs.NotFound, Message: "not found"}
	case errors.Is(err, store.ErrConflict):
		return &errs.Error{Code: errs.Aborted, Message: "版本冲突：规则已被其他人修改，请刷新后重试"}
	default:
		return &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// Health reports boot status. It is public so a probe never needs a tenant.
//
//encore:api public method=GET path=/v1/health
func (s *Service) Health(ctx context.Context) (*HealthResponse, error) {
	out := &HealthResponse{Status: "ok", Clock: time.Now().UTC().Format(time.RFC3339)}
	if err := db.Stdlib().PingContext(ctx); err != nil {
		out.Status = "degraded"
		out.Database = err.Error()
		return out, nil
	}
	out.Database = "ok"
	out.Deployment = ports.FromEnv().DeploymentID
	return out, nil
}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

// CreateRule validates, normalizes and stores a new rule at revision 1.
//
//encore:api auth method=POST path=/v1/rules
func (s *Service) CreateRule(ctx context.Context, req *CreateRuleRequest) (*RuleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "规则名称不能为空"}
	}
	def := req.Definition
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		return nil, invalidDefinition(err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: tenantID, Name: req.Name, Kind: def.Kind,
		Enabled: req.Enabled, Definition: def, DefinitionHash: domain.DefinitionHash(def),
		LegacyID: req.LegacyID,
	}
	if err := rt.Store().CreateRule(ctx, rule); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetRule(ctx, tenantID, rule.ID)
	if err != nil {
		return nil, internal(err)
	}
	out := toRuleResponse(stored)
	return &out, nil
}

// UpdateRule publishes a new revision when the client's version still matches.
//
//encore:api auth method=PUT path=/v1/rules/:id
func (s *Service) UpdateRule(ctx context.Context, id string, req *UpdateRuleRequest) (*RuleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	existing, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	def := req.Definition
	def.Kind = existing.Kind // the kind is fixed for the lifetime of a rule
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		return nil, invalidDefinition(err)
	}
	name := req.Name
	if strings.TrimSpace(name) == "" {
		name = existing.Name
	}
	rule := &store.Rule{
		ID: id, TenantID: tenantID, Name: name, Kind: def.Kind,
		Definition: def, DefinitionHash: domain.DefinitionHash(def),
	}
	if err := rt.Store().UpdateRule(ctx, rule, req.Version); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := toRuleResponse(stored)
	return &out, nil
}

// GetRule reads one rule.
//
//encore:api auth method=GET path=/v1/rules/:id
func (s *Service) GetRule(ctx context.Context, id string) (*RuleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	rule, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := toRuleResponse(rule)
	return &out, nil
}

// ListRules lists the tenant's rules.
//
//encore:api auth method=GET path=/v1/rules
func (s *Service) ListRules(ctx context.Context, req *ListRulesRequest) (*ListRulesResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	rules, err := rt.Store().ListRules(ctx, tenantID, req.Kind, req.Limit, req.Offset)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListRulesResponse{Rules: make([]RuleResponse, 0, len(rules))}
	for _, r := range rules {
		out.Rules = append(out.Rules, toRuleResponse(r))
	}
	out.Total = len(out.Rules)
	return out, nil
}

// DeleteRule soft deletes and disables a rule. In-flight runs keep their pinned
// revision, so a deletion never changes what an already accepted run does.
//
//encore:api auth method=DELETE path=/v1/rules/:id
func (s *Service) DeleteRule(ctx context.Context, id string) error {
	tenantID, err := mustTenant()
	if err != nil {
		return err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return err
	}
	if err := rt.Store().DeleteRule(ctx, tenantID, id); err != nil {
		return internal(err)
	}
	// A deleted rule must not leave a schedule that still fires. The due time is
	// cleared here as well, so the scan cannot pick it up even before the
	// soft-deleted rule is filtered out of the join.
	_ = rt.Store().CancelSchedulesForRule(ctx, id)
	return nil
}

// SetRuleEnabled is the switch used by the legacy on/off page control.
//
//encore:api auth method=POST path=/v1/rules/:id/enabled
func (s *Service) SetRuleEnabled(ctx context.Context, id string, req *SetEnabledRequest) (*RuleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().SetEnabled(ctx, tenantID, id, req.Enabled, req.Version); err != nil {
		return nil, internal(err)
	}
	// Schedules are deliberately left alone here. The due scan joins the rule and
	// requires it to be enabled, so a disabled rule fires nothing; and because the
	// stored due time is kept, re-enabling resumes under the schedule's misfire
	// policy instead of silently losing the job.
	stored, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := toRuleResponse(stored)
	return &out, nil
}

// ValidateRule checks a definition without storing it. It returns every issue,
// not just the first, so the editor can show them all.
//
//encore:api auth method=POST path=/v1/rule-validation
func (s *Service) ValidateRule(ctx context.Context, req *ValidateRuleRequest) (*ValidateRuleResponse, error) {
	def := req.Definition
	def.Normalize()
	out := &ValidateRuleResponse{DefinitionHash: domain.DefinitionHash(def)}
	if err := domain.Validate(&def); err != nil {
		var ve *domain.ValidationError
		if errors.As(err, &ve) {
			for _, i := range ve.Issues {
				out.Issues = append(out.Issues, ValidationIssue{Field: i.Field, Code: i.Code, Message: i.Message})
			}
		} else {
			out.Issues = append(out.Issues, ValidationIssue{Code: "invalid", Message: err.Error()})
		}
		return out, nil
	}
	out.Valid = true
	out.StepCount = domain.CountSteps(def.Steps)
	return out, nil
}

func invalidDefinition(err error) error {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		msgs := make([]string, 0, len(ve.Issues))
		for _, i := range ve.Issues {
			msgs = append(msgs, i.Field+": "+i.Message)
		}
		return &errs.Error{Code: errs.InvalidArgument, Message: strings.Join(msgs, "; ")}
	}
	return &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
}

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// SimulateRule replays synthetic samples through the same evaluator the runtime
// uses, with a virtual clock. It performs no device command, no alarm transition
// and no notification; SideEffects is always zero.
//
//encore:api auth method=POST path=/v1/rules/:id/simulate
func (s *Service) SimulateRule(ctx context.Context, id string, req *SimulateRequest) (*SimulateResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	samples := make([]runtime.SimSample, 0, len(req.Samples))
	for _, in := range req.Samples {
		samples = append(samples, runtime.SimSample{
			At: in.At, DeviceID: in.DeviceID, Signal: in.Signal, Key: in.Key,
			Value: in.Value, Unit: in.Unit, Quality: in.Quality,
		})
	}
	res, err := rt.Simulate(ctx, tenantID, id, samples)
	if err != nil {
		return nil, internal(err)
	}
	out := &SimulateResponse{
		RuleID: res.RuleID, Revision: res.Revision, DefinitionHash: res.DefinitionHash,
		Steps: res.Steps, WouldRun: res.WouldRun, SideEffects: res.SideEffects, Notes: res.Notes,
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Ingress
// ---------------------------------------------------------------------------

// IngestEvent accepts one normalized uplink event. The event_id is the dedupe
// identity: a replayed broker message returns duplicate=true and produces no
// second physical action.
//
//encore:api auth method=POST path=/v1/ingress/events
func (s *Service) IngestEvent(ctx context.Context, req *IngestRequest) (*IngestResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.EventID) == "" || strings.TrimSpace(req.DeviceID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "event_id 与 device_id 均为必填"}
	}
	signal := domain.SignalKind(req.Signal)
	if signal == "" {
		signal = domain.SignalTelemetry
	}
	quality := domain.Quality(req.Quality)
	if quality == "" {
		quality = domain.QualityGood
	}
	var params map[string]any
	if strings.TrimSpace(req.Params) != "" {
		if err := json.Unmarshal([]byte(req.Params), &params); err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "params 不是合法 JSON 对象"}
		}
	}
	rec, err := rt.Ingest(ctx, store.IngestEvent{
		EventID: req.EventID, TenantID: tenantID, DeviceID: req.DeviceID,
		Signal: signal, Key: req.Key, Value: req.Value, Unit: req.Unit,
		Quality: quality, ObservedAt: req.ObservedAt, Params: params, Source: "api",
	})
	if err != nil {
		return nil, internal(err)
	}
	return &IngestResponse{EventID: rec.EventID, Seq: rec.Seq, Duplicate: rec.Duplicate}, nil
}

// IngestUplink is the explicit input adapter for the owner modules.
//
// Each input class has its own normalization rules: telemetry and attributes
// produce one durable sample per key, events are instantaneous, online/offline
// is a state transition, and an alarm transition carries the authoritative
// transition id. The tenant always comes from the session, never from the body.
//
//encore:api auth method=POST path=/v1/ingress/uplink
func (s *Service) IngestUplink(ctx context.Context, req *UplinkRequest) (*UplinkResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	class := ingress.Class(req.Class)
	if class == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "class 为必填"}
	}
	values := map[string]any{}
	if strings.TrimSpace(req.Values) != "" {
		if err := json.Unmarshal([]byte(req.Values), &values); err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "values 不是合法 JSON 对象"}
		}
	}
	var params map[string]any
	if strings.TrimSpace(req.Params) != "" {
		if err := json.Unmarshal([]byte(req.Params), &params); err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: "params 不是合法 JSON 对象"}
		}
	}
	events, err := ingress.Normalize(class, ingress.Envelope{
		TenantID: tenantID, DeviceID: req.DeviceID, GatewayID: req.GatewayID,
		MessageID: req.MessageID, ObservedAt: req.ObservedAt, Values: values,
		Params: params, Units: req.Units, Quality: req.Quality, Source: req.Source,
		CausalRoot: req.CausalRoot, CausalHop: req.CausalHop,
	}, time.Now().UTC())
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}

	out := &UplinkResponse{Class: string(class), EventIDs: make([]string, 0, len(events))}
	for i := range events {
		rec, err := rt.Ingest(ctx, events[i])
		if err != nil {
			return nil, internal(err)
		}
		out.EventIDs = append(out.EventIDs, rec.EventID)
		if rec.Duplicate {
			out.Duplicates++
			continue
		}
		out.Accepted++
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

// ListRuleRuns lists recent runs of a rule.
//
//encore:api auth method=GET path=/v1/rules/:id/runs
func (s *Service) ListRuleRuns(ctx context.Context, id string) (*ListRunsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	runs, err := rt.Store().ListRuns(ctx, tenantID, id, "", 50)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListRunsResponse{Runs: make([]RunResponse, 0, len(runs))}
	for _, r := range runs {
		out.Runs = append(out.Runs, toRunResponse(r, nil, nil, nil))
	}
	return out, nil
}

// GetRun is the explain view: run header, every step, the frozen target snapshot
// and every outbound intent with its exact proof level.
//
//encore:api auth method=GET path=/v1/runs/:runId
func (s *Service) GetRun(ctx context.Context, runId string) (*RunResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	run, err := rt.Store().GetRun(ctx, tenantID, runId)
	if err != nil {
		return nil, internal(err)
	}
	steps, _ := rt.Store().ListSteps(ctx, run.ID)
	targets, _ := rt.Store().ListTargets(ctx, run.ID)
	outbox, _ := rt.Store().OutboxForRun(ctx, run.ID)
	out := toRunResponse(run, steps, targets, outbox)
	return &out, nil
}

func toRunResponse(r *store.Run, steps []*store.Step, targets []store.Target, outbox []*store.OutboxItem) RunResponse {
	out := RunResponse{
		ID: r.ID, TenantID: r.TenantID, RuleID: r.RuleID, Revision: r.Revision,
		DefinitionHash: r.DefinitionHash, ScopeKey: r.ScopeKey, Generation: r.Generation,
		DeviceID: r.DeviceID, Status: r.Status, ReasonCode: r.ReasonCode, Reason: r.Reason,
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Deadline: r.Deadline,
		Snapshot: string(r.Snapshot),
	}
	for _, st := range steps {
		out.Steps = append(out.Steps, StepResponse{
			ID: st.ID, Idx: st.Idx, Kind: st.Kind, Status: st.Status,
			IdempotencyKey: st.IdempotencyKey, Attempt: st.Attempt, Detail: string(st.Detail),
		})
	}
	for _, t := range targets {
		out.Targets = append(out.Targets, TargetResponse{
			DeviceID: t.DeviceID, Selected: t.Selected, Status: t.Status,
			ReasonCode: t.ReasonCode, Reason: t.Reason,
		})
	}
	for _, o := range outbox {
		out.Outbox = append(out.Outbox, OutboxResponse{
			ID: o.ID, Kind: o.Kind, State: o.State, Attempt: o.Attempt,
			IdempotencyKey: o.IdempotencyKey, AcceptedRef: o.AcceptedRef, LastError: o.LastError,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Alarms
// ---------------------------------------------------------------------------

// ListAlarms lists alarm episodes.
//
//encore:api auth method=GET path=/v1/alarms
func (s *Service) ListAlarms(ctx context.Context, req *ListAlarmsRequest) (*ListAlarmsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	eps, err := rt.Store().ListEpisodes(ctx, tenantID, req.PolicyID, req.SubjectID, req.State, req.Limit)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListAlarmsResponse{Alarms: make([]AlarmResponse, 0, len(eps))}
	for _, e := range eps {
		out.Alarms = append(out.Alarms, AlarmResponse{
			ID: e.ID, PolicyID: e.PolicyID, SubjectID: e.SubjectID, Dimension: e.Dimension,
			State: e.State, Revision: e.Revision, Epoch: e.Epoch, OpenedAt: e.OpenedAt,
			LastSeq: e.LastSeq, AckedAt: e.AckedAt, AckedBy: e.AckedBy, ClearedAt: e.ClearedAt,
		})
	}
	return out, nil
}

// AckAlarm acknowledges an episode. It records "someone knows", never "the
// device is healthy": the episode state stays active.
//
//encore:api auth method=POST path=/v1/alarms/:id/ack
func (s *Service) AckAlarm(ctx context.Context, id string, req *AckAlarmRequest) (*AckAlarmResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	actor := req.Actor
	if actor == "" {
		if d := authn.CurrentData(); d != nil {
			actor = d.UserID
		}
	}
	ok, err := rt.Ack(ctx, tenantID, id, actor)
	if err != nil {
		return nil, internal(err)
	}
	return &AckAlarmResponse{Acknowledged: ok}, nil
}

// ListAlarmTransitions returns the lifecycle history of an episode.
//
//encore:api auth method=GET path=/v1/alarms/:id/transitions
func (s *Service) ListAlarmTransitions(ctx context.Context, id string) (*ListTransitionsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().GetActiveEpisodeByID(ctx, tenantID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, internal(store.ErrNotFound)
		}
		return nil, internal(err)
	}
	trs, err := rt.Store().ListTransitions(ctx, id)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListTransitionsResponse{Transitions: make([]TransitionResponse, 0, len(trs))}
	for _, t := range trs {
		evidence, _ := json.Marshal(t.Evidence)
		out.Transitions = append(out.Transitions, TransitionResponse{
			ID: t.ID, Kind: t.Kind, Seq: t.Seq, Revision: t.Revision, Epoch: t.Epoch,
			Evidence: string(evidence), CreatedAt: t.CreatedAt,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

// Tick runs one scheduler pass: due condition timers, due run continuations and
// pending outbound intents. It is idempotent and safe to run from several
// workers because every claim uses SKIP LOCKED.
//
//encore:api auth method=POST path=/v1/tick
func (s *Service) Tick(ctx context.Context) (*TickResponse, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	res, err := rt.Tick(ctx)
	if err != nil {
		return nil, internal(err)
	}
	return &TickResponse{
		PendingExamined: res.PendingExamined, PendingFired: res.PendingFired,
		PendingDeferred: res.PendingDeferred, PendingDropped: res.PendingDropped,
		RunTimers: res.RunTimers, OutboxDrained: res.OutboxDrained,
		ScheduleMaterialized: res.ScheduleMaterialized, ScheduleDuplicates: res.ScheduleDuplicates,
		ScheduleMisfired: res.ScheduleMisfired, ScheduleExecuted: res.ScheduleExecuted,
		ScheduleSkipped: res.ScheduleSkipped, ScheduleFailed: res.ScheduleFailed,
		ScheduleRecovered: res.ScheduleRecovered, NoDataNoted: res.NoDataNoted,
		RunsExpired: res.RunsExpired, RunsResumed: res.RunsResumed,
	}, nil
}

// Recover replays receipts that were received but not evaluated before a crash.
//
//encore:api auth method=POST path=/v1/recover
func (s *Service) Recover(ctx context.Context) (*RecoverResponse, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	n, err := rt.RecoverAfterRestart(ctx, 500)
	if err != nil {
		return nil, internal(err)
	}
	return &RecoverResponse{Replayed: n}, nil
}

// Backlog reports queue depth for readiness and performance checks.
//
//encore:api auth method=GET path=/v1/backlog
func (s *Service) Backlog(ctx context.Context) (*BacklogResponse, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	st, err := rt.Store().Backlog(ctx)
	if err != nil {
		return nil, internal(err)
	}
	return &BacklogResponse{
		UnprocessedEvents: st.UnprocessedEvents, DuePending: st.DuePending,
		PendingOutbox: st.PendingOutbox, ActiveRuns: st.ActiveRuns,
		OldestUnprocessed: st.OldestUnprocessed,
	}, nil
}

// Events reads the business transition feed.
//
//encore:api auth method=GET path=/v1/events
func (s *Service) Events(ctx context.Context, req *EventsRequest) (*EventsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	events, err := rt.Store().EventsSince(ctx, tenantID, req.After, limit)
	if err != nil {
		return nil, internal(err)
	}
	out := &EventsResponse{Events: make([]EventResponse, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, EventResponse{
			ID: e.ID, Kind: e.Kind, Subject: e.Subject, Payload: string(e.Payload),
			Seq: e.Seq, CreatedAt: e.CreatedAt,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Schedules (A07)
// ---------------------------------------------------------------------------
//
// A schedule is business data, not a source-code cron entry: it is created,
// changed and deleted through these endpoints, so adding one never needs a
// deployment and deleting one cannot leave a ghost job behind.
//
// The occurrence, not the schedule, is the unit of work. Each occurrence gets a
// row unique on (schedule_id, scheduled_for), which is what makes a duplicate
// wake-up, a retried claim and a restart all harmless.

// defaultTimezone keeps the legacy scene timer's behaviour: an expression
// without an explicit zone is evaluated in the deployment's own zone, not UTC.
// TP_SCHEDULE_TZ overrides it for a container that runs with TZ=UTC.
func defaultTimezone() string {
	if tz := strings.TrimSpace(os.Getenv("TP_SCHEDULE_TZ")); tz != "" {
		return tz
	}
	if name := time.Local.String(); name != "" && name != "Local" {
		return name
	}
	return "UTC"
}

func specFromRequest(kind, cron, tz string, runAt *time.Time, misfire string) domain.ScheduleSpec {
	if strings.TrimSpace(tz) == "" {
		tz = defaultTimezone()
	}
	if strings.TrimSpace(misfire) == "" {
		misfire = string(domain.MisfireSkip)
	}
	spec := domain.ScheduleSpec{
		Kind:     domain.ScheduleKind(strings.TrimSpace(kind)),
		Cron:     strings.TrimSpace(cron),
		Timezone: tz,
		Misfire:  domain.MisfirePolicy(strings.TrimSpace(misfire)),
	}
	if runAt != nil {
		spec.RunAt = *runAt
	}
	return spec
}

func scheduleResponse(sc *store.Schedule) ScheduleResponse {
	return ScheduleResponse{
		ID: sc.ID, TenantID: sc.TenantID, RuleID: sc.RuleID, Kind: sc.Kind,
		Cron: sc.Cron, Timezone: sc.Timezone, RunAt: sc.RunAt,
		MisfirePolicy: sc.MisfirePolicy, Enabled: sc.Enabled,
		NextFireAt: sc.NextFireAt, LastFireAt: sc.LastFireAt, CreatedAt: sc.CreatedAt,
	}
}

func fireResponse(f *store.Fire) FireResponse {
	return FireResponse{
		ID: f.ID, ScheduleID: f.ScheduleID, RuleID: f.RuleID, ScheduledFor: f.ScheduledFor,
		State: f.State, RunID: f.RunID, Attempt: f.Attempt, Missed: f.Missed,
		Error: f.Error, CreatedAt: f.CreatedAt,
	}
}

// CreateSchedule attaches a timed trigger to an existing rule.
//
//encore:api auth method=POST path=/v1/schedules
func (s *Service) CreateSchedule(ctx context.Context, req *ScheduleRequest) (*ScheduleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.RuleID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "rule_id 不能为空"}
	}
	// The rule must exist in this tenant: a schedule without an owner would
	// fire into nothing and be impossible to find from the UI.
	if _, err := rt.Store().GetRule(ctx, tenantID, req.RuleID); err != nil {
		return nil, internal(err)
	}
	spec := specFromRequest(req.Kind, req.Cron, req.Timezone, req.RunAt, req.MisfirePolicy)
	sc, err := runtime.ScheduleFromSpec(tenantID, req.RuleID, runtime.NewID(), spec, rt.Now())
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	// A one-off whose instant has already passed has no next occurrence, so it
	// would be stored enabled but never fire. Refusing it here is better than
	// leaving a schedule that silently does nothing.
	if spec.Kind == domain.ScheduleOnce && sc.NextFireAt == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument,
			Message: "单次定时的执行时刻必须晚于当前时间；如需立即执行请直接触发规则"}
	}
	if req.Enabled != nil {
		sc.Enabled = *req.Enabled
	}
	if err := rt.Store().CreateSchedule(ctx, sc); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetSchedule(ctx, tenantID, sc.ID)
	if err != nil {
		return nil, internal(err)
	}
	out := scheduleResponse(stored)
	return &out, nil
}

// UpdateSchedule rewrites a schedule and recomputes its next occurrence from
// now, so an edited expression never inherits a stale due time.
//
//encore:api auth method=PUT path=/v1/schedules/:id
func (s *Service) UpdateSchedule(ctx context.Context, id string, req *ScheduleRequest) (*ScheduleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	current, err := rt.Store().GetSchedule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	spec := specFromRequest(req.Kind, req.Cron, req.Timezone, req.RunAt, req.MisfirePolicy)
	next, err := runtime.ScheduleFromSpec(tenantID, current.RuleID, id, spec, rt.Now())
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	if spec.Kind == domain.ScheduleOnce && next.NextFireAt == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument,
			Message: "单次定时的执行时刻必须晚于当前时间；如需立即执行请直接触发规则"}
	}
	next.Enabled = current.Enabled
	if req.Enabled != nil {
		next.Enabled = *req.Enabled
	}
	if err := rt.Store().UpdateSchedule(ctx, next); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetSchedule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := scheduleResponse(stored)
	return &out, nil
}

// GetSchedule reads one schedule.
//
//encore:api auth method=GET path=/v1/schedules/:id
func (s *Service) GetSchedule(ctx context.Context, id string) (*ScheduleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	sc, err := rt.Store().GetSchedule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := scheduleResponse(sc)
	return &out, nil
}

// ListSchedules lists schedules, optionally for one rule.
//
//encore:api auth method=GET path=/v1/schedules
func (s *Service) ListSchedules(ctx context.Context, req *ListSchedulesRequest) (*ListSchedulesResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	list, err := rt.Store().ListSchedules(ctx, tenantID, strings.TrimSpace(req.RuleID), req.Limit)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListSchedulesResponse{Schedules: make([]ScheduleResponse, 0, len(list))}
	for _, sc := range list {
		out.Schedules = append(out.Schedules, scheduleResponse(sc))
	}
	return out, nil
}

// SetScheduleEnabled turns a schedule on or off. Enabling recomputes the next
// occurrence from now; disabling clears it, so neither a burst of catch-up runs
// nor a schedule that can never fire again is possible.
//
//encore:api auth method=POST path=/v1/schedules/:id/enabled
func (s *Service) SetScheduleEnabled(ctx context.Context, id string, req *SetEnabledRequest) (*ScheduleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	current, err := rt.Store().GetSchedule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	var next *time.Time
	if req.Enabled {
		spec := runtime.SpecOf(current)
		spec.Validate()
		n, err := spec.NextAfter(rt.Now())
		if err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
		}
		if !n.IsZero() {
			u := n.UTC()
			next = &u
		}
	}
	if err := rt.Store().SetScheduleEnabled(ctx, tenantID, id, req.Enabled, next); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetSchedule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := scheduleResponse(stored)
	return &out, nil
}

// DeleteSchedule removes a schedule and its due time in one statement.
//
//encore:api auth method=DELETE path=/v1/schedules/:id
func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	tenantID, err := mustTenant()
	if err != nil {
		return err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return err
	}
	return internal(rt.Store().DeleteSchedule(ctx, tenantID, id))
}

// ListFires reads the occurrence history of one schedule. It is what answers
// "did the 9:00 job run, and if not, why".
//
//encore:api auth method=GET path=/v1/schedules/:id/fires
func (s *Service) ListFires(ctx context.Context, id string, req *ListFiresRequest) (*ListFiresResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().GetSchedule(ctx, tenantID, id); err != nil {
		return nil, internal(err)
	}
	fires, err := rt.Store().ListFires(ctx, id, req.Limit)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListFiresResponse{Fires: make([]FireResponse, 0, len(fires))}
	for _, f := range fires {
		out.Fires = append(out.Fires, fireResponse(f))
	}
	return out, nil
}

// PreviewSchedule shows what an expression means before it is saved. It stores
// nothing, so an operator can compare "0 9 * * 1-5" with the real calendar.
//
//encore:api auth method=POST path=/v1/schedule-preview
func (s *Service) PreviewSchedule(ctx context.Context, req *PreviewScheduleRequest) (*PreviewScheduleResponse, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	spec := specFromRequest(req.Kind, req.Cron, req.Timezone, req.RunAt, req.MisfirePolicy)
	from := rt.Now()
	if req.From != nil {
		from = *req.From
	}
	times, err := rt.PreviewSchedule(spec, from, req.Count)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	loc, _ := spec.Location()
	out := &PreviewScheduleResponse{Timezone: spec.Timezone, Next: make([]string, 0, len(times))}
	for _, t := range times {
		out.Next = append(out.Next, t.In(loc).Format(time.RFC3339))
	}
	return out, nil
}

// bootLog records which ports were configured, so a missing owner is visible at
// startup instead of being discovered when a rule fires.
func init() {
	cfg := ports.FromEnv()
	rlog.Info("automation owner starting",
		"deployment", cfg.DeploymentID,
		"community_base_url", cfg.CommunityBaseURL != "",
		"notification_url", cfg.NotificationURL != "" || cfg.CommunityBaseURL != "",
		"auth_mode", envOr("TP_AUTH_MODE", "legacy-jwt"),
	)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

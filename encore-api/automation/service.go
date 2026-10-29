// Package automation is the single Encore host for the automation, scene and
// alarm owners. It owns the rules, the runtime state, the alarm episodes and the
// outbound intents. Device commands, notifications and the device directory stay
// in their existing modules and are reached only through narrow ports.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/rlog"
	"encore.dev/storage/sqldb"

	"encore.app/automation/domain"
	"encore.app/automation/ingress"
	"encore.app/automation/legacy"
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
	// A create that was retried returns the rule the first attempt made, not a
	// second one with the same name. If that rule has since been deleted, the
	// key is stale and the create runs for real.
	if prior, ok := s.replayRule(ctx, tenantID, req.RequestID); ok {
		return prior, nil
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
	s.remember(ctx, tenantID, req.RequestID, "create_rule", &out)
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
	// A save that was retried replays the first answer. Without this the retry
	// fails on the version check and reads as "somebody else edited this".
	if prior, ok := s.replayRule(ctx, tenantID, req.RequestID); ok {
		return prior, nil
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

	// "普通保存内部完成必要校验": a save that would orphan the per-device values
	// three devices are running on is refused here, not left for somebody to
	// notice later on the device page.
	if err := s.checkBindingsAfterEdit(ctx, tenantID, id, def, req.Force); err != nil {
		return nil, err
	}
	// A preview that no longer matches this rule is not a preview of this save.
	if err := s.checkPreviewToken(ctx, tenantID, id, req, existing); err != nil {
		return nil, err
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
	s.remember(ctx, tenantID, req.RequestID, "update_rule", &out)
	return &out, nil
}

// checkBindingsAfterEdit refuses an edit whose stored per-device values no
// longer fit the new parameter contract.
//
// It only speaks when there is something to lose: a rule with no bindings, or
// one whose bindings carry no parameters, saves exactly as before.
func (s *Service) checkBindingsAfterEdit(ctx context.Context, tenantID, ruleID string,
	def domain.Definition, force bool) error {

	rt, err := s.rtOrErr()
	if err != nil {
		return err
	}
	bindings, berr := rt.Store().ListBindingsForRule(ctx, tenantID, ruleID)
	if berr != nil {
		return internal(berr)
	}
	var broken []domain.Issue
	for _, b := range bindings {
		if !b.Enabled || len(b.Params) == 0 {
			continue
		}
		if _, verr := def.Values(b.Params); verr != nil {
			broken = append(broken, domain.Issue{
				Field: "bindings", Code: "binding_incompatible",
				Message: fmt.Sprintf("%s 上的值无法套用到新版本：%v", scopeLabel(b), verr),
			})
		}
	}
	if len(broken) == 0 || force {
		return nil
	}
	e := detailError(errs.FailedPrecondition,
		"这次改动会让已有设备的参数值失效："+broken[0].Message+"；请先调整那些设备的值，或确认后强制保存",
		ErrorDetail{
			Code: "binding_incompatible",
			Fields: []map[string]string{{
				"field": "bindings", "code": "binding_incompatible", "message": broken[0].Message,
			}},
		})
	e.Meta["affected"] = len(broken)
	return e
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
	rules, err := rt.Store().ListRules(ctx, tenantID, store.RuleFilter{
		Kind:           req.Kind,
		DeviceID:       req.DeviceID,
		DeviceConfigID: req.DeviceConfigID,
		Name:           req.Name,
		Enabled:        parseEnabled(req.Enabled),
		HasAlarmStep:   req.HasAlarmStep,
		Limit:          req.Limit,
		Offset:         req.Offset,
	})
	if err != nil {
		return nil, internal(err)
	}
	out := &ListRulesResponse{Rules: make([]RuleResponse, 0, len(rules))}
	for _, r := range rules {
		out.Rules = append(out.Rules, toRuleResponse(r))
	}
	// Total is the number of matching rules, not the size of this page. The App
	// dashboard asks for one row just to print a count.
	total, err := rt.Store().CountRules(ctx, tenantID, store.RuleFilter{
		Kind:           req.Kind,
		DeviceID:       req.DeviceID,
		DeviceConfigID: req.DeviceConfigID,
		Name:           req.Name,
		Enabled:        parseEnabled(req.Enabled),
		HasAlarmStep:   req.HasAlarmStep,
	})
	if err != nil {
		return nil, internal(err)
	}
	out.Total = total
	return out, nil
}

// ResolveLegacyRef maps an id from the old console onto the rule that now owns
// it (A30).
//
// The old App and the old panel still send `scene.id` and
// `scene_automations.id`. Those two id spaces are not the same object and were
// never unique across kinds, so this reports ambiguity instead of choosing: a
// compat adapter that silently picks the first match turns "activate the
// hallway scene" into "run some other rule".
//
//encore:api auth method=GET path=/v1/legacy-refs/:legacyId
func (s *Service) ResolveLegacyRef(ctx context.Context, legacyId string) (*LegacyRefResponse, error) {
	legacyID := legacyId
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(legacyID) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "legacy id 不能为空"}
	}
	candidates, err := rt.Store().FindRulesByLegacyID(ctx, tenantID, legacyID, 5)
	if err != nil {
		return nil, internal(err)
	}
	if len(candidates) == 0 {
		return nil, detailError(errs.NotFound, "这个旧 id 还没有迁移到新规则", ErrorDetail{
			Code: "legacy_ref_missing", Field: "legacy_id",
		})
	}
	out := &LegacyRefResponse{
		LegacyID:   legacyID,
		RuleID:     candidates[0].ID,
		Kind:       string(candidates[0].Kind),
		Name:       candidates[0].Name,
		Enabled:    candidates[0].Enabled,
		Ambiguous:  len(candidates) > 1,
		Candidates: make([]string, 0, len(candidates)),
	}
	for _, c := range candidates {
		out.Candidates = append(out.Candidates, c.ID)
	}
	return out, nil
}

// parseEnabled turns the query string into a tri-state filter. An empty value
// means "any", which is what the old lists did: they showed enabled and disabled
// rules together with a switch on each card.
func parseEnabled(v string) *bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		t := true
		return &t
	case "false", "0", "no":
		f := false
		return &f
	default:
		return nil
	}
}

// toQuery turns the query string into a store query, refusing values that would
// silently return nothing.
//
// An unknown status is the dangerous one: filtering on "faild" would show an
// empty list, and an empty list reads as "nothing failed". Refusing is the only
// answer that cannot be misread.
func (r *ListRunsRequest) toQuery(tenantID, ruleID string) (store.RunQuery, error) {
	q := store.RunQuery{TenantID: tenantID, RuleID: ruleID, Limit: r.Limit, Offset: r.Offset}
	// The window is normalised here, not only inside the store, so the response
	// can echo the window that was actually used instead of the zero the caller
	// left out.
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	if r.Status != "" {
		for _, part := range strings.Split(r.Status, ",") {
			s := strings.TrimSpace(part)
			if s == "" {
				continue
			}
			if !knownRunStatus(s) {
				return q, detailError(errs.InvalidArgument,
					"没有这个运行状态："+s, ErrorDetail{Code: "unknown_status", Field: "status"})
			}
			q.Statuses = append(q.Statuses, s)
		}
	}
	for _, pair := range []struct {
		raw    string
		target **time.Time
		field  string
	}{{r.Before, &q.Before, "before"}, {r.After, &q.After, "after"}} {
		if pair.raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, pair.raw)
		if err != nil {
			return q, &errs.Error{Code: errs.InvalidArgument,
				Message: pair.field + " 需要 RFC3339 时间，例如 2026-10-23T08:00:00Z",
				Meta:    errs.Metadata{"field": pair.field}}
		}
		*pair.target = &t
	}
	return q, nil
}

func knownRunStatus(s string) bool {
	switch s {
	case runtime.StatusAccepted, runtime.StatusRunning, runtime.StatusQueued,
		runtime.StatusSucceeded, runtime.StatusPartial, runtime.StatusFailed,
		runtime.StatusCancelled, runtime.StatusIgnored, runtime.StatusRejected,
		runtime.StatusExpired:
		return true
	}
	return false
}

// DeleteRule lives in reference_service.go: A20 gave it a reference pre-check,
// a tombstone reason and a body describing what it stopped.

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
		// The message is for humans; fields is for everything else. A CLI, an
		// MCP tool and the web form all need to know *which* input was wrong,
		// and none of them should be parsing Chinese prose to find out.
		fields := make([]map[string]string, 0, len(ve.Issues))
		for _, i := range ve.Issues {
			msgs = append(msgs, i.Field+": "+i.Message)
			fields = append(fields, map[string]string{
				"field": i.Field, "code": i.Code, "message": i.Message,
			})
		}
		return detailError(errs.InvalidArgument, strings.Join(msgs, "; "), ErrorDetail{
			Code: "invalid_definition", Fields: fields,
		})
	}
	var pe *domain.ParamError
	if errors.As(err, &pe) {
		return detailError(errs.InvalidArgument, err.Error(), ErrorDetail{
			Code: "invalid_param",
			Fields: []map[string]string{{
				"field": "params." + pe.Param, "code": pe.Code, "message": pe.Message,
			}},
		})
	}
	return detailError(errs.InvalidArgument, err.Error(), ErrorDetail{Code: "invalid_definition"})
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
		Steps: res.Steps, Actions: res.Actions, WouldRun: res.WouldRun,
		SideEffects: res.SideEffects, EndsAt: res.EndsAt, Notes: res.Notes,
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
	return s.ingestUplink(ctx, tenantID, req)
}

func (s *Service) ingestUplink(ctx context.Context, tenantID string, req *UplinkRequest) (*UplinkResponse, error) {
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

// ListRuleRuns lists runs of a rule, one page at a time.
//
// The old version returned the latest fifty and nothing else. That is not a
// listing, it is a sample: a page showing twenty of them could not say whether
// there were more, and "show me what failed yesterday" was unanswerable. The
// filters below are the four questions the run log actually gets asked, and
// `total` is what lets a pager show a count instead of "next" forever.
//
//encore:api auth method=GET path=/v1/rules/:id/runs
func (s *Service) ListRuleRuns(ctx context.Context, id string, req *ListRunsRequest) (*ListRunsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &ListRunsRequest{}
	}
	q, perr := req.toQuery(tenantID, id)
	if perr != nil {
		return nil, perr
	}
	runs, total, err := rt.Store().ListRunsPaged(ctx, q)
	if err != nil {
		return nil, internal(err)
	}
	out := &ListRunsResponse{
		Runs:  make([]RunResponse, 0, len(runs)),
		Total: total, Limit: q.Limit, Offset: q.Offset,
	}
	for _, r := range runs {
		out.Runs = append(out.Runs, toRunResponse(r, nil, nil, nil))
	}
	return out, nil
}

// RunRule executes a rule immediately, without evaluating its trigger.
//
// This is the endpoint behind the legacy scene page's "激活" button: the old
// scene object had no condition and no enable switch, the user was the trigger.
// Without it that button simply stops working on the new backend, which is a
// regression rather than a simplification.
//
// The run-mode contract still applies (single/restart/queued/parallel), because
// pressing the button twice is exactly the re-entry those modes describe. A
// skipped admission comes back as a run with status ignored/rejected, so the
// caller can say "已被忽略：上一次还没跑完" instead of showing nothing.
//
//encore:api auth method=POST path=/v1/rules/:id/run
func (s *Service) RunRule(ctx context.Context, id string, req *RunRuleRequest) (*RunResponse, error) {
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
	deviceID := ""
	if req != nil {
		deviceID = strings.TrimSpace(req.DeviceID)
	}
	// "Run now" is the one endpoint that reaches a device on demand, so it is
	// also the one a retried request would double. The key is checked before
	// the run is created, not after: a run that exists is already a command.
	var prior RunResponse
	if req != nil && s.replay(ctx, tenantID, req.RequestID, &prior) {
		return &prior, nil
	}
	run, err := rt.RunNow(ctx, rule, deviceID)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
	}
	steps, _ := rt.Store().ListSteps(ctx, run.ID)
	targets, _ := rt.Store().ListTargets(ctx, run.ID)
	outbox, _ := rt.Store().OutboxForRun(ctx, run.ID)
	out := toRunResponse(run, steps, targets, outbox)
	if req != nil {
		s.remember(ctx, tenantID, req.RequestID, "run_rule", &out)
	}
	return &out, nil
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
		out.Alarms = append(out.Alarms, alarmResponse(e))
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

// GetAlarm returns one episode, active or historical, so an existing detail link
// keeps working after the alarm recovered.
//
//encore:api auth method=GET path=/v1/alarms/:id
func (s *Service) GetAlarm(ctx context.Context, id string) (*AlarmResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	ep, err := rt.Store().GetEpisodeByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "告警不存在"}
		}
		return nil, internal(err)
	}
	out := alarmResponse(ep)
	return &out, nil
}

// SubmitAlarmEvent submits an authorised alarm action on the event channel.
//
// This is the intake the legacy scene "raise this alarm" action uses. It is not a
// generic "set the verdict" endpoint: the policy must declare the event channel,
// the caller must be on its whitelist, and only qualified/clear can be asserted.
// A detector policy refuses it outright, which is what stops an ordinary business
// request from impersonating the evaluator.
//
//encore:api auth method=POST path=/v1/alarm-policies/:id/events
func (s *Service) SubmitAlarmEvent(ctx context.Context, id string, req *AlarmEventRequest) (*AlarmEventResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if req.Payload != "" {
		if err := json.Unmarshal([]byte(req.Payload), &payload); err != nil {
			return nil, &errs.Error{Code: errs.InvalidArgument,
				Message: "payload 必须是合法 JSON"}
		}
	}
	dec, err := rt.SubmitAlarmEvent(ctx, tenantID, id, runtime.AlarmEventRequest{
		ActionID: req.ActionID, Source: req.Source, Kind: req.Kind,
		SubjectID: req.SubjectID, Dimension: req.Dimension, Payload: payload,
	})
	if err != nil {
		return nil, internal(err)
	}
	return &AlarmEventResponse{
		Outcome: dec.Outcome, ReasonCode: dec.ReasonCode,
		EpisodeID: dec.EpisodeID, TransitionID: dec.TransitionID, Replayed: dec.Replayed,
	}, nil
}

// ReconcileAlarms reports this service's own alarm counters so the migration tool
// can diff them against the legacy alarm_info / alarm_history rows.
//
//encore:api auth method=GET path=/v1/alarm-reconcile
func (s *Service) ReconcileAlarms(ctx context.Context) (*AlarmReconcileResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	st, err := rt.Store().AlarmStatsOf(ctx, tenantID)
	if err != nil {
		return nil, internal(err)
	}
	return &AlarmReconcileResponse{
		ActiveEpisodes: st.ActiveEpisodes, TotalEpisodes: st.TotalEpisodes,
		Transitions: st.Transitions, ByPolicy: st.ByPolicy,
		ByState: st.ByState, ByLevel: st.ByLevel,
	}, nil
}

func alarmResponse(e *store.Episode) AlarmResponse {
	out := AlarmResponse{
		ID: e.ID, PolicyID: e.PolicyID, PolicyKey: e.PolicyKey, SubjectID: e.SubjectID,
		Dimension: e.Dimension, State: e.State, Level: e.Level, Source: e.Source,
		Revision: e.Revision, Epoch: e.Epoch, OpenedAt: e.OpenedAt,
		FirstSeq: e.FirstSeq, LastSeq: e.LastSeq, SampleCount: e.SampleCount,
		AckedAt: e.AckedAt, AckedBy: e.AckedBy, ClearedAt: e.ClearedAt,
	}
	if len(e.LastEvidence) > 0 {
		out.LastEvidence = string(e.LastEvidence)
	}
	return out
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
// A13 legacy scene migration
// ---------------------------------------------------------------------------

// legacySnapshot answers the migration questions from the snapshot the caller
// supplied, plus this service's own alarm policies.
//
// The split matters: device and scene existence come from the legacy database,
// which only the caller can read; the alarm policy mapping comes from here,
// because that is where the policy now lives. Neither side guesses about the
// other.
type legacySnapshot struct {
	scenes  map[string]*legacy.Scene
	devices map[string]bool
	configs map[string]bool
	resolve func(legacyAlarmID string) (string, string, bool, error)
}

func (l *legacySnapshot) SceneExists(id string) (bool, error) {
	_, ok := l.scenes[id]
	return ok, nil
}

func (l *legacySnapshot) DeviceExists(id string) (bool, error) { return l.devices[id], nil }

func (l *legacySnapshot) DeviceConfigExists(id string) (bool, error) { return l.configs[id], nil }

func (l *legacySnapshot) AlarmPolicyFor(id string) (string, string, bool, error) {
	return l.resolve(id)
}

func (l *legacySnapshot) Load(id string) (*legacy.Scene, error) { return l.scenes[id], nil }

func toLegacyScene(in *LegacySceneInput) *legacy.Scene {
	out := &legacy.Scene{ID: in.ID, Name: in.Name}
	for _, a := range in.Actions {
		out.Actions = append(out.Actions, legacy.Action{
			ID: a.ID, Target: a.Target, Type: a.Type, ParamType: a.ParamType,
			Param: a.Param, Value: a.Value, ContinueOnFailure: a.ContinueOnFailure,
		})
	}
	return out
}

// PlanLegacyScene is the side-effect-free migration preview for one old scene.
//
// It classifies the scene, maps every action, reports every problem at once, and
// returns the exact definition the migration would publish. Nothing is written:
// the operator compares the plan with the old behaviour first, which is what makes
// "迁移前后行为可对照" a fact instead of a promise.
//
//encore:api auth method=POST path=/v1/legacy/scene-plan
func (s *Service) PlanLegacyScene(ctx context.Context, req *LegacyScenePlanRequest) (*LegacyScenePlanResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}

	snap := &legacySnapshot{
		scenes:  map[string]*legacy.Scene{},
		devices: map[string]bool{},
		configs: map[string]bool{},
	}
	scene := toLegacyScene(&req.Scene)
	if scene.ID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "scene.id 必填"}
	}
	snap.scenes[scene.ID] = scene
	for i := range req.Graph {
		g := toLegacyScene(&req.Graph[i])
		if g.ID != "" {
			snap.scenes[g.ID] = g
		}
	}
	for _, d := range req.Devices {
		snap.devices[d] = true
	}
	for _, c := range req.DeviceConfigs {
		snap.configs[c] = true
	}
	// The legacy alarm id is what the old form stored in action_target. The
	// mapping is by legacy_id, which the migration writes onto the policy so the
	// active alarm keeps its identity.
	snap.resolve = func(legacyAlarmID string) (string, string, bool, error) {
		rule, gerr := rt.Store().GetRuleByLegacyID(ctx, tenantID, legacyAlarmID)
		if gerr != nil {
			if errors.Is(gerr, store.ErrNotFound) {
				return "", "", false, nil
			}
			return "", "", false, gerr
		}
		if rule.Kind != domain.KindAlarm {
			return "", "", false, nil
		}
		return rule.ID, rule.PolicyKey(), true, nil
	}

	plan := legacy.Build(scene, snap)
	out := &LegacyScenePlanResponse{
		SceneID: plan.SceneID, Name: plan.Name, Shape: string(plan.Shape),
		Migratable: plan.OK(), Definition: plan.Definition,
		ContinuedSteps: plan.ContinuedSteps,
	}
	for i, m := range plan.Mapping {
		out.Mapping = append(out.Mapping, LegacyStepMappingResponse{
			Index: i, LegacyActionID: m.LegacyActionID, LegacyType: m.LegacyType,
			StepKind: m.StepKind, Target: m.Target,
			ContinueOnFailure: m.ContinueOnFailure, Note: m.Note,
		})
	}
	for _, iss := range plan.Issues {
		out.Issues = append(out.Issues, LegacyIssueResponse{
			Code: iss.Code, Field: iss.Field, Message: iss.Message,
		})
	}
	// A scene that activates other scenes can still loop even when every single
	// action resolves. Check the graph, not just the rows.
	if path, iss := legacy.CheckCycles(scene.ID, snap); iss != nil {
		out.Migratable = false
		out.CyclePath = path
		out.Issues = append(out.Issues, LegacyIssueResponse{
			Code: iss.Code, Field: iss.Field, Message: iss.Message,
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
func (s *Service) Tick(ctx context.Context) (*TickResponse, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	res, err := rt.Tick(ctx)
	if err != nil {
		return nil, internal(err)
	}
	// Dedupe rows are a retry window, not an audit log. The delete is indexed
	// and normally matches nothing, so it rides along with the sweep. A failure
	// here must not fail the tick: the scheduler has real work to report.
	pruned := 0
	if n, perr := rt.Store().PruneRequests(ctx, requestRetentionHours); perr == nil {
		pruned = int(n)
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
		RequestsPruned: pruned,
	}, nil
}

// Recover replays receipts that were received but not evaluated before a crash.
//
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

// ExplainRule answers "why did this rule not run?" for one device and window.
//
// It is a separate endpoint from GetRun because the interesting case is the one
// with no run at all. Without it the only honest answer to a customer is "look
// in the logs", which is the situation A17 exists to remove.
//
//encore:api auth method=GET path=/v1/rules/:id/explain
func (s *Service) ExplainRule(ctx context.Context, id string, req *ExplainRequest) (*ExplainResponse, error) {
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
	from, err := parseTimeParam(req.From)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "from 不是合法的 RFC3339 时间"}
	}
	to, err := parseTimeParam(req.To)
	if err != nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "to 不是合法的 RFC3339 时间"}
	}
	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID = firstTargetDevice(rule.Definition)
	}
	if deviceID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "必须指定 device_id，规则也没有可推断的目标设备"}
	}

	res, err := rt.Explain(ctx, rule, deviceID, from, to)
	if err != nil {
		return nil, internal(err)
	}
	out := &ExplainResponse{
		RuleID: res.RuleID, DeviceID: res.DeviceID, From: res.From, To: res.To,
		Verdict: string(res.Verdict), Reasons: res.Reasons,
		Evidence: EvidenceResponse{
			RunsInWindow:  res.Evidence.RunsInWindow,
			StateObserved: res.Evidence.StateObserved,
			Complete:      res.Evidence.Complete,
			Note:          res.Evidence.Note,
		},
	}
	for _, r := range res.Runs {
		out.Runs = append(out.Runs, toRunResponse(r.Run, r.Steps, r.Target, nil))
	}
	if p := res.Pending; p != nil {
		out.Pending = &PendingResponse{
			ScopeKey: p.ScopeKey, StartedAt: p.StartedAt, Deadline: p.Deadline,
			LastEvidenceAt: p.LastEvidenceAt, RequireBefore: p.RequireBefore,
			DeferCount: p.DeferCount, RemainingMillis: p.RemainingMillis,
		}
	}
	if i := res.Input; i != nil {
		out.Input = &InputResponse{
			DeviceID: i.DeviceID, Signal: string(i.Signal), Key: i.Key, Value: encodeSampleValue(i.Value),
			Quality: string(i.Quality), ObservedAt: i.ObservedAt, AgeMillis: i.AgeMillis,
		}
	}
	return out, nil
}

// encodeSampleValue renders a stored sample value as JSON text for the API.
func encodeSampleValue(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// parseTimeParam accepts an empty value as "not given".
func parseTimeParam(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}

// firstTargetDevice infers the device from the rule when the caller did not name
// one: an explicit single device target, or the rule's own trigger device.
func firstTargetDevice(def domain.Definition) string {
	if len(def.Targets.DeviceIDs) == 1 {
		return def.Targets.DeviceIDs[0]
	}
	for _, g := range def.Trigger.Groups {
		for _, c := range g.Conditions {
			if c.Source == "device" && c.DeviceID != "" {
				return c.DeviceID
			}
		}
	}
	return ""
}

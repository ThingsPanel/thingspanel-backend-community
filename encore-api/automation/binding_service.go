package automation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A19: bindings, inheritance and per-device overrides
// ---------------------------------------------------------------------------
//
// The old console let a scene automation be attached to a device template: the
// condition's trigger source was a device_config_id, and every device of that
// template reacted. That behaviour is a first-delivery requirement, not a new
// feature, so it is modelled explicitly instead of being re-derived from
// condition text.
//
// A binding says "this rule applies to this scope". Two scopes exist:
//
//   device          one device
//   device_config   every device of that template, present and future
//
// Values are not copied into the rule: the rule keeps its placeholders and the
// binding supplies the numbers. That is what makes "change the threshold on one
// device" a one-row edit instead of a second rule that nobody can find later.

// AttachBindingRequest binds a rule to one device or one device template.
type AttachBindingRequest struct {
	// ScopeType is "device" or "device_config".
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	// Params fills the rule's declared parameter slots. Omitted slots keep the
	// rule's defaults, so binding a rule with no parameters needs no body at
	// all — which is exactly the legacy "attach this automation to this
	// template" gesture.
	Params map[string]string `json:"params"`
}

// BindingResponse is one binding as the device or template page shows it.
type BindingResponse struct {
	ID             string            `json:"id"`
	RuleID         string            `json:"rule_id"`
	RuleName       string            `json:"rule_name,omitempty"`
	Source         string            `json:"source"`
	DeviceID       string            `json:"device_id,omitempty"`
	DeviceConfigID string            `json:"device_config_id,omitempty"`
	Revision       int               `json:"revision"`
	Params         map[string]string `json:"params,omitempty"`
	Enabled        bool              `json:"enabled"`
	DetachedAt     *time.Time        `json:"detached_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	// EffectiveParams is what this binding actually produces after defaults are
	// applied. The device page shows this, because "I set 60" and "the rule
	// uses 60 because that is the default" are different facts.
	EffectiveParams map[string]string `json:"effective_params,omitempty"`
	// Issues are advisory notes about the scope (device not in the directory
	// yet, template has no devices). They never mean the binding failed.
	Issues []domain.Issue `json:"issues,omitempty"`
}

// ListBindingsResponse lists the bindings of a rule or a scope.
type ListBindingsResponse struct {
	Bindings []BindingResponse `json:"bindings"`
	// Note explains an empty list instead of leaving the caller to guess
	// between "nothing is bound" and "the query was wrong".
	Note string `json:"note,omitempty"`
}

// EffectiveRuleResponse is one rule that really applies to a device.
type EffectiveRuleResponse struct {
	RuleID   string            `json:"rule_id"`
	RuleName string            `json:"rule_name"`
	Kind     string            `json:"kind"`
	Enabled  bool              `json:"enabled"`
	// Source is "template", "device" or "condition".
	//
	// "condition" is the legacy behaviour: a rule whose trigger names no device
	// applies to every device that reports the matching signal. It is shown so
	// the device page does not pretend a rule is bound when it is not.
	Source    string            `json:"source"`
	BindingID string            `json:"binding_id,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
	// Effective is the parameter set after defaults, i.e. what the evaluator
	// will actually compare against.
	Effective map[string]string `json:"effective_params,omitempty"`
	// Overridden lists the parameter names this device sets itself, so the page
	// can mark them "本设备覆盖" without diffing two maps in the browser.
	Overridden []string `json:"overridden,omitempty"`
}

// DeviceAutomationResponse answers "what applies to this device, and why".
type DeviceAutomationResponse struct {
	DeviceID       string                  `json:"device_id"`
	DeviceConfigID string                  `json:"device_config_id,omitempty"`
	Rules          []EffectiveRuleResponse `json:"rules"`
	// Issues are things we could not check, not failures: a directory that does
	// not answer must not be reported as "this device has no rules".
	Issues []domain.Issue `json:"issues,omitempty"`
}

// TemplateBindingsResponse is the reconciliation view for one template.
type TemplateBindingsResponse struct {
	DeviceConfigID string            `json:"device_config_id"`
	Attached       int               `json:"attached"`
	Detached       int               `json:"detached"`
	// DeviceCount is how many devices currently inherit these bindings. It is
	// the "生效范围" half of the reconciliation: a template with three bindings
	// and zero devices is a configuration nobody is using.
	DeviceCount int               `json:"device_count"`
	Bindings    []BindingResponse `json:"bindings"`
	Issues      []domain.Issue    `json:"issues,omitempty"`
}

// AttachBinding binds a rule to a device or a template.
//
//encore:api auth method=POST path=/v1/rules/:id/bindings
func (s *Service) AttachBinding(ctx context.Context, id string, req *AttachBindingRequest) (*BindingResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &AttachBindingRequest{}
	}
	scopeType := strings.ToLower(strings.TrimSpace(req.ScopeType))
	scopeID := strings.TrimSpace(req.ScopeID)
	if scopeID == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "scope_id 不能为空"}
	}
	rule, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, notFoundOr(err, "规则不存在")
	}

	b := &store.Binding{
		ID:       runtime.NewID(),
		TenantID: tenantID,
		RuleID:   rule.ID,
		Revision: rule.Revision,
		Params:   map[string]string{},
		Enabled:  true,
	}
	switch scopeType {
	case "device":
		b.DeviceID = scopeID
		b.Source = store.BindingSourceDevice
	case "device_config", "template":
		b.DeviceConfigID = scopeID
		b.Source = store.BindingSourceTemplate
	default:
		return nil, &errs.Error{Code: errs.InvalidArgument,
			Message: fmt.Sprintf("scope_type 只能是 device 或 device_config，收到 %q", req.ScopeType)}
	}

	// Validate the values against the rule's declared slots *before* storing
	// them. A binding that stores "40 秒" for a number and only fails when the
	// device reports is worse than a refusal now: nobody is watching at 3am.
	if len(req.Params) > 0 {
		if _, err := rule.Definition.Values(req.Params); err != nil {
			return nil, paramErr(err)
		}
		b.Params = req.Params
	}
	// A device that cannot be found is not a reason to refuse: the device list
	// is another module, and creating the binding first then adding the device
	// is a legitimate order of work. It is reported instead.
	issues := checkBindingScope(ctx, rt, tenantID, scopeType, scopeID)

	b.BindingKey = store.BindingKey(tenantID, rule.ID, b.DeviceID, b.DeviceConfigID)
	if err := rt.Store().UpsertBinding(ctx, b); err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "绑定失败: " + err.Error()}
	}
	// The ingest path caches "which template is this device" and "does this
	// tenant use templates". A new binding must be visible on the next uplink,
	// not in five minutes.
	rt.InvalidateBindings(tenantID, b.DeviceID)

	stored, err := rt.Store().GetBinding(ctx, tenantID, b.ID)
	if err != nil {
		// Upsert may have hit an existing row with a different id (re-attach of
		// a detached binding keeps the original id). Re-read by key.
		stored = b
	}
	resp := toBindingResponse(stored, rule)
	resp.EffectiveParams, _ = effectiveParams(rule, stored.Params)
	resp.Issues = issues
	return resp, nil
}

// ListRuleBindings lists every scope a rule is attached to.
//
//encore:api auth method=GET path=/v1/rules/:id/bindings
func (s *Service) ListRuleBindings(ctx context.Context, id string) (*ListBindingsResponse, error) {
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
		return nil, notFoundOr(err, "规则不存在")
	}
	bindings, err := rt.Store().ListBindingsForRule(ctx, tenantID, id)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	out := &ListBindingsResponse{}
	for _, b := range bindings {
		r := toBindingResponse(b, rule)
		r.EffectiveParams, _ = effectiveParams(rule, b.Params)
		out.Bindings = append(out.Bindings, *r)
	}
	if len(out.Bindings) == 0 {
		if rule.Definition.HasParams() {
			out.Note = "这条规则带参数，但还没有绑定任何设备或模板；绑定前它按默认值对所有匹配的设备生效"
		} else {
			out.Note = "这条规则没有绑定任何设备或模板，按触发条件对所有匹配的设备生效"
		}
	}
	return out, nil
}

// DetachBinding stops a binding.
//
// The row is kept with a detached_at date: "why did it stop running" and the
// reconciliation both need it, and deleting the row would answer neither.
//
//encore:api auth method=DELETE path=/v1/bindings/:id
func (s *Service) DetachBinding(ctx context.Context, id string) (*BindingResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	b, err := rt.Store().GetBinding(ctx, tenantID, id)
	if err != nil {
		return nil, notFoundOr(err, "绑定不存在")
	}
	if err := rt.Store().DetachBinding(ctx, tenantID, id); err != nil {
		if err == store.ErrAlreadyDone {
			// Idempotent: detaching twice is the same outcome, and a retried
			// request must not look like a failure.
			return toBindingResponse(b, nil), nil
		}
		return nil, notFoundOr(err, "绑定不存在")
	}
	rt.InvalidateBindings(tenantID, b.DeviceID)
	fresh, gerr := rt.Store().GetBinding(ctx, tenantID, id)
	if gerr != nil {
		fresh = b
		fresh.Enabled = false
	}
	return toBindingResponse(fresh, nil), nil
}

// GetDeviceAutomation answers "what really applies to this device".
//
// It is the device page's single read: template bindings, device overrides and
// the legacy "condition matches any device" rules are all listed with their
// source, so the page cannot show two different answers for the same device.
//
//encore:api auth method=GET path=/v1/device-automation/:deviceId
func (s *Service) GetDeviceAutomation(ctx context.Context, deviceId string) (*DeviceAutomationResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	out := &DeviceAutomationResponse{DeviceID: deviceId, Rules: []EffectiveRuleResponse{}}
	if rt.Directory() != nil {
		devices, derr := rt.Directory().ByIDs(ctx, tenantID, []string{deviceId})
		if derr != nil {
			out.Issues = append(out.Issues, domain.Issue{
				Field: "device_id", Code: "directory_unavailable",
				Message: "设备目录暂时不可用，无法确认该设备所属模板；设备级绑定仍然有效",
			})
		} else if len(devices) > 0 {
			out.DeviceConfigID = devices[0].DeviceConfigID
		}
	}

	bound, err := rt.Store().BindingsForDevice(ctx, tenantID, deviceId, out.DeviceConfigID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	seen := map[string]bool{}
	for _, b := range bound {
		rule, rerr := rt.Store().GetRule(ctx, tenantID, b.RuleID)
		if rerr != nil {
			out.Issues = append(out.Issues, domain.Issue{
				Field: "rule_id", Code: "binding_target_missing",
				Message: "绑定指向的规则已不存在: " + b.RuleID,
			})
			continue
		}
		eff, _ := effectiveParams(rule, b.Params)
		row := EffectiveRuleResponse{
			RuleID: rule.ID, RuleName: rule.Name, Kind: string(rule.Kind),
			Enabled: rule.Enabled, Source: b.Source, BindingID: b.ID,
			Params: b.Params, Effective: eff,
		}
		if b.DeviceID != "" {
			row.Overridden = store.SortedParamKeys(b.Params)
		}
		if prev, ok := indexOfRule(out.Rules, rule.ID); ok {
			// Template first, device second: the later row wins and is the one
			// the page must show, but the overridden list comes from the device
			// row only.
			out.Rules[prev] = row
		} else {
			out.Rules = append(out.Rules, row)
		}
		seen[rule.ID] = true
	}

	// Legacy condition-matched rules. They are not bound, but they do apply, and
	// hiding them would make the page lie by omission.
	conditionRules, cerr := rt.Store().RulesMatchingDevice(ctx, tenantID, deviceId)
	if cerr != nil {
		out.Issues = append(out.Issues, domain.Issue{
			Field: "rules", Code: "condition_scan_failed", Message: cerr.Error(),
		})
	}
	for _, rule := range conditionRules {
		if seen[rule.ID] {
			continue
		}
		out.Rules = append(out.Rules, EffectiveRuleResponse{
			RuleID: rule.ID, RuleName: rule.Name, Kind: string(rule.Kind),
			Enabled: rule.Enabled, Source: "condition",
		})
	}
	sort.Slice(out.Rules, func(i, j int) bool {
		if out.Rules[i].Source != out.Rules[j].Source {
			return out.Rules[i].Source < out.Rules[j].Source
		}
		return out.Rules[i].RuleName < out.Rules[j].RuleName
	})
	return out, nil
}

// GetTemplateBindings is the reconciliation view for one device template.
//
//encore:api auth method=GET path=/v1/device-configs/:id/bindings
func (s *Service) GetTemplateBindings(ctx context.Context, id string) (*TemplateBindingsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	out := &TemplateBindingsResponse{DeviceConfigID: id}
	attached, detached, cerr := rt.Store().CountBindings(ctx, tenantID, id)
	if cerr != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: cerr.Error()}
	}
	out.Attached, out.Detached = attached, detached

	rows, err := rt.Store().ListBindingsForScope(ctx, tenantID, "", id)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: err.Error()}
	}
	for _, b := range rows {
		var rule *store.Rule
		if r, rerr := rt.Store().GetRule(ctx, tenantID, b.RuleID); rerr == nil {
			rule = r
		}
		resp := toBindingResponse(b, rule)
		if rule != nil {
			resp.EffectiveParams, _ = effectiveParams(rule, b.Params)
		}
		out.Bindings = append(out.Bindings, *resp)
	}

	if rt.Directory() != nil {
		devices, derr := rt.Directory().ByDeviceConfig(ctx, tenantID, []string{id})
		if derr != nil {
			out.Issues = append(out.Issues, domain.Issue{
				Field: "device_config_id", Code: "directory_unavailable",
				Message: "设备目录暂时不可用，无法统计该模板下的设备数量",
			})
		} else {
			out.DeviceCount = len(devices)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func toBindingResponse(b *store.Binding, rule *store.Rule) *BindingResponse {
	out := &BindingResponse{
		ID: b.ID, RuleID: b.RuleID, Source: b.Source,
		DeviceID: b.DeviceID, DeviceConfigID: b.DeviceConfigID,
		Revision: b.Revision, Params: b.Params, Enabled: b.Enabled,
		DetachedAt: b.DetachedAt, CreatedAt: b.CreatedAt,
	}
	if rule != nil {
		out.RuleName = rule.Name
	}
	return out
}

// effectiveParams returns the full parameter set, defaults included.
func effectiveParams(rule *store.Rule, overrides map[string]string) (map[string]string, error) {
	if !rule.Definition.HasParams() {
		return nil, nil
	}
	return rule.Definition.Values(overrides)
}

func indexOfRule(rows []EffectiveRuleResponse, ruleID string) (int, bool) {
	for i := range rows {
		if rows[i].RuleID == ruleID {
			return i, true
		}
	}
	return 0, false
}

// checkBindingScope reports, without failing, that a scope could not be found.
//
// It is a warning rather than a refusal because the device list lives in another
// module: creating the binding first and the device second is a legitimate order
// of work, and refusing would force the operator to reverse it for no reason.
func checkBindingScope(ctx context.Context, rt *runtime.Runtime, tenantID, scopeType, scopeID string) []domain.Issue {
	if rt.Directory() == nil {
		return nil
	}
	switch scopeType {
	case "device":
		devices, err := rt.Directory().ByIDs(ctx, tenantID, []string{scopeID})
		if err == nil && len(devices) == 0 {
			return []domain.Issue{{
				Field: "scope_id", Code: "device_not_found",
				Message: "设备目录里暂时没有这台设备；绑定已保存，设备接入后自动生效",
			}}
		}
	case "device_config", "template":
		devices, err := rt.Directory().ByDeviceConfig(ctx, tenantID, []string{scopeID})
		if err == nil && len(devices) == 0 {
			return []domain.Issue{{
				Field: "scope_id", Code: "template_has_no_devices",
				Message: "该模板下还没有设备；绑定已保存，新设备接入后自动继承",
			}}
		}
	}
	return nil
}

// asParamError unwraps a parameter failure.
func asParamError(err error, target **domain.ParamError) bool {
	return errors.As(err, target)
}

// paramErr turns a parameter failure into a field-level 400.
func paramErr(err error) error {
	var pe *domain.ParamError
	if asParamError(err, &pe) {
		return detailError(errs.InvalidArgument, pe.Message, ErrorDetail{
			Field: "params." + pe.Param, Code: pe.Code,
		})
	}
	return &errs.Error{Code: errs.InvalidArgument, Message: err.Error()}
}

// notFoundOr maps a store error to the right HTTP code with a message the
// caller can act on.
func notFoundOr(err error, message string) error {
	if errors.Is(err, store.ErrNotFound) {
		return &errs.Error{Code: errs.NotFound, Message: message}
	}
	return &errs.Error{Code: errs.Internal, Message: err.Error()}
}

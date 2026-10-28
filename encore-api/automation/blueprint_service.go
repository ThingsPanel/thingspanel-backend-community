package automation

import (
	"context"
	"fmt"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/automation/blueprint"
	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A18: rule templates
// ---------------------------------------------------------------------------
//
// The endpoints below are one contract shared by Web, CLI and MCP: they all read
// the parameter list from /v1/blueprints and all get their validation from the
// same Instantiate call. Nothing here re-implements a template.

// ListBlueprints lists the built-in rule templates with their parameter forms.
//
//encore:api auth method=GET path=/v1/blueprints
func (s *Service) ListBlueprints(ctx context.Context) (*ListBlueprintsResponse, error) {
	if _, err := mustTenant(); err != nil {
		return nil, err
	}
	out := &ListBlueprintsResponse{}
	for _, bp := range blueprint.All() {
		out.Blueprints = append(out.Blueprints, toBlueprintResponse(bp))
	}
	return out, nil
}

// PreviewBlueprint shows what a set of values would produce, without creating
// anything.
//
// It answers 200 even when the values are wrong, with valid=false and the issues
// attached: a form needs to render field errors, and "the request was
// well-formed but the content is not" is exactly what an inline error is. The
// write endpoints below answer 400 instead, because there the caller asked for
// something to happen.
//
//encore:api auth method=POST path=/v1/blueprint-preview
func (s *Service) PreviewBlueprint(ctx context.Context, req *BlueprintPreviewRequest) (*BlueprintPreviewResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	bp := blueprint.Get(req.Key, req.Version)
	if bp == nil {
		return nil, &errs.Error{Code: errs.NotFound,
			Message: fmt.Sprintf("没有这个模板：%s", req.Key)}
	}
	resp := &BlueprintPreviewResponse{
		BlueprintKey: bp.Key, Version: bp.Version, Revision: bp.Revision,
	}
	inst, issues := blueprint.Instantiate(req.Key, req.Version, req.Values)
	if inst != nil {
		issues = append(issues, s.checkCapabilities(ctx, tenantID, rt, inst.Values["device"], inst.Capabilities)...)
		resp.Values = inst.Values
		resp.Overrides = inst.Overrides
		resp.Capabilities = inst.Capabilities
		resp.Definition = inst.Definition
	}
	resp.Issues = issues
	resp.Valid = inst != nil && !blueprint.HasError(issues)
	return resp, nil
}

// InstantiateBlueprint creates a real rule from a template.
//
// The rule is created through the same store path as a hand-written one, so a
// template instance is not a special kind of rule: it has a definition, a
// revision and a hash like everything else. What makes it a template instance is
// the separate origin row.
//
//encore:api auth method=POST path=/v1/blueprints/:key/instantiate
func (s *Service) InstantiateBlueprint(ctx context.Context, key string, req *InstantiateBlueprintRequest) (*RuleResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	bp := blueprint.Get(key, req.Version)
	if bp == nil {
		return nil, &errs.Error{Code: errs.NotFound,
			Message: fmt.Sprintf("没有这个模板：%s", key)}
	}
	inst, issues := blueprint.Instantiate(key, req.Version, req.Values)
	if inst == nil {
		return nil, blueprintError("模板参数不完整", issues)
	}
	issues = append(issues, s.checkCapabilities(ctx, tenantID, rt, inst.Values["device"], inst.Capabilities)...)
	if blueprint.HasError(issues) {
		return nil, blueprintError("模板参数与所选设备不匹配", issues)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = bp.Name
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: tenantID, Name: name, Kind: inst.Definition.Kind,
		Enabled: !req.Disabled, Definition: *inst.Definition,
		DefinitionHash: domain.DefinitionHash(*inst.Definition),
	}
	origin := &store.BlueprintInstance{
		RuleID: rule.ID, TenantID: tenantID,
		BlueprintKey: inst.BlueprintKey, BlueprintVersion: inst.BlueprintVersion,
		BlueprintRevision: inst.BlueprintRevision,
		Params:            inst.Values, Overrides: inst.Overrides,
		Capabilities: capabilityStrings(inst.Capabilities),
	}
	if err := rt.Store().CreateRuleWithOrigin(ctx, rule, origin); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetRule(ctx, tenantID, rule.ID)
	if err != nil {
		return nil, internal(err)
	}
	out := toRuleResponse(stored)
	return &out, nil
}

// GetRuleBlueprint reports where a rule came from, and what an upgrade would do.
//
// The diff is computed here rather than at upgrade time so the answer to "is
// there a new version, and what does it change" is available before anyone
// commits to it.
//
//encore:api auth method=GET path=/v1/rules/:id/blueprint
func (s *Service) GetRuleBlueprint(ctx context.Context, id string) (*GetRuleBlueprintResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if _, err := rt.Store().GetRule(ctx, tenantID, id); err != nil {
		return nil, internal(err)
	}
	origin, err := rt.Store().GetBlueprintInstance(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	if origin == nil {
		return &GetRuleBlueprintResponse{RuleID: id,
			Note: "这条规则是手工创建的，不来自模板"}, nil
	}
	return &GetRuleBlueprintResponse{RuleID: id,
		Origin: s.originResponse(origin, nil)}, nil
}

// UpgradeRuleBlueprint moves an instance to another template version.
//
// Nothing about publishing a new template version reaches an existing rule; this
// endpoint is the only path that does, and it refuses when the move would lose a
// choice the customer made.
//
//encore:api auth method=POST path=/v1/rules/:id/blueprint/upgrade
func (s *Service) UpgradeRuleBlueprint(ctx context.Context, id string, req *UpgradeBlueprintRequest) (*UpgradeBlueprintResponse, error) {
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
	origin, err := rt.Store().GetBlueprintInstance(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	if origin == nil {
		return nil, &errs.Error{Code: errs.FailedPrecondition,
			Message: "这条规则不是从模板创建的，没有可升级的模板版本"}
	}

	cur := blueprint.Instance{
		BlueprintKey:      origin.BlueprintKey,
		BlueprintVersion:  origin.BlueprintVersion,
		BlueprintRevision: origin.BlueprintRevision,
		Values:            origin.Params,
		Overrides:         origin.Overrides,
	}
	up := blueprint.PlanUpgrade(cur, req.Version, req.Values)
	resp := &UpgradeBlueprintResponse{RuleID: id, Blocked: up.Blocked, Diff: up.Diff, Issues: up.Issues}
	if up.Instance != nil {
		resp.Issues = append(resp.Issues, s.checkCapabilities(ctx, tenantID, rt, up.Instance.Values["device"], up.Instance.Capabilities)...)
		resp.Blocked = resp.Blocked || blueprint.HasError(resp.Issues)
	}
	if up.Blocked || resp.Blocked || req.DryRun {
		if up.Instance != nil && !resp.Blocked {
			// A dry run still shows the origin as it would look afterwards.
			resp.Origin = originResponseFor(up.Instance)
		}
		return resp, nil
	}

	def := *up.Instance.Definition
	published := &store.Rule{
		ID: id, TenantID: tenantID, Name: rule.Name, Kind: rule.Kind,
		Definition: def, DefinitionHash: domain.DefinitionHash(def),
	}
	next := &store.BlueprintInstance{
		RuleID: id, TenantID: tenantID,
		BlueprintKey: up.Instance.BlueprintKey, BlueprintVersion: up.Instance.BlueprintVersion,
		BlueprintRevision: up.Instance.BlueprintRevision,
		Params:            up.Instance.Values, Overrides: up.Instance.Overrides,
		Capabilities:      capabilityStrings(up.Instance.Capabilities),
	}
	if err := rt.Store().UpgradeRuleWithOrigin(ctx, published, rule.Version, next); err != nil {
		return nil, internal(err)
	}
	stored, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	out := toRuleResponse(stored)
	saved, err := rt.Store().GetBlueprintInstance(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	resp.Applied = true
	resp.Rule = &out
	resp.Origin = s.originResponse(saved, nil)
	return resp, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func toBlueprintResponse(bp *blueprint.Blueprint) BlueprintResponse {
	return BlueprintResponse{
		Key: bp.Key, Version: bp.Version, Revision: bp.Revision,
		Name: bp.Name, Summary: bp.Summary, Guidance: bp.Guidance,
		Params: bp.Params, Requires: bp.Requires,
		Versions: blueprint.Versions(bp.Key),
	}
}

// originResponse renders a stored origin and, when a newer version exists,
// includes the diff and whether the move would be blocked.
func (s *Service) originResponse(in *store.BlueprintInstance, _ *blueprint.Instance) *BlueprintOriginResponse {
	out := &BlueprintOriginResponse{
		RuleID: in.RuleID, BlueprintKey: in.BlueprintKey,
		BlueprintVersion: in.BlueprintVersion, BlueprintRevision: in.BlueprintRevision,
		Params: in.Params, Overrides: in.Overrides, Capabilities: in.Capabilities,
		CreatedAt: in.CreatedAt, UpgradedAt: in.UpgradedAt,
	}
	if latest := blueprint.Latest(in.BlueprintKey); latest != nil {
		out.LatestVersion = latest.Version
		out.UpdateAvailable = latest.Version > in.BlueprintVersion
	}
	if out.UpdateAvailable {
		cur := blueprint.Instance{
			BlueprintKey: in.BlueprintKey, BlueprintVersion: in.BlueprintVersion,
			BlueprintRevision: in.BlueprintRevision, Values: in.Params, Overrides: in.Overrides,
		}
		up := blueprint.PlanUpgrade(cur, out.LatestVersion, nil)
		out.Diff = up.Diff
		out.Blocked = up.Blocked
		out.Issues = up.Issues
	}
	return out
}

func originResponseFor(inst *blueprint.Instance) *BlueprintOriginResponse {
	out := &BlueprintOriginResponse{
		BlueprintKey: inst.BlueprintKey, BlueprintVersion: inst.BlueprintVersion,
		BlueprintRevision: inst.BlueprintRevision,
		Params:            inst.Values, Overrides: inst.Overrides,
		Capabilities:      capabilityStrings(inst.Capabilities),
	}
	if latest := blueprint.Latest(inst.BlueprintKey); latest != nil {
		out.LatestVersion = latest.Version
		out.UpdateAvailable = latest.Version > inst.BlueprintVersion
	}
	return out
}

// checkCapabilities verifies the chosen device exposes the signals the template
// needs.
//
// The community device list does not expose capabilities, so "the list is empty"
// must not be read as "the device lacks this" — that would reject every template
// on every device. It becomes a warning instead, which is the honest answer: we
// could not check.
func (s *Service) checkCapabilities(ctx context.Context, tenantID string, rt *runtime.Runtime, deviceID string, caps []blueprint.Capability) []blueprint.Issue {
	if len(caps) == 0 || strings.TrimSpace(deviceID) == "" {
		return nil
	}
	unverified := func(reason string) []blueprint.Issue {
		return []blueprint.Issue{{
			Code: "capability_unverified", Severity: blueprint.SeverityWarning,
			Message: reason + "，模板仍可创建，但规则可能一直读不到数据",
		}}
	}
	dir := rt.Directory()
	if dir == nil {
		return unverified("设备目录未接入，无法确认所选设备是否上报这些字段")
	}
	devices, err := dir.ByIDs(ctx, tenantID, []string{deviceID})
	if err != nil {
		return unverified("查询设备目录失败（" + err.Error() + "）")
	}
	if len(devices) == 0 {
		return []blueprint.Issue{{
			Code: "device_not_found", Param: "device", Severity: blueprint.SeverityError,
			Message: fmt.Sprintf("设备 %s 不在本租户下", deviceID),
		}}
	}
	exposed := devices[0].Capabilities
	if len(exposed) == 0 {
		return unverified("设备目录没有提供能力清单，无法确认这些字段是否存在")
	}
	var issues []blueprint.Issue
	for _, c := range caps {
		if !exposes(exposed, c) {
			issues = append(issues, blueprint.Issue{
				Code: "capability_missing", Param: c.Key, Severity: blueprint.SeverityError,
				Message: fmt.Sprintf("所选设备没有上报 %s，这个模板在它上面永远不会触发", c.String()),
			})
		}
	}
	return issues
}

// exposes accepts the three shapes a capability list might use, because the
// directory owner is free to spell them and this side must not guess one.
func exposes(list []string, c blueprint.Capability) bool {
	for _, x := range list {
		if x == c.Key || x == c.Kind+":"+c.Key || x == c.Key+"@"+c.Kind {
			return true
		}
	}
	return false
}

func capabilityStrings(caps []blueprint.Capability) []string {
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, c.String())
	}
	return out
}

// blueprintError turns template issues into a 400 that names every problem.
func blueprintError(headline string, issues []blueprint.Issue) error {
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		if i.Severity != blueprint.SeverityError {
			continue
		}
		if i.Param != "" {
			parts = append(parts, i.Param+"："+i.Message)
		} else {
			parts = append(parts, i.Message)
		}
	}
	if len(parts) == 0 {
		return &errs.Error{Code: errs.InvalidArgument, Message: headline}
	}
	return &errs.Error{Code: errs.InvalidArgument, Message: headline + "：" + strings.Join(parts, "；")}
}

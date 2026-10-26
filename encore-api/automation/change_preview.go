package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/bundle"
	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A25: change preview
// ---------------------------------------------------------------------------
//
// A rule edit is not always one rule. A template rule with forty bindings is
// forty devices; a rule that raises an alarm policy touches every episode that
// policy owns. The editor should be able to see that before pressing save — and
// the answer must not be reusable after the world has moved.
//
// Two things make the preview trustworthy rather than decorative:
//
//   - It is computed from the same store reads the save path uses, so "twelve
//     devices" is the number the save will actually affect.
//   - It carries a token that binds the preview to (rule, base version, base
//     revision, new hash, scope digest, expiry). A preview computed against a
//     rule that has since changed cannot be applied, because its token no
//     longer recomputes.

// previewTTL is how long a preview stays usable. Long enough to read the page,
// short enough that "I left the tab open" is not a licence to apply a stale plan.
const previewTTL = 10 * time.Minute

// ChangePreviewRequest carries the body that is about to replace the current one.
type ChangePreviewRequest struct {
	Definition domain.Definition `json:"definition"`
}

// ScopePreview is what the change would touch.
type ScopePreview struct {
	DeviceIDs           []string `json:"deviceIds,omitempty"`
	DeviceCount         int      `json:"deviceCount"`
	TemplateIDs         []string `json:"templateIds,omitempty"`
	TemplateDeviceCount int      `json:"templateDeviceCount"`
	// Truncated says the list was cut for size. The counts are still exact: a
	// page that shows the first 50 and claims "50" is worse than one that says
	// "showing 50 of 214".
	Truncated bool `json:"truncated"`
}

// DependencyPreview is one object the rule points at, and whether it still exists.
type DependencyPreview struct {
	Kind   string `json:"kind"` // alarm_policy | notification_policy | scene
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Exists bool   `json:"exists"`
}

// RunPolicyPreview is the admission and budget contract that would apply.
type RunPolicyPreview struct {
	Mode           string `json:"mode"`
	MaxSteps       int    `json:"maxSteps"`
	MaxRunDuration string `json:"maxRunDuration"`
	// Deadline is the wall-clock moment a run started now would be expired at.
	Deadline string `json:"deadline"`
	// TTL applies to queued and parallel modes only.
	TTL string `json:"ttl,omitempty"`
}

// ChangePreviewResponse is the whole report.
type ChangePreviewResponse struct {
	RuleID       string `json:"ruleId"`
	BaseRevision int    `json:"baseRevision"`
	BaseVersion  int    `json:"baseVersion"`
	HashBefore   string `json:"hashBefore"`
	HashAfter    string `json:"hashAfter"`
	Changed      bool   `json:"changed"`

	Scope        ScopePreview        `json:"scope"`
	Dependencies []DependencyPreview `json:"dependencies,omitempty"`
	RunPolicy    RunPolicyPreview    `json:"runPolicy"`

	// BindingIssues lists bindings whose stored values would not fit the new
	// body. Any entry means the save would leave devices running on values the
	// editor cannot represent.
	BindingIssues []domain.Issue `json:"bindingIssues,omitempty"`
	// RequiresConfirmation is true when the change is wide enough that a plain
	// save should ask first.
	RequiresConfirmation bool `json:"requiresConfirmation"`

	Token     string         `json:"token"`
	ExpiresAt time.Time      `json:"expiresAt"`
	Issues    []domain.Issue `json:"issues,omitempty"`
}

// PreviewRuleChange reports what saving this body would change.
//
//encore:api auth method=POST path=/v1/rules/:id/change-preview
func (s *Service) PreviewRuleChange(ctx context.Context, id string, req *ChangePreviewRequest) (*ChangePreviewResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "需要提供新的定义"}
	}
	existing, err := rt.Store().GetRule(ctx, tenantID, id)
	if err != nil {
		return nil, notFoundOr(err, "规则不存在")
	}
	def := req.Definition
	def.Kind = existing.Kind
	def.Normalize()

	out := &ChangePreviewResponse{
		RuleID:       id,
		BaseRevision: existing.Revision,
		BaseVersion:  existing.Version,
		HashBefore:   bundle.Hash(existing.Definition),
		HashAfter:    bundle.Hash(def),
		Changed:      bundle.Hash(existing.Definition) != bundle.Hash(def),
	}
	out.RunPolicy = runPolicyOf(def, rt.Now())

	if err := domain.Validate(&def); err != nil {
		out.Issues = append(out.Issues, issuesOf(err)...)
		// No token for a body that cannot be saved: a token is a promise that
		// the plan is applicable.
		return out, nil
	}

	scope, serr := s.previewScope(ctx, tenantID, id, def)
	if serr != nil {
		return nil, serr
	}
	out.Scope = scope
	out.Dependencies = s.previewDependencies(ctx, tenantID, def)

	// Bindings are the reason a "small" edit is not small: the stored values are
	// per device, and a renamed or narrowed parameter orphans all of them.
	bindings, berr := rt.Store().ListBindingsForRule(ctx, tenantID, id)
	if berr != nil {
		return nil, internal(berr)
	}
	for _, b := range bindings {
		if !b.Enabled || len(b.Params) == 0 {
			continue
		}
		if _, verr := def.Values(b.Params); verr != nil {
			out.BindingIssues = append(out.BindingIssues, domain.Issue{
				Field: "bindings", Code: "binding_incompatible",
				Message: fmt.Sprintf("%s 上的值无法套用到新版本：%v", scopeLabel(b), verr),
			})
		}
	}
	out.RequiresConfirmation = len(out.BindingIssues) > 0 || scope.DeviceCount+scope.TemplateDeviceCount > 1

	out.ExpiresAt = rt.Now().Add(previewTTL)
	out.Token = previewToken(id, out)
	return out, nil
}

// previewScope expands the rule's targets into concrete devices.
func (s *Service) previewScope(ctx context.Context, tenantID, ruleID string,
	def domain.Definition) (ScopePreview, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return ScopePreview{}, err
	}
	out := ScopePreview{}
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if len(out.DeviceIDs) < 50 {
			out.DeviceIDs = append(out.DeviceIDs, id)
		} else {
			out.Truncated = true
		}
		out.DeviceCount++
	}
	for _, id := range def.Targets.DeviceIDs {
		add(id)
	}
	dir := rt.Directory()
	if dir != nil {
		if len(def.Targets.DeviceConfigIDs) > 0 {
			out.TemplateIDs = append(out.TemplateIDs, def.Targets.DeviceConfigIDs...)
			devices, derr := dir.ByDeviceConfig(ctx, tenantID, def.Targets.DeviceConfigIDs)
			if derr != nil {
				return out, nil // a directory hiccup must not block an editor
			}
			for _, d := range devices {
				add(d.ID)
				out.TemplateDeviceCount++
			}
		}
		// Bindings add devices the definition itself does not name: a template
		// rule with forty bindings affects forty devices while its target list
		// says nothing at all.
		bindings, berr := rt.Store().ListBindingsForRule(ctx, tenantID, ruleID)
		if berr != nil {
			return out, internal(berr)
		}
		for _, b := range bindings {
			if !b.Enabled {
				continue
			}
			if b.DeviceID != "" {
				add(b.DeviceID)
				continue
			}
			if b.DeviceConfigID == "" {
				continue
			}
			if !containsString(out.TemplateIDs, b.DeviceConfigID) {
				out.TemplateIDs = append(out.TemplateIDs, b.DeviceConfigID)
			}
			devices, derr := dir.ByDeviceConfig(ctx, tenantID, []string{b.DeviceConfigID})
			if derr != nil {
				continue
			}
			for _, d := range devices {
				add(d.ID)
			}
		}
	}
	sort.Strings(out.DeviceIDs)
	sort.Strings(out.TemplateIDs)
	return out, nil
}

// previewDependencies resolves every object the body points at.
func (s *Service) previewDependencies(ctx context.Context, tenantID string,
	def domain.Definition) []DependencyPreview {

	rt, err := s.rtOrErr()
	if err != nil {
		return nil
	}
	var out []DependencyPreview
	seen := map[string]bool{}
	check := func(kind, id string) {
		if id == "" || seen[kind+"|"+id] {
			return
		}
		seen[kind+"|"+id] = true
		row := DependencyPreview{Kind: kind, ID: id}
		if rule, gerr := rt.Store().GetRule(ctx, tenantID, id); gerr == nil {
			row.Name = rule.Name
			row.Exists = true
		}
		out = append(out, row)
	}
	for _, e := range bundle.DefinitionRefs(def) {
		kind := "alarm_policy"
		if e.Field == "scene" {
			kind = "scene"
		}
		check(kind, e.To)
	}
	for _, key := range bundle.DefinitionSlots(def) {
		check("notification_policy", key)
	}
	return out
}

// runPolicyOf is the admission contract a run started now would get.
//
// It is derived from the same definition the runtime reads, so "mode=queued,
// deadline in 10 minutes" is not a second description of the policy — it is the
// policy.
func runPolicyOf(def domain.Definition, now time.Time) RunPolicyPreview {
	b := def.Budget
	maxDur := b.MaxRunDuration.Std()
	out := RunPolicyPreview{
		Mode:           string(def.Mode),
		MaxSteps:       b.MaxSteps,
		MaxRunDuration: maxDur.String(),
		Deadline:       now.Add(maxDur).UTC().Format(time.RFC3339),
	}
	if def.Queue != nil && def.Queue.TTL.Std() > 0 {
		out.TTL = def.Queue.TTL.Std().String()
	}
	if def.Parallel != nil && def.Parallel.TTL.Std() > 0 {
		out.TTL = def.Parallel.TTL.Std().String()
	}
	return out
}

// previewToken binds a preview to the exact world it was computed in.
//
// It is a hash, not a stored row: nothing to clean up, nothing to expire, and no
// way for a preview to be "renewed" without recomputing the scope it describes.
// Any of these changing makes the token fail to recompute:
//
//   - the rule's version (somebody saved in between),
//   - the new body's hash (the editor changed the form),
//   - the affected scope (devices were added to the template),
//   - the dependency set (an alarm policy was deleted).
//
// The expiry is carried in the token rather than folded into the hash, because
// a preview computed at 10:00:01 and re-checked at 10:00:02 would otherwise
// produce two different tokens for the same world — and every save would be
// refused as stale. That is not a theoretical bug: the in-process tests never
// saw it because they run on a frozen clock, and the CLI hit it on the first
// real save.
//
// The expiry is therefore *checked*, not *protected*: the token is a "did you
// actually look at the impact?" guard, not an authorisation. Anybody who wants
// to skip it can simply not send one — the save path runs its own binding and
// version checks either way.
func previewToken(ruleID string, out *ChangePreviewResponse) string {
	expiry := out.ExpiresAt.Unix()
	sum := previewDigest(ruleID, out, expiry)
	return fmt.Sprintf("pv1:%d:%s", expiry, sum)
}

// previewDigest is the deterministic half: the same world always produces the
// same digest.
func previewDigest(ruleID string, out *ChangePreviewResponse, expiry int64) string {
	scope := append([]string{}, out.Scope.DeviceIDs...)
	sort.Strings(scope)
	deps := make([]string, 0, len(out.Dependencies))
	for _, d := range out.Dependencies {
		deps = append(deps, fmt.Sprintf("%s|%s|%t", d.Kind, d.ID, d.Exists))
	}
	sort.Strings(deps)
	body, _ := json.Marshal(struct {
		RuleID  string   `json:"ruleId"`
		Version int      `json:"version"`
		Before  string   `json:"before"`
		After   string   `json:"after"`
		Scope   []string `json:"scope"`
		Deps    []string `json:"deps"`
		Expiry  int64    `json:"expiry"`
	}{
		RuleID: ruleID, Version: out.BaseVersion, Before: out.HashBefore,
		After: out.HashAfter, Scope: scope, Deps: deps, Expiry: expiry,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:16])
}

// parsePreviewToken reads back the expiry a token was issued with.
func parsePreviewToken(tok string) (expiry int64, digest string, ok bool) {
	parts := strings.Split(tok, ":")
	if len(parts) != 3 || parts[0] != "pv1" {
		return 0, "", false
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return n, parts[2], true
}

// checkPreviewToken refuses a save whose preview no longer describes the world.
func (s *Service) checkPreviewToken(ctx context.Context, tenantID, ruleID string,
	req *UpdateRuleRequest, existing *store.Rule) error {

	if req.PreviewToken == "" {
		return nil // the legacy edit path does not send one
	}
	expiry, digest, ok := parsePreviewToken(req.PreviewToken)
	if !ok {
		return detailError(errs.InvalidArgument,
			"预览令牌格式不对，请重新预览", ErrorDetail{Code: "preview_invalid"})
	}
	// Recompute the preview the caller claims to have seen, using the expiry
	// the token was issued with, so the comparison is about the world and not
	// about when we happened to look.
	again, err := s.PreviewRuleChange(ctx, ruleID, &ChangePreviewRequest{Definition: req.Definition})
	if err != nil {
		return err
	}
	if again.BaseVersion != existing.Version || again.HashAfter == "" {
		return detailError(errs.FailedPrecondition,
			"规则在预览之后被改过，请重新预览再保存", ErrorDetail{Code: "preview_stale"})
	}
	if previewDigest(ruleID, again, expiry) != digest {
		return detailError(errs.FailedPrecondition,
			"预览与当前内容不再一致，请重新预览再保存", ErrorDetail{Code: "preview_stale"})
	}
	if s.nowForPreview().After(time.Unix(expiry, 0)) {
		return detailError(errs.FailedPrecondition,
			"预览已过期，请重新预览再保存", ErrorDetail{Code: "preview_expired"})
	}
	return nil
}

// nowForPreview is the clock the expiry check uses. It is a method so tests can
// pin it; in production it is the runtime's clock, which is the same clock that
// stamped the expiry.
func (s *Service) nowForPreview() time.Time {
	if rt, err := s.rtOrErr(); err == nil {
		return rt.Now()
	}
	return time.Now()
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A20: references, invalidation and tombstones
// ---------------------------------------------------------------------------
//
// A19 made "which rule applies to which device" a queryable row. That created
// the next problem: the row can outlive the thing it points at. A device gets
// deleted, a rule gets deleted while a scene still activates it, a template
// upgrade changes the body under a pinned binding.
//
// The rule for all of it: **configuration deletion is not the same as recalling
// a physical action**. These endpoints stop what will happen next and keep what
// already happened explainable. They never rewrite history and never claim a
// command was undone.

// OrphanBindingResponse is one binding whose device no longer exists.
type OrphanBindingResponse struct {
	BindingID string `json:"binding_id"`
	RuleID    string `json:"rule_id"`
	RuleName  string `json:"rule_name,omitempty"`
	DeviceID  string `json:"device_id"`
	Reason    string `json:"reason"`
	Detail    string `json:"detail"`
	Detached  bool   `json:"detached"`
}

// GhostPendingResponse is one pending window that can never be decided
// meaningfully.
type GhostPendingResponse struct {
	DeviceID string    `json:"device_id"`
	RuleID   string    `json:"rule_id"`
	RuleName string    `json:"rule_name,omitempty"`
	Deadline time.Time `json:"deadline"`
	// Reason is "device_missing", "rule_missing" or "binding_gone". The last
	// one is subtler: the device exists, but the values the window was opened
	// with no longer have a source, so the deadline it is waiting for
	// describes a rule that is no longer the rule.
	Reason         string `json:"reason"`
	Detail         string `json:"detail"`
	PendingCleared bool   `json:"pending_cleared"`
}

// BindingReconcileResponse is the reconciliation report.
type BindingReconcileResponse struct {
	CheckedBindings int                     `json:"checked_bindings"`
	CheckedPending  int                     `json:"checked_pending"`
	Orphans         []OrphanBindingResponse `json:"orphans,omitempty"`
	Ghosts          []GhostPendingResponse  `json:"ghosts,omitempty"`
	Detached        int                     `json:"detached"`
	PendingCleared  int                     `json:"pending_cleared"`
	Applied         bool                    `json:"applied"`
	Issues          []domain.Issue          `json:"issues,omitempty"`
}

// ReconcileBindingsRequest asks for a dry run by default.
type ReconcileBindingsRequest struct {
	// Apply actually detaches orphans and clears ghost windows. The default is
	// a report, because "clean up my references" is not something that should
	// happen because somebody opened a page.
	Apply bool `json:"apply"`
}

// ReconcileBindingRefs finds bindings and pending windows that can no longer
// resolve, and optionally stops them.
//
// It is deliberately a separate, explicit call rather than a background sweep
// that silently deletes rows: an operator should see "12 bindings pointed at
// devices that no longer exist" before those bindings disappear.
//
//encore:api auth method=POST path=/v1/binding-reconcile
func (s *Service) ReconcileBindingRefs(ctx context.Context, req *ReconcileBindingsRequest) (*BindingReconcileResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &ReconcileBindingsRequest{}
	}
	out := &BindingReconcileResponse{Applied: req.Apply}

	boundIDs, err := rt.Store().DeviceScopedBindingIDs(ctx, tenantID, 1000)
	if err != nil {
		return nil, internal(err)
	}
	pendings, err := rt.Store().PendingRefs(ctx, tenantID, 1000)
	if err != nil {
		return nil, internal(err)
	}
	out.CheckedBindings = len(boundIDs)
	out.CheckedPending = len(pendings)

	// One directory call for every id we need to know about, not one per row.
	want := map[string]bool{}
	for _, id := range boundIDs {
		want[id] = true
	}
	for _, p := range pendings {
		want[p.DeviceID] = true
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}

	known := map[string]bool{}
	configOf := map[string]string{}
	if len(ids) > 0 {
		if rt.Directory() == nil {
			// Same reasoning as the error case below: with no directory at all
			// every device looks missing. Refusing to guess is the only safe
			// answer.
			out.Issues = append(out.Issues, domain.Issue{
				Field: "device_id", Code: "directory_unavailable",
				Message: "设备目录未接入，无法判断哪些绑定已失效；本次未做任何变更",
			})
			return out, nil
		}
		devices, derr := rt.Directory().ByIDs(ctx, tenantID, ids)
		if derr != nil {
			// Without the directory we cannot tell "deleted" from "temporarily
			// unreachable". Reporting everything as an orphan would be a
			// destructive guess, so the call says so and does nothing.
			out.Issues = append(out.Issues, domain.Issue{
				Field: "device_id", Code: "directory_unavailable",
				Message: "设备目录暂时不可用，无法判断哪些绑定已失效；本次未做任何变更",
			})
			return out, nil
		}
		for _, d := range devices {
			known[d.ID] = true
			configOf[d.ID] = d.DeviceConfigID
		}
	}

	orphans, err := rt.Store().ReconcileBindings(ctx, tenantID, known)
	if err != nil {
		return nil, internal(err)
	}
	for _, o := range orphans {
		row := OrphanBindingResponse{
			BindingID: o.Binding.ID, RuleID: o.Binding.RuleID, DeviceID: o.Binding.DeviceID,
			Reason: o.Reason, Detail: o.Detail,
		}
		if rule, rerr := rt.Store().GetRule(ctx, tenantID, o.Binding.RuleID); rerr == nil {
			row.RuleName = rule.Name
		}
		if req.Apply {
			if derr := rt.Store().DetachBinding(ctx, tenantID, o.Binding.ID); derr == nil ||
				errors.Is(derr, store.ErrAlreadyDone) {
				row.Detached = true
				out.Detached++
			}
		}
		out.Orphans = append(out.Orphans, row)
	}

	// Pending windows: a device that is gone, a rule that is gone, or a rule
	// whose parameter values no longer have a binding for this device.
	bindingCache := map[string][]*store.Binding{}
	for _, p := range pendings {
		reason, detail := "", ""
		switch {
		case !known[p.DeviceID]:
			reason = "device_missing"
			detail = "设备已不存在，这个等待窗到期后不会产生任何有意义的动作"
		default:
			rule, rerr := rt.Store().GetRule(ctx, tenantID, p.RuleID)
			if rerr != nil {
				reason = "rule_missing"
				detail = "规则已不存在，这个等待窗不会再被判定"
				break
			}
			if !rule.Definition.HasParams() {
				break
			}
			// A parameterized rule with no binding for this device falls back
			// to defaults — which is exactly what a template change looks like
			// from here. The window was opened under values that no longer
			// apply, so waiting for its deadline would decide a rule the
			// customer did not configure.
			bindings, ok := bindingCache[p.DeviceID]
			if !ok {
				bindings, err = rt.Store().BindingsForDevice(ctx, tenantID, p.DeviceID, configOf[p.DeviceID])
				if err != nil {
					return nil, internal(err)
				}
				bindingCache[p.DeviceID] = bindings
			}
			if !hasBindingFor(bindings, p.RuleID) {
				reason = "binding_gone"
				detail = "这个等待窗是按模板/设备绑定的参数打开的，现在绑定已不存在；到期判定会用默认值，结论不再可信"
			}
		}
		if reason == "" {
			continue
		}
		g := GhostPendingResponse{
			DeviceID: p.DeviceID, RuleID: p.RuleID, Deadline: p.Deadline,
			Reason: reason, Detail: detail,
		}
		if rule, rerr := rt.Store().GetRule(ctx, tenantID, p.RuleID); rerr == nil {
			g.RuleName = rule.Name
		}
		if req.Apply {
			// Only this rule's window is cleared. Clearing the whole device
			// would silently drop deadlines belonging to rules that are still
			// perfectly resolvable.
			if n, cerr := rt.Store().ClearPendingOne(ctx, tenantID, p.DeviceID, p.RuleID); cerr == nil {
				out.PendingCleared += n
				g.PendingCleared = true
			}
		}
		out.Ghosts = append(out.Ghosts, g)
	}
	return out, nil
}

// DeleteRuleRequest carries the override for the reference pre-check.
type DeleteRuleRequest struct {
	// Force deletes even though other rules still reference this one. It is a
	// query parameter so that the safe path stays the default: an operator has
	// to say out loud that they accept a broken reference.
	Force bool `json:"force"`
}

// DeleteRuleResponse reports what the delete actually stopped.
type DeleteRuleResponse struct {
	RuleID           string `json:"rule_id"`
	RuleName         string `json:"rule_name"`
	DetachedBindings int    `json:"detached_bindings"`
	ClearedPending   int    `json:"cleared_pending"`
	// Forced is true when the caller overrode a live reference. The response
	// says so explicitly so the caller's audit log can record it.
	Forced bool `json:"forced"`
}

// DeleteRule removes a rule, after checking that nothing still points at it.
//
// The pre-check is the difference between "deleted" and "silently broken": a
// scene that activates a deleted rule would otherwise keep a step that can
// never succeed, and the run log would blame the device.
//
// Deleting stops what would happen next — bindings are detached, pending
// windows are cleared, schedules are cancelled — and keeps everything that
// already happened: runs keep their pinned revision, and the rule row survives
// as a tombstone with a delete_reason, so "why did this stop" has an answer.
//
//encore:api auth method=DELETE path=/v1/rules/:id
func (s *Service) DeleteRule(ctx context.Context, id string, req *DeleteRuleRequest) (*DeleteRuleResponse, error) {
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
		return nil, notFoundOr(err, "规则不存在")
	}
	force := req != nil && req.Force

	refs, err := rt.Store().ReferencingRules(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	if len(refs) > 0 && !force {
		names := make([]string, 0, len(refs))
		ids := make([]string, 0, len(refs))
		for _, r := range refs {
			names = append(names, r.Name)
			ids = append(ids, r.ID)
		}
		return nil, detailError(errs.FailedPrecondition,
			fmt.Sprintf("还有 %d 条规则引用了它（%v）；先解除引用再删除，否则那些步骤会永远失败",
				len(refs), names),
			ErrorDetail{Code: "referenced_by_rules", ReferencingRuleIDs: ids})
	}

	detached, err := rt.Store().DetachBindingsForRule(ctx, tenantID, id, "rule_deleted")
	if err != nil {
		return nil, internal(err)
	}
	cleared, err := rt.Store().ClearPendingForRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	if err := rt.Store().DeleteRuleTombstone(ctx, tenantID, id, deleteReason(force, len(refs))); err != nil {
		return nil, internal(err)
	}
	// A deleted rule must not leave a schedule that still fires. The due time is
	// cleared here as well, so the scan cannot pick it up even before the
	// soft-deleted rule is filtered out of the join.
	_ = rt.Store().CancelSchedulesForRule(ctx, id)
	rt.InvalidateBindings(tenantID, "")
	return &DeleteRuleResponse{
		RuleID: id, RuleName: existing.Name,
		DetachedBindings: detached, ClearedPending: cleared,
		Forced: force && len(refs) > 0,
	}, nil
}

// deleteReason writes down why the tombstone exists, including the case where
// somebody chose to leave a broken reference behind.
func deleteReason(force bool, refs int) string {
	if force && refs > 0 {
		return fmt.Sprintf("deleted_by_user_forced_with_%d_references", refs)
	}
	return "deleted_by_user"
}

// CheckRuleRevision reports whether a new body can be applied without breaking
// the bindings that already exist.
//
// Bindings pin the revision they were authored against, so editing a rule does
// not silently change what a device runs. This endpoint is how an editor finds
// out, before saving, that the new body renames a parameter three devices were
// using.
//
//encore:api auth method=POST path=/v1/rules/:id/revision-check
func (s *Service) CheckRuleRevision(ctx context.Context, id string, req *RevisionCheckRequest) (*RevisionCheckResponse, error) {
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
	out := &RevisionCheckResponse{}
	if err := domain.Validate(&def); err != nil {
		out.Issues = append(out.Issues, issuesOf(err)...)
		return out, nil
	}
	bindings, err := rt.Store().ListBindingsForRule(ctx, tenantID, id)
	if err != nil {
		return nil, internal(err)
	}
	for _, b := range bindings {
		if !b.Enabled {
			continue
		}
		out.CheckedBindings++
		if len(b.Params) == 0 {
			continue
		}
		if _, verr := def.Values(b.Params); verr != nil {
			out.Blocked = true
			out.Issues = append(out.Issues, domain.Issue{
				Field: "bindings", Code: "binding_incompatible",
				Message: fmt.Sprintf("绑定 %s 的值无法套用到新版本：%v", scopeLabel(b), verr),
			})
		}
	}
	out.Compatible = !out.Blocked
	return out, nil
}

// RevisionCheckRequest carries the body that is about to become a revision.
type RevisionCheckRequest struct {
	Definition domain.Definition `json:"definition"`
}

// RevisionCheckResponse is the compatibility report.
type RevisionCheckResponse struct {
	Compatible      bool           `json:"compatible"`
	Blocked         bool           `json:"blocked"`
	CheckedBindings int            `json:"checked_bindings"`
	Issues          []domain.Issue `json:"issues,omitempty"`
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hasBindingFor(bindings []*store.Binding, ruleID string) bool {
	for _, b := range bindings {
		if b.RuleID == ruleID && b.Enabled {
			return true
		}
	}
	return false
}

func scopeLabel(b *store.Binding) string {
	if b.DeviceID != "" {
		return "设备 " + b.DeviceID
	}
	return "模板 " + b.DeviceConfigID
}

func joinIDs(rules []*store.Rule) string {
	out := ""
	for i, r := range rules {
		if i > 0 {
			out += ","
		}
		out += r.ID
	}
	return out
}

// issuesOf flattens a validation failure into field-level issues, keeping the
// machine-readable codes the editor switches on.
func issuesOf(err error) []domain.Issue {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Issues
	}
	var pe *domain.ParamError
	if errors.As(err, &pe) {
		return []domain.Issue{{Field: "params." + pe.Param, Code: pe.Code, Message: pe.Message}}
	}
	return []domain.Issue{{Field: "definition", Code: "invalid_definition", Message: err.Error()}}
}

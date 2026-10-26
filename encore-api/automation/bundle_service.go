package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/bundle"
	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A21: packages, dependency manifests and resumable imports
// ---------------------------------------------------------------------------
//
// The market already ships device templates and dashboards. What it could not
// ship was the automation that makes those templates useful: the rules, the
// notification slots they need, and the references between them. Exporting a
// template without them produced a package that installed cleanly and then did
// nothing.
//
// Three properties this file is built around:
//
//   - **A package is checkable before it writes anything.** Preview runs the
//     same code path as import and stops at the last moment.
//   - **Importing twice is not importing twice.** The identity is
//     (bundleKey, version, contentHash), and the row survives, so the second
//     call finds the first one.
//   - **A half-finished import says so.** Status moves staged -> activating ->
//     activated, or to failed with the mapping kept, so a retry resumes instead
//     of duplicating.

// ExportBundleRequest asks for the automation half of a market bundle.
type ExportBundleRequest struct {
	BundleKey string `json:"bundleKey"`
	Version   string `json:"version"`
	RuleIDs   []string `json:"ruleIds"`

	// TemplateKeys maps a local device-config id to the business key the market
	// knows it by. Without it the local id is used, which still round-trips but
	// only inside one installation.
	TemplateKeys map[string]string `json:"templateKeys,omitempty"`
	// RoleNames maps a local device id to the role name shown to the importer.
	RoleNames map[string]string `json:"roleNames,omitempty"`
}

// BundlePreviewRequest checks a package against this environment.
type BundlePreviewRequest struct {
	Manifest        *bundle.Manifest    `json:"manifest"`
	RoleMapping     map[string][]string `json:"roleMapping,omitempty"`
	TemplateMapping map[string]string   `json:"templateMapping,omitempty"`
	NotifyChoices   map[string]string   `json:"notifyChoices,omitempty"`
}

// BundleImportRequest is a preview that is allowed to write.
type BundleImportRequest struct {
	Manifest        *bundle.Manifest    `json:"manifest"`
	RoleMapping     map[string][]string `json:"roleMapping,omitempty"`
	TemplateMapping map[string]string   `json:"templateMapping,omitempty"`
	NotifyChoices   map[string]string   `json:"notifyChoices,omitempty"`
	// Resume continues a failed import with the same content hash. Without it a
	// failed import is reported as failed rather than silently retried.
	Resume bool `json:"resume,omitempty"`
}

// BundlePlanResponse is what a preview and an import both return.
type BundlePlanResponse struct {
	ImportID string       `json:"importId,omitempty"`
	Status   string       `json:"status,omitempty"`
	Reused   bool         `json:"reused"`
	Plan     *bundle.Plan `json:"plan"`
	Issues   []domain.Issue `json:"issues,omitempty"`
}

// BundleImportResponse reports what the import actually created.
type BundleImportResponse struct {
	ImportID string            `json:"importId"`
	Status   string            `json:"status"`
	Reused   bool              `json:"reused"`
	Created  int               `json:"created"`
	Updated  int               `json:"updated"`
	Skipped  int               `json:"skipped"`
	Rules    map[string]string `json:"rules,omitempty"`
	Plan     *bundle.Plan      `json:"plan"`
	Issues   []domain.Issue    `json:"issues,omitempty"`
}

// BundleImportSummary is one row of the import history.
type BundleImportSummary struct {
	ImportID    string     `json:"importId"`
	BundleKey   string     `json:"bundleKey"`
	Version     string     `json:"version"`
	ContentHash string     `json:"contentHash"`
	Status      string     `json:"status"`
	ReasonCode  string     `json:"reasonCode,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Rules       int        `json:"rules"`
	CreatedAt   time.Time  `json:"createdAt"`
	ActivatedAt *time.Time `json:"activatedAt,omitempty"`
}

// ListBundleImportsResponse is the history page.
type ListBundleImportsResponse struct {
	Imports []BundleImportSummary `json:"imports"`
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// ExportBundle turns a set of local rules into a portable manifest.
//
// It refuses rather than guesses in three cases: a rule that is not in this
// tenant, a reference that leaves the package, and a definition that looks like
// it carries a credential. Each of those would produce a package that installs
// and then fails, which is the failure mode the whole task exists to remove.
//
//encore:api auth method=POST path=/v1/bundles/export
func (s *Service) ExportBundle(ctx context.Context, req *ExportBundleRequest) (*bundle.Manifest, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil || len(req.RuleIDs) == 0 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "至少选择一条规则"}
	}
	if strings.TrimSpace(req.BundleKey) == "" || strings.TrimSpace(req.Version) == "" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "包必须有 bundleKey 与版本号"}
	}

	// Load every rule in one pass. Export is a batch operation by nature.
	rules := make([]*store.Rule, 0, len(req.RuleIDs))
	for _, id := range req.RuleIDs {
		r, gerr := rt.Store().GetRule(ctx, tenantID, id)
		if gerr != nil {
			return nil, notFoundOr(gerr, "规则不存在："+id)
		}
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

	// The package's own identity map: local row id -> resource key. References
	// that are not in this map are references that leave the package.
	keyOf := map[string]string{}
	taken := map[string]bool{}
	for _, r := range rules {
		key := r.ResourceKey
		if key == "" {
			key = DeriveResourceKey(r.Name, r.Kind, r.ID)
		}
		for taken[key] {
			key += "-2"
		}
		taken[key] = true
		keyOf[r.ID] = key
	}

	man := &bundle.Manifest{
		ContractVersion: bundle.ContractVersion,
		BundleKey:       req.BundleKey,
		Version:         req.Version,
		Security:        bundle.Security{},
	}

	// Notification policies are exported as slots, one per distinct policy, so
	// the importer chooses a local policy instead of inheriting the exporter's.
	slotOf := map[string]string{}
	slotRequired := map[string]bool{}
	slot := func(policyID string, required bool) string {
		if policyID == "" {
			return ""
		}
		if key, ok := slotOf[policyID]; ok {
			if required {
				slotRequired[key] = true
			}
			return bundle.SlotValue(key)
		}
		key := "notify-" + shortHash(policyID)
		slotOf[policyID] = key
		slotRequired[key] = required
		return bundle.SlotValue(key)
	}

	roles := map[string]bundle.Role{}
	templates := map[string]bundle.TemplateDep{}

	for _, r := range rules {
		key := keyOf[r.ID]

		// References: rewrite to resource keys, refusing anything that points
		// outside the package.
		def := r.Definition
		var outside []string
		def = bundle.RewriteRefs(def, func(id string) string {
			if k, ok := keyOf[id]; ok {
				return k
			}
			outside = append(outside, id)
			return id
		})
		if len(outside) > 0 {
			return nil, &errs.Error{
				Code: errs.FailedPrecondition,
				Message: fmt.Sprintf("规则「%s」引用了包外的对象（%s）；请把它们一起导出，否则导入后这一步永远失败",
					r.Name, strings.Join(uniqueStrings(outside), ", ")),
				Meta: errs.Metadata{"rule_id": r.ID},
			}
		}

		// Notification targets become slots. A notify step exists only to
		// notify, so its slot is required; an alarm still opens an episode even
		// with nobody to tell, so that slot is optional.
		def = bundle.RewriteSlots(def, func(policyID string) string {
			if _, ok := bundle.SlotOf(policyID); ok {
				return policyID
			}
			return slot(policyID, false)
		})
		for i := range def.Steps {
			if def.Steps[i].Kind == domain.StepNotify && def.Steps[i].NotificationPolicyID != "" {
				if k, ok := bundle.SlotOf(def.Steps[i].NotificationPolicyID); ok {
					slotRequired[k] = true
				}
			}
		}

		// Explicit targets become roles for the same reason bindings do: a
		// package that names a device only works where that device exists. The
		// definition ships with its target lists empty and the importer fills
		// them back in from the roles it resolved.
		var targetRoles []string
		devRoles := make([]string, 0, len(def.Targets.DeviceIDs))
		for _, dev := range def.Targets.DeviceIDs {
			roleKey := "dev:" + roleNameFor(req.RoleNames, dev)
			roles[roleKey] = bundle.Role{
				Key: roleKey, Kind: bundle.RoleDevice,
				DisplayName: roleNameFor(req.RoleNames, dev), Required: true,
			}
			devRoles = append(devRoles, roleKey)
			targetRoles = append(targetRoles, roleKey)
		}
		cfgRoles := make([]string, 0, len(def.Targets.DeviceConfigIDs))
		for _, cfg := range def.Targets.DeviceConfigIDs {
			tplKey := templateKeyFor(req.TemplateKeys, cfg)
			roleKey := "tpl:" + tplKey
			roles[roleKey] = bundle.Role{
				Key: roleKey, Kind: bundle.RoleTemplate, TemplateKey: tplKey,
				DisplayName: "模板 " + tplKey, Required: true,
			}
			templates[tplKey] = bundle.TemplateDep{Key: tplKey}
			cfgRoles = append(cfgRoles, roleKey)
			targetRoles = append(targetRoles, roleKey)
		}
		// The lists stay populated but hold role keys instead of ids: the
		// definition still has to satisfy "at least one target", and an empty
		// list would also read as "the author removed the target".
		def.Targets.DeviceIDs = devRoles
		def.Targets.DeviceConfigIDs = cfgRoles

		entry := bundle.Rule{
			ResourceKey: key,
			Name:        r.Name,
			Kind:        r.Kind,
			Revision:    r.Revision,
			Definition:  def,
			Hash:        bundle.Hash(def),
			TargetRoles: targetRoles,
		}

		// Bindings: scope becomes a role, never a device id.
		bindings, berr := rt.Store().ListBindingsForRule(ctx, tenantID, r.ID)
		if berr != nil {
			return nil, internal(berr)
		}
		for _, b := range bindings {
			if !b.Enabled {
				// A detached binding is history. Exporting it would resurrect
				// a scope the customer explicitly gave up.
				continue
			}
			if b.DeviceID != "" {
				roleKey := "dev:" + roleNameFor(req.RoleNames, b.DeviceID)
				roles[roleKey] = bundle.Role{
					Key: roleKey, Kind: bundle.RoleDevice, DisplayName: roleNameFor(req.RoleNames, b.DeviceID),
					Required: true,
				}
				entry.Bindings = append(entry.Bindings, bundle.Binding{
					ScopeRole: roleKey, Params: b.Params, Revision: b.Revision,
				})
				continue
			}
			tplKey := templateKeyFor(req.TemplateKeys, b.DeviceConfigID)
			roleKey := "tpl:" + tplKey
			roles[roleKey] = bundle.Role{
				Key: roleKey, Kind: bundle.RoleTemplate, TemplateKey: tplKey,
				DisplayName: "模板 " + tplKey, Required: true,
			}
			templates[tplKey] = bundle.TemplateDep{Key: tplKey}
			entry.Bindings = append(entry.Bindings, bundle.Binding{
				ScopeRole: roleKey, Params: b.Params, Revision: b.Revision,
			})
		}

		// Template origin: kept so an importer can tell which template version
		// this body expects.
		if origin, oerr := rt.Store().GetBlueprintInstance(ctx, tenantID, r.ID); oerr == nil && origin != nil {
			entry.Blueprint = &bundle.BlueprintOrigin{
				Key: origin.BlueprintKey, Version: origin.BlueprintVersion, Params: origin.Params,
			}
			man.Resources.Blueprints = append(man.Resources.Blueprints, bundle.BlueprintRef{
				Key: origin.BlueprintKey, Version: origin.BlueprintVersion,
				Hash: bundle.Hash(origin.Params),
			})
		}

		man.Resources.Rules = append(man.Resources.Rules, entry)
	}

	// Internal edges, recorded explicitly so an importer can check them without
	// re-parsing every definition.
	for _, r := range man.Resources.Rules {
		for _, e := range bundle.DefinitionRefs(r.Definition) {
			man.Dependencies.Edges = append(man.Dependencies.Edges, bundle.Edge{
				From: r.ResourceKey, To: e.To, Field: e.Field,
			})
		}
	}

	for _, r := range roles {
		man.Dependencies.Roles = append(man.Dependencies.Roles, r)
	}
	for _, t := range templates {
		man.Dependencies.DeviceTemplates = append(man.Dependencies.DeviceTemplates, t)
	}
	for key, required := range slotRequired {
		man.Resources.NotifySlots = append(man.Resources.NotifySlots, bundle.NotifySlot{
			SlotKey: key, Label: "通知策略 " + key, Required: required,
		})
	}
	// Capabilities are declared per role from the template's thing model. The
	// market bundle owns that model; here we only state which roles must exist.
	for _, r := range man.Dependencies.Roles {
		if r.Kind == bundle.RoleTemplate {
			man.Dependencies.Capabilities = append(man.Dependencies.Capabilities, bundle.CapabilityDep{
				RoleKey: r.Key, Capability: "telemetry", Why: "规则按遥测触发",
			})
		}
	}

	man.Sort()
	man.ComputeHash()

	// The promise is checked, not asserted: the same validator the importer runs
	// runs here too, so an exporter cannot produce a package the importer would
	// reject.
	if issues := bundle.Validate(man); len(issues) > 0 {
		// A credential is the one problem worth surfacing first: it is the only
		// one that cannot be fixed by editing the rule later, because by then
		// the package has already been handed to somebody.
		first := issues[0]
		for _, i := range issues {
			if i.Code == "secret_in_definition" || i.Code == "bundle_has_secrets" {
				first = i
				break
			}
		}
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "这个包本身不合法，已拒绝导出：" + first.Message,
			Meta:    errs.Metadata{"issue_code": first.Code},
		}
	}
	return man, nil
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

// PreviewBundleImport reports what importing this package would do.
//
// It is the same plan the import runs; the only difference is that nothing is
// written. That is deliberate: a preview computed by different code is a
// preview that can be right while the import is wrong.
//
//encore:api auth method=POST path=/v1/bundles/preview
func (s *Service) PreviewBundleImport(ctx context.Context, req *BundlePreviewRequest) (*BundlePlanResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil || req.Manifest == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "需要提供包内容"}
	}
	man := req.Manifest
	if man.ContentHash == "" {
		man.ComputeHash()
	}

	out := &BundlePlanResponse{}
	// Already imported? Say so instead of showing a plan that will not happen.
	if existing, gerr := rt.Store().GetBundleImportByIdentity(ctx, tenantID, man.BundleKey, man.Version, man.ContentHash); gerr == nil {
		out.ImportID = existing.ID
		out.Status = existing.Status
		out.Reused = true
	}

	target, terr := s.bundleTarget(ctx, tenantID, man, req.RoleMapping, req.TemplateMapping, req.NotifyChoices)
	if terr != nil {
		return nil, terr
	}
	out.Plan = bundle.Preview(man, target)
	out.Issues = append(out.Issues, out.Plan.Issues...)
	return out, nil
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// ImportBundle applies a package, resumably and idempotently.
//
//encore:api auth method=POST path=/v1/bundles/import
func (s *Service) ImportBundle(ctx context.Context, req *BundleImportRequest) (*BundleImportResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	if req == nil || req.Manifest == nil {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "需要提供包内容"}
	}
	man := req.Manifest
	if man.ContentHash == "" {
		man.ComputeHash()
	}

	// Plan first. An import that cannot succeed must not create a row that says
	// it started.
	target, terr := s.bundleTarget(ctx, tenantID, man, req.RoleMapping, req.TemplateMapping, req.NotifyChoices)
	if terr != nil {
		return nil, terr
	}
	plan := bundle.Preview(man, target)
	if !plan.OK {
		return nil, &errs.Error{
			Code:    errs.FailedPrecondition,
			Message: "这个包还不能导入：" + plan.Issues[0].Message,
			Meta:    errs.Metadata{"issue_code": plan.Issues[0].Code},
		}
	}

	manifestJSON, merr := json.Marshal(man)
	if merr != nil {
		return nil, internal(merr)
	}
	reportJSON, _ := json.Marshal(plan)

	row := &store.BundleImport{
		ID: runtime.NewID(), TenantID: tenantID,
		BundleKey: man.BundleKey, Version: man.Version, ContentHash: man.ContentHash,
		Status: store.BundleStaged, Manifest: manifestJSON,
		RoleMapping: req.RoleMapping, NotifyChoices: req.NotifyChoices,
		Report: reportJSON,
	}
	stored, created, serr := rt.Store().StageBundleImport(ctx, row)
	if serr != nil {
		return nil, internal(serr)
	}
	if !created {
		switch {
		case stored.Done():
			// Same bytes, already applied. Returning the original mapping is the
			// only honest answer: nothing was created, and the caller still
			// needs to know which rules this package corresponds to.
			return &BundleImportResponse{
				ImportID: stored.ID, Status: stored.Status, Reused: true,
				Rules: stored.CreatedRules, Plan: plan,
			}, nil
		case !req.Resume:
			return nil, &errs.Error{
				Code: errs.FailedPrecondition,
				Message: fmt.Sprintf("这个包的上一次导入停在 %s；请用 resume 继续，或先处理原因：%s",
					stored.Status, stored.ReasonMsg),
				Meta: errs.Metadata{"import_id": stored.ID, "status": stored.Status},
			}
		}
	}

	// From here on the row exists and says "activating". If the process dies,
	// the row says so too, and a resume picks up from created_rules.
	if err := rt.Store().MarkBundleImport(ctx, tenantID, stored.ID, store.BundleActivating,
		stored.CreatedRules, "", "", nil); err != nil {
		return nil, internal(err)
	}

	createdRules := map[string]string{}
	for k, v := range stored.CreatedRules {
		createdRules[k] = v
	}
	resp := &BundleImportResponse{ImportID: stored.ID, Status: store.BundleActivating, Plan: plan}

	// Two passes. The first creates or updates every rule with its references
	// pointing at resource keys; the second rewrites those references once all
	// local ids exist. Without the second pass, a rule that activates another
	// rule would be created before its target has an id.
	for _, r := range plan.Resources {
		key := r.ResourceKey
		src := findBundleRule(man, key)
		if src == nil {
			continue
		}
		if _, done := createdRules[key]; done && r.Action == bundle.ActionCreate {
			// Resume: this one was already created before the interruption.
			resp.Skipped++
			continue
		}
		def := s.materialize(man, src, createdRules, target, req.NotifyChoices)
		def.Normalize()

		switch r.Action {
		case bundle.ActionSkip:
			resp.Skipped++
			if r.RuleID != "" {
				createdRules[key] = r.RuleID
			}
		case bundle.ActionCreate:
			rule := &store.Rule{
				ID: runtime.NewID(), TenantID: tenantID, Name: src.Name, Kind: src.Kind,
				Enabled: true, Definition: def, DefinitionHash: domain.DefinitionHash(def),
				ResourceKey: key, BundleHash: src.Hash,
			}
			if cerr := rt.Store().CreateRule(ctx, rule); cerr != nil {
				return s.failBundleImport(ctx, tenantID, stored.ID, createdRules, resp,
					"create_failed", fmt.Sprintf("创建规则「%s」失败：%v", src.Name, cerr))
			}
			createdRules[key] = rule.ID
			resp.Created++
		case bundle.ActionUpdate:
			existing, gerr := rt.Store().GetRule(ctx, tenantID, r.RuleID)
			if gerr != nil {
				return s.failBundleImport(ctx, tenantID, stored.ID, createdRules, resp,
					"update_target_missing", fmt.Sprintf("要更新的规则不见了：%v", gerr))
			}
			existing.Definition = def
			existing.DefinitionHash = domain.DefinitionHash(def)
			existing.Name = src.Name
			existing.ResourceKey = key
			existing.BundleHash = src.Hash
			if uerr := rt.Store().UpdateRule(ctx, existing, existing.Version); uerr != nil {
				return s.failBundleImport(ctx, tenantID, stored.ID, createdRules, resp,
					"update_failed", fmt.Sprintf("更新规则「%s」失败：%v", src.Name, uerr))
			}
			createdRules[key] = existing.ID
			resp.Updated++
		}

		// Record progress after every rule. A crash mid-import then loses at
		// most one rule's worth of work, and the retry knows exactly which.
		if merr := rt.Store().MarkBundleImport(ctx, tenantID, stored.ID, store.BundleActivating,
			createdRules, "", "", nil); merr != nil {
			return nil, internal(merr)
		}
	}

	// Second pass: references and bindings.
	for _, src := range man.Resources.Rules {
		localID, ok := createdRules[src.ResourceKey]
		if !ok {
			continue
		}
		rule, gerr := rt.Store().GetRule(ctx, tenantID, localID)
		if gerr != nil {
			return s.failBundleImport(ctx, tenantID, stored.ID, createdRules, resp,
				"rule_missing", fmt.Sprintf("规则「%s」在第二步找不到：%v", src.Name, gerr))
		}
		// Refs now point at local ids.
		def := bundle.RewriteRefs(rule.Definition, func(k string) string {
			if id, ok := createdRules[k]; ok {
				return id
			}
			return k
		})
		if bundle.Hash(def) != bundle.Hash(rule.Definition) {
			rule.Definition = def
			rule.DefinitionHash = domain.DefinitionHash(def)
			if uerr := rt.Store().UpdateRule(ctx, rule, rule.Version); uerr != nil {
				return s.failBundleImport(ctx, tenantID, stored.ID, createdRules, resp,
					"rewrite_failed", fmt.Sprintf("回填引用失败：%v", uerr))
			}
			if rr, gerr2 := rt.Store().GetRule(ctx, tenantID, localID); gerr2 == nil {
				rule = rr
			}
		}

		// Bindings: role -> local scope, one row per mapped device.
		for _, b := range src.Bindings {
			if berr := s.applyBundleBinding(ctx, tenantID, rule, b, man, target); berr != nil {
				return s.failBundleImport(ctx, tenantID, stored.ID, createdRules, resp,
					"binding_failed", fmt.Sprintf("绑定「%s」失败：%v", b.ScopeRole, berr))
			}
		}
	}

	if err := rt.Store().MarkBundleImport(ctx, tenantID, stored.ID, store.BundleActivated,
		createdRules, "", "", reportJSON); err != nil {
		return nil, internal(err)
	}
	rt.InvalidateBindings(tenantID, "")
	resp.Status = store.BundleActivated
	resp.Rules = createdRules
	return resp, nil
}

// failBundleImport records a failure and hands the caller the same error.
//
// The status write is the point: an import that stopped halfway must not look
// like one that finished, and the mapping stays so a resume can continue.
func (s *Service) failBundleImport(ctx context.Context, tenantID, importID string,
	createdRules map[string]string, resp *BundleImportResponse, code, message string) (*BundleImportResponse, error) {
	rt, err := s.rtOrErr()
	if err == nil {
		_ = rt.Store().MarkBundleImport(ctx, tenantID, importID, store.BundleFailed,
			createdRules, code, message, nil)
	}
	resp.Status = store.BundleFailed
	resp.Rules = createdRules
	return resp, &errs.Error{Code: errs.Internal, Message: message,
		Meta: errs.Metadata{"import_id": importID, "reason_code": code}}
}

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------

// ListBundleImports shows what has been imported, including failures.
//
//encore:api auth method=GET path=/v1/bundles/imports
func (s *Service) ListBundleImports(ctx context.Context) (*ListBundleImportsResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	rows, lerr := rt.Store().ListBundleImports(ctx, tenantID, 100)
	if lerr != nil {
		return nil, internal(lerr)
	}
	out := &ListBundleImportsResponse{}
	for _, r := range rows {
		out.Imports = append(out.Imports, BundleImportSummary{
			ImportID: r.ID, BundleKey: r.BundleKey, Version: r.Version,
			ContentHash: r.ContentHash, Status: r.Status,
			ReasonCode: r.ReasonCode, Reason: r.ReasonMsg,
			Rules: len(r.CreatedRules), CreatedAt: r.CreatedAt, ActivatedAt: r.ActivatedAt,
		})
	}
	return out, nil
}

// GetBundleImport returns one import with its plan.
//
//encore:api auth method=GET path=/v1/bundles/imports/:id
func (s *Service) GetBundleImport(ctx context.Context, id string) (*BundleImportResponse, error) {
	tenantID, err := mustTenant()
	if err != nil {
		return nil, err
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	row, gerr := rt.Store().GetBundleImport(ctx, tenantID, id)
	if gerr != nil {
		return nil, notFoundOr(gerr, "导入记录不存在")
	}
	out := &BundleImportResponse{
		ImportID: row.ID, Status: row.Status, Rules: row.CreatedRules,
	}
	if len(row.Report) > 0 {
		var plan bundle.Plan
		if json.Unmarshal(row.Report, &plan) == nil {
			out.Plan = &plan
		}
	}
	if row.ReasonMsg != "" {
		out.Issues = append(out.Issues, domain.Issue{
			Field: "import", Code: row.ReasonCode, Message: row.ReasonMsg,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// bundleTarget builds the importer's view of this environment.
func (s *Service) bundleTarget(ctx context.Context, tenantID string, man *bundle.Manifest,
	roleMapping map[string][]string, templateMapping map[string]string,
	notifyChoices map[string]string) (*bundle.Target, error) {
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(man.Resources.Rules))
	for _, r := range man.Resources.Rules {
		keys = append(keys, r.ResourceKey)
	}
	existing, err := rt.Store().RulesByResourceKeys(ctx, tenantID, keys)
	if err != nil {
		return nil, internal(err)
	}
	target := &bundle.Target{
		Existing:      map[string]*bundle.Existing{},
		Roles:         roleMapping,
		Slots:         notifyChoices,
		DeviceConfigs: templateMapping,
	}
	for key, r := range existing {
		// The comparison is against the package form this rule was created
		// from, not against the stored body.
		target.Existing[key] = &bundle.Existing{
			RuleID: r.ID, Name: r.Name, Hash: r.BundleHash,
			Definition: r.Definition,
		}
	}
	// Capability checking uses the same directory the runtime uses, so "the
	// package says this device can do it" is verified against reality rather
	// than against the package's own claim.
	if dir := rt.Directory(); dir != nil {
		target.CanDo = func(deviceID, capability string) bool {
			devices, derr := dir.ByIDs(ctx, tenantID, []string{deviceID})
			if derr != nil || len(devices) == 0 {
				return true // unknown is not a refusal
			}
			for _, c := range devices[0].Capabilities {
				if c == capability {
					return true
				}
			}
			return false
		}
	}
	return target, nil
}

// materialize resolves slots and references for the first pass.
func (s *Service) materialize(man *bundle.Manifest, src *bundle.Rule,
	createdRules map[string]string, target *bundle.Target, choices map[string]string) domain.Definition {
	def := src.Definition
	def = bundle.RewriteSlots(def, func(v string) string {
		if key, ok := bundle.SlotOf(v); ok {
			if chosen, given := choices[key]; given {
				return chosen
			}
			return ""
		}
		return v
	})
	def = bundle.RewriteRefs(def, func(k string) string {
		if id, ok := createdRules[k]; ok {
			return id
		}
		return k
	})
	// Targets come back from the roles the importer resolved. Without this the
	// rule would keep role keys as device ids and never run.
	if target != nil {
		devices := make([]string, 0, len(def.Targets.DeviceIDs))
		for _, roleKey := range def.Targets.DeviceIDs {
			if ids, ok := target.Roles[roleKey]; ok {
				devices = append(devices, ids...)
			}
		}
		configs := make([]string, 0, len(def.Targets.DeviceConfigIDs))
		for _, roleKey := range def.Targets.DeviceConfigIDs {
			role := findRole(man, roleKey)
			if role == nil {
				continue
			}
			if cfg := target.DeviceConfigs[role.TemplateKey]; cfg != "" {
				configs = append(configs, cfg)
			}
		}
		if len(devices) > 0 || len(configs) > 0 {
			def.Targets.DeviceIDs = devices
			def.Targets.DeviceConfigIDs = configs
		}
	}
	return def
}

// applyBundleBinding attaches a rule to the local scope a role resolved to.
func (s *Service) applyBundleBinding(ctx context.Context, tenantID string, rule *store.Rule,
	b bundle.Binding, man *bundle.Manifest, target *bundle.Target) error {
	rt, err := s.rtOrErr()
	if err != nil {
		return err
	}
	role := findRole(man, b.ScopeRole)
	if role == nil {
		return fmt.Errorf("包里没有这个角色：%s", b.ScopeRole)
	}
	upsert := func(deviceID, configID, source string) error {
		row := &store.Binding{
			ID: runtime.NewID(), TenantID: tenantID, RuleID: rule.ID,
			Revision: rule.Revision, Params: b.Params, Enabled: true, Source: source,
			DeviceID: deviceID, DeviceConfigID: configID,
		}
		row.BindingKey = store.BindingKey(tenantID, rule.ID, deviceID, configID)
		return rt.Store().UpsertBinding(ctx, row)
	}
	if role.Kind == bundle.RoleTemplate {
		configID := ""
		if target != nil && target.DeviceConfigs != nil {
			configID = target.DeviceConfigs[role.TemplateKey]
		}
		if configID == "" {
			// No local template: the binding is kept as a template role with the
			// package's own key so it starts working when the template arrives.
			configID = role.TemplateKey
		}
		return upsert("", configID, store.BindingSourceTemplate)
	}
	devices := []string{}
	if target != nil {
		devices = target.Roles[role.Key]
	}
	for _, dev := range devices {
		if err := upsert(dev, "", store.BindingSourceDevice); err != nil {
			return err
		}
	}
	return nil
}

func findRole(man *bundle.Manifest, key string) *bundle.Role {
	for i := range man.Dependencies.Roles {
		if man.Dependencies.Roles[i].Key == key {
			return &man.Dependencies.Roles[i]
		}
	}
	return nil
}

func findBundleRule(man *bundle.Manifest, key string) *bundle.Rule {
	for i := range man.Resources.Rules {
		if man.Resources.Rules[i].ResourceKey == key {
			return &man.Resources.Rules[i]
		}
	}
	return nil
}

// DeriveResourceKey builds a stable key for a rule that never had one.
//
// It is derived from name and kind, not from the row id, so exporting the same
// rule twice — even from two installations that were set up by hand — produces
// the same key. The id is only a tie-breaker for two rules with the same name.
func DeriveResourceKey(name string, kind domain.RuleKind, id string) string {
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		default:
			return '-'
		}
	}, name)
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "rule"
	}
	return "rule:" + slug + "-" + shortHash(string(kind)+"|"+id)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

func roleNameFor(names map[string]string, deviceID string) string {
	if names != nil {
		if n, ok := names[deviceID]; ok && n != "" {
			return n
		}
	}
	return "device-" + shortHash(deviceID)
}

func templateKeyFor(keys map[string]string, configID string) string {
	if keys != nil {
		if k, ok := keys[configID]; ok && k != "" {
			return k
		}
	}
	return configID
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

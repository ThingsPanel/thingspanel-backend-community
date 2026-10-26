package automation

import (
	"encoding/json"
	"strings"
	"testing"
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
// The tests below are all about the difference between "the install finished"
// and "the install worked". A package that imports cleanly and then does
// nothing is worse than one that refuses, because nobody goes looking.

// notifyRule is an automation that raises a notification.
//
// The notification policy id is a local id on purpose: exporting it unchanged
// is exactly the bug this task exists to prevent, so a test that passes one in
// is testing the right thing.
func notifyRule(deviceID, policyID string) domain.Definition {
	def := thresholdRule(deviceID, "30", 0)
	def.Steps = []domain.Step{{
		Kind: domain.StepNotify, NotificationPolicyID: policyID, Value: "高温",
	}}
	return def
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

// ruleCountWithKey counts the rules a package has produced in this tenant.
func (e *testEnv) ruleCountWithKey(key string) int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_rules WHERE tenant_id=$1 AND resource_key=$2 AND deleted_at IS NULL`,
		serviceTestTenant, key).Scan(&n); err != nil {
		e.t.Fatalf("count rules: %v", err)
	}
	return n
}

// exportOne exports a single rule and fails the test if that is refused.
func (e *testEnv) exportOne(rule *store.Rule, extra ...string) *bundle.Manifest {
	e.t.Helper()
	ids := append([]string{rule.ID}, extra...)
	man, err := e.svc().ExportBundle(e.ctx(), &ExportBundleRequest{
		BundleKey: "bundle-" + runtime.NewID()[:8], Version: "1.0.0", RuleIDs: ids,
	})
	if err != nil {
		e.t.Fatalf("export: %v", err)
	}
	return man
}

// TestThePackageCarriesTheRulesButNotTheDevices is the core promise: a package
// describes what it needs, not which row it came from.
func TestThePackageCarriesTheRulesButNotTheDevices(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-1-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "高温通知", notifyRule(dev, "pol-local-secret"))
	e.bind(rule, "device", dev, nil)

	man := e.exportOne(rule)

	if len(man.Resources.Rules) != 1 {
		t.Fatalf("want one rule, got %d", len(man.Resources.Rules))
	}
	if len(man.Dependencies.Roles) != 1 {
		t.Fatalf("want one role, got %d", len(man.Dependencies.Roles))
	}
	if man.Dependencies.Roles[0].Kind != bundle.RoleDevice {
		t.Fatalf("a device binding must export as a device role, got %q", man.Dependencies.Roles[0].Kind)
	}
	if len(man.Resources.NotifySlots) != 1 {
		t.Fatalf("want one notification slot, got %d", len(man.Resources.NotifySlots))
	}
	if !man.Resources.NotifySlots[0].Required {
		t.Fatal("a notify step exists only to notify, so its slot is required")
	}
	if man.ContentHash == "" || !strings.HasPrefix(man.ContentHash, "sha256:") {
		t.Fatalf("a package needs a content hash, got %q", man.ContentHash)
	}

	body := string(mustJSON(t, man))
	if strings.Contains(body, dev) {
		t.Fatalf("the package leaks a device id: %s", body)
	}
	if strings.Contains(body, "pol-local-secret") {
		t.Fatalf("the package leaks a local notification policy id: %s", body)
	}
	if !strings.Contains(body, bundle.SlotPrefix) {
		t.Fatalf("the notification target should have become a slot: %s", body)
	}
}

// TestAPackageThatReferencesSomethingOutsideItselfIsRefused: an edge that leaves
// the package becomes a step that can never succeed.
func TestAPackageThatReferencesSomethingOutsideItselfIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-2-" + runtime.NewID()[:8])
	target := e.createNamedRuleIn(serviceTestTenant, "被引用的场景", thresholdRule(dev, "30", 0))
	caller := e.createNamedRuleIn(serviceTestTenant, "调用者", sceneCalling(dev, target.ID))

	_, err := e.svc().ExportBundle(e.ctx(), &ExportBundleRequest{
		BundleKey: "bundle-out-" + runtime.NewID()[:8], Version: "1.0.0",
		RuleIDs: []string{caller.ID},
	})
	if err == nil {
		t.Fatal("exporting a rule whose reference is outside the package must be refused")
	}
	if code := errs.Code(err); code != errs.FailedPrecondition {
		t.Fatalf("want failed_precondition, got %v (%v)", code, err)
	}

	// Including the target makes the same export legal, and the edge is written
	// in terms of resource keys.
	man := e.exportOne(caller, target.ID)
	if len(man.Dependencies.Edges) != 1 {
		t.Fatalf("want one internal edge, got %d (%+v)", len(man.Dependencies.Edges), man.Dependencies.Edges)
	}
	body := string(mustJSON(t, man))
	if strings.Contains(body, target.ID) {
		t.Fatalf("the edge still points at a local row id: %s", body)
	}
}

// TestImportingTheSamePackageTwiceCreatesItOnce is the "重复导入不重复建资源"
// criterion, checked the only way that means anything: count the rules.
func TestImportingTheSamePackageTwiceCreatesItOnce(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-3-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "要搬走的规则", notifyRule(dev, "pol-a21-3"))
	e.bind(rule, "device", dev, nil)
	man := e.exportOne(rule)
	key := man.Resources.Rules[0].ResourceKey
	roleKey := man.Dependencies.Roles[0].Key
	slotKey := man.Resources.NotifySlots[0].SlotKey

	// Pretend the destination has never seen this rule.
	if err := e.st.DeleteRule(e.ctx(), serviceTestTenant, rule.ID); err != nil {
		t.Fatalf("delete original: %v", err)
	}

	req := &BundleImportRequest{
		Manifest: man, RoleMapping: map[string][]string{roleKey: {dev}},
		NotifyChoices: map[string]string{slotKey: "pol-local-2"},
	}
	first, err := e.svc().ImportBundle(e.ctx(), req)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if first.Created != 1 || first.Status != store.BundleActivated {
		t.Fatalf("want one created rule and activated, got %+v", first)
	}
	if n := e.ruleCountWithKey(key); n != 1 {
		t.Fatalf("want one rule with that key, got %d", n)
	}

	second, err := e.svc().ImportBundle(e.ctx(), req)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !second.Reused {
		t.Fatal("the second import must report that it reused the first")
	}
	if second.Created != 0 {
		t.Fatalf("the second import must not create anything, created %d", second.Created)
	}
	if n := e.ruleCountWithKey(key); n != 1 {
		t.Fatalf("importing twice must not create a second rule, got %d", n)
	}
}

// TestAnUnmappedRoleIsRefusedAndCreatesNothing: a package that needs a device
// cannot be imported into an environment that has not chosen one.
func TestAnUnmappedRoleIsRefusedAndCreatesNothing(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-4-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "需要设备的规则", notifyRule(dev, "pol-a21-4"))
	e.bind(rule, "device", dev, nil)
	man := e.exportOne(rule)
	key := man.Resources.Rules[0].ResourceKey
	slotKey := man.Resources.NotifySlots[0].SlotKey

	preview, err := e.svc().PreviewBundleImport(e.ctx(), &BundlePreviewRequest{
		Manifest: man, NotifyChoices: map[string]string{slotKey: "pol-x"},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Plan.OK {
		t.Fatal("a package with an unfilled role must not be importable")
	}
	if len(preview.Plan.MissingRoles) != 1 {
		t.Fatalf("want one missing role, got %v", preview.Plan.MissingRoles)
	}

	_, err = e.svc().ImportBundle(e.ctx(), &BundleImportRequest{
		Manifest: man, NotifyChoices: map[string]string{slotKey: "pol-x"},
	})
	if err == nil {
		t.Fatal("the import must be refused")
	}
	if n := e.ruleCountWithKey(key); n != 0 {
		t.Fatalf("a refused import must not create anything, got %d", n)
	}

	// And the history must not claim a start either.
	list, err := e.svc().ListBundleImports(e.ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, imp := range list.Imports {
		if imp.ContentHash == man.ContentHash {
			t.Fatalf("a refused import must not leave a row behind: %+v", imp)
		}
	}
}

// TestAFailedImportIsNotReportedAsDoneAndResumes: "半途重启" and "失败不显示完成"
// in one test. The row keeps the mapping, so a retry continues.
func TestAFailedImportIsNotReportedAsDoneAndResumes(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-5-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会失败的导入", notifyRule(dev, "pol-a21-5"))
	e.bind(rule, "device", dev, nil)
	man := e.exportOne(rule)
	roleKey := man.Dependencies.Roles[0].Key
	slotKey := man.Resources.NotifySlots[0].SlotKey
	key := man.Resources.Rules[0].ResourceKey
	if err := e.st.DeleteRule(e.ctx(), serviceTestTenant, rule.ID); err != nil {
		t.Fatalf("delete original: %v", err)
	}

	// Stand in for "the process died halfway": the row exists, says failed, and
	// created nothing yet.
	staged := &store.BundleImport{
		ID: runtime.NewID(), TenantID: serviceTestTenant,
		BundleKey: man.BundleKey, Version: man.Version, ContentHash: man.ContentHash,
		Status: store.BundleFailed, Manifest: mustJSON(t, man),
		ReasonCode: "create_failed", ReasonMsg: "上一次创建到一半被中断",
	}
	if _, _, err := e.st.StageBundleImport(e.ctx(), staged); err != nil {
		t.Fatalf("stage: %v", err)
	}

	req := &BundleImportRequest{
		Manifest: man, RoleMapping: map[string][]string{roleKey: {dev}},
		NotifyChoices: map[string]string{slotKey: "pol-local-5"},
	}
	if _, err := e.svc().ImportBundle(e.ctx(), req); err == nil {
		t.Fatal("a failed import must not be retried silently; it has to be resumed")
	} else if !strings.Contains(err.Error(), "resume") {
		t.Fatalf("the refusal should say how to continue, got %q", err.Error())
	}
	if n := e.ruleCountWithKey(key); n != 0 {
		t.Fatalf("nothing may be created before the resume, got %d", n)
	}

	req.Resume = true
	resumed, err := e.svc().ImportBundle(e.ctx(), req)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != store.BundleActivated {
		t.Fatalf("a resumed import must finish, got %q", resumed.Status)
	}
	if n := e.ruleCountWithKey(key); n != 1 {
		t.Fatalf("the resume must create exactly the missing rule, got %d", n)
	}

	// The history tells the truth about what happened.
	list, err := e.svc().ListBundleImports(e.ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *BundleImportSummary
	for i := range list.Imports {
		if list.Imports[i].ImportID == staged.ID {
			found = &list.Imports[i]
		}
	}
	if found == nil {
		t.Fatal("the import row disappeared")
	}
	if found.Status != store.BundleActivated || found.ActivatedAt == nil {
		t.Fatalf("the finished import must say so: %+v", found)
	}
}

// TestAnOptionalSlotLeftUnansweredMeansNoNotification: declining is a decision,
// not a silent fallback to the exporter's policy.
func TestAnOptionalSlotLeftUnansweredMeansNoNotification(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-6-" + runtime.NewID()[:8])

	// An alarm policy: the episode still opens even if nobody is told, so its
	// notification slot is optional.
	alarm := e.createNamedRuleIn(serviceTestTenant, "可选通知的告警", domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃",
		}}}}},
		Recovery: &domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpLt, Value: "25", Unit: "℃",
		}}}}},
		Freshness: domain.Freshness{
			MaxAge: domain.Duration(10 * time.Minute), MaxGap: domain.Duration(10 * time.Minute),
		},
		Targets: domain.TargetSpec{DeviceIDs: []string{dev}},
		Alarm: &domain.AlarmSpec{
			Key: "policy.a21." + dev, Sources: []domain.AlarmSource{domain.AlarmSourceDetector},
			Level: "H", NotifyPolicyID: "pol-alarm-a21",
		},
	})
	man := e.exportOne(alarm)
	if len(man.Resources.NotifySlots) != 1 {
		t.Fatalf("want one slot, got %d", len(man.Resources.NotifySlots))
	}
	if man.Resources.NotifySlots[0].Required {
		t.Fatal("an alarm that still opens an episode must not require a notification target")
	}
	if err := e.st.DeleteRule(e.ctx(), serviceTestTenant, alarm.ID); err != nil {
		t.Fatalf("delete original: %v", err)
	}

	// The alarm still names a device, so that role has to be filled; what is
	// deliberately left out is the notification choice.
	roleKey := man.Dependencies.Roles[0].Key
	resp, err := e.svc().ImportBundle(e.ctx(), &BundleImportRequest{
		Manifest: man, RoleMapping: map[string][]string{roleKey: {dev}},
	})
	if err != nil {
		t.Fatalf("import without choosing a notification: %v", err)
	}
	if resp.Created != 1 {
		t.Fatalf("want one created rule, got %d", resp.Created)
	}
	local, err := e.st.GetRule(e.ctx(), serviceTestTenant, resp.Rules[man.Resources.Rules[0].ResourceKey])
	if err != nil {
		t.Fatalf("load imported rule: %v", err)
	}
	if local.Definition.Alarm == nil {
		t.Fatal("the alarm spec is missing")
	}
	if local.Definition.Alarm.NotifyPolicyID != "" {
		t.Fatalf("declining a slot must clear the target, got %q", local.Definition.Alarm.NotifyPolicyID)
	}
}

// TestAPackageWithACredentialInAWebhookIsRefused: credentials belong to an
// environment, not to a template.
func TestAPackageWithACredentialInAWebhookIsRefused(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-7-" + runtime.NewID()[:8])
	def := thresholdRule(dev, "30", 0)
	def.Steps[0].Value = "https://hooks.example.com/notify?token=abc123"
	rule := e.createNamedRuleIn(serviceTestTenant, "带凭据的规则", def)

	_, err := e.svc().ExportBundle(e.ctx(), &ExportBundleRequest{
		BundleKey: "bundle-secret-" + runtime.NewID()[:8], Version: "1.0.0",
		RuleIDs: []string{rule.ID},
	})
	if err == nil {
		t.Fatal("a package carrying a webhook token must be refused")
	}
	if !strings.Contains(err.Error(), "凭据") {
		t.Fatalf("the refusal should say what is wrong, got %q", err.Error())
	}
}

// TestAnOldPackageWithoutRolesOrSlotsStillImports: packages published before
// this task existed have no manifest sections at all. They must keep working.
func TestAnOldPackageWithoutRolesOrSlotsStillImports(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-8-" + runtime.NewID()[:8])
	def := thresholdRule(dev, "30", 0)
	def.Normalize()

	man := &bundle.Manifest{
		ContractVersion: bundle.ContractVersion,
		BundleKey:       "legacy-bundle-" + runtime.NewID()[:8],
		Version:         "0.9.0",
		Resources: bundle.Resources{Rules: []bundle.Rule{{
			ResourceKey: "rule:legacy-" + runtime.NewID()[:8],
			Name:        "旧包里的规则",
			Kind:        domain.KindAutomation,
			Revision:    1,
			Definition:  def,
			Hash:        bundle.Hash(def),
		}}},
	}
	man.ComputeHash()

	resp, err := e.svc().ImportBundle(e.ctx(), &BundleImportRequest{Manifest: man})
	if err != nil {
		t.Fatalf("an old package must still import: %v", err)
	}
	if resp.Created != 1 || resp.Status != store.BundleActivated {
		t.Fatalf("want one created rule and activated, got %+v", resp)
	}
}

// TestTheImportWritesThePackageFormHashSoASecondRunIsANoOp: without the stored
// package hash, every re-import would look like a change and bump the revision.
func TestTheImportWritesThePackageFormHashSoASecondRunIsANoOp(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("dev-a21-9-" + runtime.NewID()[:8])
	rule := e.createNamedRuleIn(serviceTestTenant, "会重复导入的规则", notifyRule(dev, "pol-a21-9"))
	e.bind(rule, "device", dev, nil)
	man := e.exportOne(rule)
	roleKey := man.Dependencies.Roles[0].Key
	slotKey := man.Resources.NotifySlots[0].SlotKey
	key := man.Resources.Rules[0].ResourceKey
	if err := e.st.DeleteRule(e.ctx(), serviceTestTenant, rule.ID); err != nil {
		t.Fatalf("delete original: %v", err)
	}

	req := &BundleImportRequest{
		Manifest: man, RoleMapping: map[string][]string{roleKey: {dev}},
		NotifyChoices: map[string]string{slotKey: "pol-local-9"},
	}
	if _, err := e.svc().ImportBundle(e.ctx(), req); err != nil {
		t.Fatalf("import: %v", err)
	}
	stored, err := e.st.RuleByResourceKey(e.ctx(), serviceTestTenant, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stored.BundleHash != man.Resources.Rules[0].Hash {
		t.Fatalf("the rule must remember which package form produced it: %q", stored.BundleHash)
	}

	// Force a fresh import row so the plan is computed again rather than short
	// circuited by the identity lookup.
	if _, err := e.st.DB.ExecContext(e.ctx(),
		`UPDATE auto_bundle_imports SET content_hash='other' WHERE tenant_id=$1 AND content_hash=$2`,
		serviceTestTenant, man.ContentHash); err != nil {
		t.Fatalf("reset identity: %v", err)
	}
	again, err := e.svc().PreviewBundleImport(e.ctx(), &BundlePreviewRequest{
		Manifest: man, RoleMapping: map[string][]string{roleKey: {dev}},
		NotifyChoices: map[string]string{slotKey: "pol-local-9"},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if again.Plan.SkipCount != 1 || again.Plan.UpdateCount != 0 {
		t.Fatalf("an unchanged package must plan a skip, got %+v", again.Plan.Resources)
	}
}

// TestTheBindingSurvivesTheImport: a package's per-device values must land on
// the device the importer chose, not on the exporter's device.
func TestTheBindingSurvivesTheImport(t *testing.T) {
	e := newEnv(t)
	source := e.templateDevice("dev-a21-src-" + runtime.NewID()[:8])
	dest := e.templateDevice("dev-a21-dst-" + runtime.NewID()[:8])

	rule := e.createNamedRuleIn(serviceTestTenant, "带参数的规则", thresholdRule(source, "30", 0))
	e.bind(rule, "device", source, map[string]string{"limit": "55"})
	man := e.exportOne(rule)
	roleKey := man.Dependencies.Roles[0].Key
	key := man.Resources.Rules[0].ResourceKey
	if err := e.st.DeleteRule(e.ctx(), serviceTestTenant, rule.ID); err != nil {
		t.Fatalf("delete original: %v", err)
	}

	resp, err := e.svc().ImportBundle(e.ctx(), &BundleImportRequest{
		Manifest: man, RoleMapping: map[string][]string{roleKey: {dest}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	local, err := e.st.GetRule(e.ctx(), serviceTestTenant, resp.Rules[key])
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	bindings, err := e.st.BindingsForDevice(e.ctx(), serviceTestTenant, dest, "")
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	found := false
	for _, b := range bindings {
		if b.RuleID == local.ID {
			found = true
			if b.Params["limit"] != "55" {
				t.Fatalf("the per-device value must survive the move, got %v", b.Params)
			}
		}
	}
	if !found {
		t.Fatalf("the imported rule is not bound to the chosen device %s", dest)
	}
}

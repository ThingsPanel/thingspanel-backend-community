package automation

import (
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"

	"encore.app/automation/blueprint"
	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// serviceTestTenant is the tenant the auth layer hands back outside a real
// request. Every service-level test in this package shares it, so assertions are
// written as deltas or filtered by device rather than as global counts.
const serviceTestTenant = "test-tenant"

// svc wires the service to this test's runtime. Tests in this package reach the
// service methods directly instead of going through HTTP, because the point here
// is the contract between the template and the rule it creates — not the router.
func (e *testEnv) svc() *Service { return &Service{rt: e.rt} }

// templateDevice registers a device the template can be pointed at.
//
// TenantID is left empty on purpose: fakeDirectory treats an empty tenant as
// visible to everyone, and the service-level endpoints run under the fixed
// test tenant that authn hands back outside a real request.
func (e *testEnv) templateDevice(id string, capabilities ...string) string {
	e.dir.devices[id] = ports.Device{ID: id, Name: "模板设备", Capabilities: capabilities}
	return id
}

func (e *testEnv) ruleCount() int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_rules WHERE tenant_id=$1 AND deleted_at IS NULL`,
		serviceTestTenant).Scan(&n); err != nil {
		e.t.Fatalf("count rules: %v", err)
	}
	return n
}

func (e *testEnv) activeEpisodes(policyKey string) int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM alarm_episodes WHERE policy_key=$1 AND state='active'`,
		policyKey).Scan(&n); err != nil {
		e.t.Fatalf("count episodes: %v", err)
	}
	return n
}

func TestTheFourTemplatesAreListedWithTheirParameters(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().ListBlueprints(e.ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Blueprints) < 4 {
		t.Fatalf("want at least the four built-in templates, got %d", len(resp.Blueprints))
	}
	seen := map[string]bool{}
	for _, bp := range resp.Blueprints {
		seen[bp.Key] = true
		if bp.Name == "" || bp.Guidance == "" {
			t.Errorf("%s: a template must explain itself without internal vocabulary", bp.Key)
		}
		if len(bp.Params) == 0 {
			t.Errorf("%s: a template with no parameters is not a template", bp.Key)
		}
		for _, p := range bp.Params {
			if p.Type == blueprint.ParamDevice && p.Default != "" {
				t.Errorf("%s.%s: a template must not pre-fill a device", bp.Key, p.Key)
			}
			if p.Type == blueprint.ParamPolicy && p.Default != "" {
				t.Errorf("%s.%s: a template must not pre-fill a notification group", bp.Key, p.Key)
			}
		}
	}
	for _, key := range []string{
		"sustained_high_temperature", "door_left_open", "device_offline", "motion_then_light_off",
	} {
		if !seen[key] {
			t.Errorf("template %s is missing from the list", key)
		}
	}
}

func TestATemplateCreatesAnOrdinaryRulePlusAnOrigin(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-1")

	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Version: 1, Values: map[string]string{"device": dev}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	// The rule is not a special kind of object: it is a normal rule with a
	// definition, a revision and a hash, which is what lets everything else
	// (validation, simulation, explain, runs) work unchanged.
	if rule.Kind != string(domain.KindAlarm) {
		t.Fatalf("want an alarm policy, got %s", rule.Kind)
	}
	if rule.Revision != 1 || rule.DefinitionHash == "" {
		t.Fatalf("want a published revision 1 with a hash, got rev=%d hash=%q", rule.Revision, rule.DefinitionHash)
	}
	if len(rule.Definition.Steps) != 0 {
		t.Fatalf("an alarm policy runs no steps, got %d", len(rule.Definition.Steps))
	}
	if rule.Definition.Alarm == nil || rule.Definition.Alarm.Key == "" {
		t.Fatalf("want a stable alarm business key")
	}

	got, err := e.svc().GetRuleBlueprint(e.ctx(), rule.ID)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	if got.Origin == nil {
		t.Fatalf("a template instance must remember where it came from: %s", got.Note)
	}
	if got.Origin.BlueprintKey != "sustained_high_temperature" || got.Origin.BlueprintVersion != 1 {
		t.Fatalf("wrong origin %+v", got.Origin)
	}
	if got.Origin.BlueprintRevision == "" {
		t.Fatalf("the origin must pin a revision, otherwise 'which template' is unanswerable")
	}
	// 5m is v1's default and the customer did not touch it, so it is not an
	// override. Recording it as one would block a later upgrade for no reason.
	if _, ok := got.Origin.Overrides["duration"]; ok {
		t.Fatalf("an untouched default must not be recorded as the customer's choice")
	}
}

func TestTheRuleFromATemplateActuallyRuns(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-2")

	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{
			Values: map[string]string{
				"device": dev, "duration": "1s", "threshold": "35",
				// A one-second recovery window keeps the test fast without
				// weakening what it proves: the recovery still has to be sustained.
				"recovery_duration": "1s",
			},
		})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	// A single hot reading must not be enough: the template declares "持续".
	e.ingestForTenant("ev-bp-hot-1", dev, "temperature", 40.0)
	if n := e.activeEpisodes(rule.Definition.Alarm.Key); n != 0 {
		t.Fatalf("one sample raised an alarm that asks for a sustained condition")
	}

	// After the declared duration the pending condition fires and the alarm
	// opens — through the normal detector path, with no template-specific code.
	e.advance(2 * time.Second)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if n := e.activeEpisodes(rule.Definition.Alarm.Key); n != 1 {
		t.Fatalf("want exactly one active alarm after the sustained condition, got %d", n)
	}

	// And it recovers on its own, because the template declared a recovery
	// condition. Without one the alarm would sit there forever.
	e.ingestForTenant("ev-bp-cool-1", dev, "temperature", 20.0)
	e.advance(2 * time.Second)
	if _, err := e.rt.Tick(e.ctx()); err != nil {
		t.Fatalf("tick after recovery: %v", err)
	}
	if n := e.activeEpisodes(rule.Definition.Alarm.Key); n != 0 {
		t.Fatalf("the alarm did not recover on the declared recovery threshold")
	}
}

func TestPublishingANewTemplateVersionDoesNotChangeARunningRule(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-3")

	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Version: 1, Values: map[string]string{"device": dev}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	// v2 changed the default duration to 3m. The instance was created on v1 and
	// must keep running with 5m until somebody asks for the upgrade.
	got, err := e.svc().GetRuleBlueprint(e.ctx(), rule.ID)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	if !got.Origin.UpdateAvailable || got.Origin.LatestVersion < 2 {
		t.Fatalf("want a newer version to be offered, got %+v", got.Origin)
	}
	if got.Origin.Diff == nil {
		t.Fatalf("an offered update must come with its diff")
	}
	if wait := rule.Definition.Trigger.For.Std(); wait != 5*time.Minute {
		t.Fatalf("the running rule changed to %s just because v2 was published", wait)
	}
	after, err := e.st.GetRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Revision != 1 {
		t.Fatalf("publishing a template version republished the rule (revision %d)", after.Revision)
	}
}

func TestUpgradingAnInstanceRepublishesItAndRepinsTheOrigin(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-4")

	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Version: 1, Values: map[string]string{"device": dev, "threshold": "40"}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	resp, err := e.svc().UpgradeRuleBlueprint(e.ctx(), rule.ID, &UpgradeBlueprintRequest{Version: 2})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if resp.Blocked || !resp.Applied {
		t.Fatalf("want a clean upgrade, got blocked=%v applied=%v issues=%+v",
			resp.Blocked, resp.Applied, resp.Issues)
	}
	// The customer's own threshold survives; the default they never touched
	// follows the new version.
	if v := resp.Rule.Definition.Trigger.Groups[0].Conditions[0].Value; v != "40" {
		t.Fatalf("the customer's threshold was overwritten with %q", v)
	}
	if wait := resp.Rule.Definition.Trigger.For.Std(); wait != 3*time.Minute {
		t.Fatalf("want the v2 default wait of 3m, got %s", wait)
	}
	if resp.Rule.Revision != 2 {
		t.Fatalf("an upgrade publishes a new revision, got %d", resp.Rule.Revision)
	}
	if resp.Origin == nil || resp.Origin.BlueprintVersion != 2 {
		t.Fatalf("the origin must follow the rule, got %+v", resp.Origin)
	}
	if resp.Origin.UpgradedAt == nil {
		t.Fatalf("an upgraded instance should record when it moved")
	}
}

func TestAnUpgradeIsBlockedWhenTheCustomersValueNoLongerFits(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-5")

	// 150 °C was allowed in v1 and is outside v2's range. Refusing is the only
	// honest answer: silently clamping it would change what the customer asked
	// for, and silently keeping it would produce a rule the template rejects.
	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Version: 1, Values: map[string]string{"device": dev, "threshold": "150"}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	resp, err := e.svc().UpgradeRuleBlueprint(e.ctx(), rule.ID, &UpgradeBlueprintRequest{Version: 2})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !resp.Blocked || resp.Applied {
		t.Fatalf("want the upgrade blocked, got blocked=%v applied=%v", resp.Blocked, resp.Applied)
	}
	if !hasBlueprintCode(resp.Issues, "out_of_range") {
		t.Fatalf("the reason must name the range, got %+v", resp.Issues)
	}
	// And nothing was published.
	after, err := e.st.GetRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Revision != 1 {
		t.Fatalf("a blocked upgrade must not publish anything, revision is %d", after.Revision)
	}
	if v := after.Definition.Trigger.Groups[0].Conditions[0].Value; v != "150" {
		t.Fatalf("a blocked upgrade changed the threshold to %q", v)
	}
}

func TestADryRunUpgradeReportsWithoutPublishing(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-6")

	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "door_left_open",
		&InstantiateBlueprintRequest{Version: 1, Values: map[string]string{"device": dev}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	resp, err := e.svc().UpgradeRuleBlueprint(e.ctx(), rule.ID,
		&UpgradeBlueprintRequest{Version: 2, DryRun: true})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if resp.Applied {
		t.Fatalf("a dry run must not publish")
	}
	if resp.Blocked {
		t.Fatalf("want a clean dry run, got %+v", resp.Issues)
	}
	if resp.Diff == nil || len(resp.Diff.AddedParams) != 1 {
		t.Fatalf("the dry run must still show what would change, got %+v", resp.Diff)
	}
	after, err := e.st.GetRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Revision != 1 {
		t.Fatalf("a dry run republished the rule (revision %d)", after.Revision)
	}
}

func TestAHandWrittenRuleHasNoOriginAndCannotBeUpgraded(t *testing.T) {
	e := newEnv(t)
	def := domain.Definition{
		Kind:  domain.KindAutomation,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "x",
			Operator: domain.OpEq, Value: "1",
		}}}}},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Steps: []domain.Step{{Kind: domain.StepAction, DeviceRef: "target",
			Action: "telemetry", Identifier: "x", Value: "1"}},
	}
	// The service-level endpoints run under the fixed tenant authn returns
	// outside a real request, so the hand-written rule has to live there too.
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		t.Fatalf("test rule is invalid: %v", err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: serviceTestTenant, Name: "手写规则", Kind: def.Kind,
		Enabled: true, Definition: def, DefinitionHash: domain.DefinitionHash(def),
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	got, err := e.svc().GetRuleBlueprint(e.ctx(), rule.ID)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	if got.Origin != nil {
		t.Fatalf("a hand-written rule has no template origin")
	}
	if got.Note == "" {
		t.Fatalf("the absence must be explained, not left blank")
	}
	// Upgrading it is refused with a precondition failure rather than a 500.
	_, err = e.svc().UpgradeRuleBlueprint(e.ctx(), rule.ID, &UpgradeBlueprintRequest{Version: 2})
	if err == nil {
		t.Fatalf("want a refusal")
	}
	if ec := errs.Code(err); ec != errs.FailedPrecondition {
		t.Fatalf("want failed_precondition, got %v", ec)
	}
}

func TestAnUnknownDeviceIsRefusedAtInstantiation(t *testing.T) {
	e := newEnv(t)
	before := e.ruleCount()
	_, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Values: map[string]string{"device": "no-such-device"}})
	if err == nil {
		t.Fatalf("want a refusal for a device that is not in the tenant")
	}
	if !strings.Contains(err.Error(), "no-such-device") {
		t.Fatalf("the refusal should name the device, got %v", err)
	}
	if n := e.ruleCount(); n != before {
		t.Fatalf("a refused instantiation left %d rules behind", n-before)
	}
}

func TestAMissingCapabilityIsRefusedOnlyWhenTheDirectoryKnows(t *testing.T) {
	e := newEnv(t)

	// The directory knows this device's capabilities and they do not include a
	// temperature. Creating the rule anyway would produce something that can
	// never fire.
	dev := e.templateDevice("bp-dev-7", "telemetry:power")
	_, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Values: map[string]string{"device": dev}})
	if err == nil {
		t.Fatalf("want a refusal when the device demonstrably lacks the metric")
	}
	if !strings.Contains(err.Error(), "temperature") {
		t.Fatalf("the refusal should name the missing field, got %v", err)
	}

	// A device whose capability list is simply absent is a different case: the
	// community device list does not expose one, so "empty" is not "missing".
	// Refusing here would make every template unusable on every real device.
	unknown := e.templateDevice("bp-dev-8")
	rule, err := e.svc().InstantiateBlueprint(e.ctx(), "sustained_high_temperature",
		&InstantiateBlueprintRequest{Values: map[string]string{"device": unknown}})
	if err != nil {
		t.Fatalf("an unverifiable capability must not block creation: %v", err)
	}
	if rule.ID == "" {
		t.Fatalf("no rule created")
	}

	// ...but the preview must say so, instead of implying it was checked.
	prev, err := e.svc().PreviewBlueprint(e.ctx(), &BlueprintPreviewRequest{
		Key: "sustained_high_temperature", Values: map[string]string{"device": unknown},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !prev.Valid {
		t.Fatalf("want valid=true with a warning, got %+v", prev.Issues)
	}
	if !hasBlueprintCode(prev.Issues, "capability_unverified") {
		t.Fatalf("want the unverified warning, got %+v", prev.Issues)
	}
}

func TestPreviewCreatesNothing(t *testing.T) {
	e := newEnv(t)
	dev := e.templateDevice("bp-dev-9")
	before := e.ruleCount()

	prev, err := e.svc().PreviewBlueprint(e.ctx(), &BlueprintPreviewRequest{
		Key: "motion_then_light_off", Values: map[string]string{"device": dev, "hold": "30s"},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !prev.Valid || prev.Definition == nil {
		t.Fatalf("want a definition back, got %+v", prev)
	}
	if prev.Definition.Mode != domain.ModeRestart {
		t.Fatalf("the motion template must re-arm the timer, got mode %s", prev.Definition.Mode)
	}
	if after := e.ruleCount(); after != before {
		t.Fatalf("preview created %d rules", after-before)
	}
}

func TestPreviewReportsEveryFieldProblemAtOnce(t *testing.T) {
	e := newEnv(t)
	prev, err := e.svc().PreviewBlueprint(e.ctx(), &BlueprintPreviewRequest{
		Key:    "sustained_high_temperature",
		Values: map[string]string{"threshold": "abc", "duration": "soon"},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.Valid {
		t.Fatalf("want valid=false")
	}
	// A form that only reports the first problem makes the customer resubmit
	// once per field.
	for _, code := range []string{"missing_param", "invalid_number", "invalid_duration"} {
		if !hasBlueprintCode(prev.Issues, code) {
			t.Fatalf("want %s among the issues, got %+v", code, prev.Issues)
		}
	}
}

// ingestForTenant pushes a sample as the service-level test tenant, which is the
// tenant the auth layer hands back outside a real request.
func (e *testEnv) ingestForTenant(eventID, device, key string, value any) {
	e.t.Helper()
	if _, err := e.rt.Ingest(e.ctx(), store.IngestEvent{
		EventID: eventID, TenantID: serviceTestTenant, DeviceID: device,
		Signal: domain.SignalTelemetry, Key: key, Value: value, Unit: "°C",
		Quality: domain.QualityGood, ObservedAt: e.now,
	}); err != nil {
		e.t.Fatalf("ingest: %v", err)
	}
}

func hasBlueprintCode(issues []blueprint.Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

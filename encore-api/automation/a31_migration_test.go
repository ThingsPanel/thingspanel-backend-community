package automation

import (
	"strings"
	"testing"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// ---------------------------------------------------------------------------
// A31: the migration has to survive being run twice, stopped halfway, and undone
// ---------------------------------------------------------------------------
//
// Four things are being proved here, and none of them can be proved by reading
// the code:
//
//   - a dry run writes nothing, and still reports every problem;
//   - running the same snapshot again does not create a second rule;
//   - an object that cannot be converted is counted separately, so a report
//     where applied+failed == total cannot hide it;
//   - a rollback refuses to delete the only record that a command was sent.

// legacySceneWriting is one old scene: a single telemetry write on one device.
func legacySceneWriting(id, device, param, value string) LegacySceneInput {
	return LegacySceneInput{
		ID: id, Name: "旧场景 " + id,
		Actions: []LegacyActionInput{{
			ID: id + "-a1", Target: device, Type: "10",
			ParamType: "TEL", Param: param, Value: value,
		}},
	}
}

// legacySceneWithService is a scene whose second action is the old "service"
// type, which has no owner in this release.
func legacySceneWithService(id, device string) LegacySceneInput {
	return LegacySceneInput{
		ID: id, Name: "调外部服务的场景 " + id,
		Actions: []LegacyActionInput{
			{ID: id + "-a1", Target: device, Type: "10", ParamType: "TEL", Param: "switch", Value: "1"},
			{ID: id + "-a2", Target: "https://example.invalid/hook", Type: "40"},
		},
	}
}

func itemOf(t *testing.T, resp *MigrationResponse, legacyID string) *MigrationItemResponse {
	t.Helper()
	for i := range resp.Items {
		if resp.Items[i].LegacyID == legacyID {
			return &resp.Items[i]
		}
	}
	t.Fatalf("迁移结果里没有 %s；实际有 %d 条", legacyID, len(resp.Items))
	return nil
}

func (e *testEnv) outboxCount() int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRowContext(e.ctx(),
		`SELECT count(*) FROM auto_outbox WHERE tenant_id=$1`, serviceTestTenant).Scan(&n); err != nil {
		e.t.Fatalf("count outbox: %v", err)
	}
	return n
}

// migratedAlarmPolicy creates an alarm policy that already carries the id it had
// in the old console, in the tenant the service endpoints run under.
//
// It cannot reuse e.createRule: that helper writes into e.tenant, a per-test
// random tenant, while mustTenant() returns serviceTestTenant outside a real
// request. A policy the service cannot see would make every alarm item resolve to
// "policy not migrated", and the test would pass for the wrong reason.
func (e *testEnv) migratedAlarmPolicy(legacyID string) *store.Rule {
	e.t.Helper()
	def := domain.Definition{
		Kind:  domain.KindAlarm,
		Mode:  domain.ModeSingle,
		Scope: domain.ScopeDevice,
		Trigger: domain.Trigger{Groups: []domain.Group{{Conditions: []domain.Condition{{
			Source: "self", Signal: domain.SignalTelemetry, Key: "temperature",
			Operator: domain.OpGt, Value: "30", Unit: "℃",
		}}}}},
		Targets: domain.TargetSpec{DeviceIDs: []string{e.dev1}},
		Alarm: &domain.AlarmSpec{
			Key: "policy.a31." + legacyID, Level: "H",
			Sources: []domain.AlarmSource{domain.AlarmSourceDetector},
		},
	}
	def.Normalize()
	if err := domain.Validate(&def); err != nil {
		e.t.Fatalf("alarm policy is invalid: %v", err)
	}
	rule := &store.Rule{
		ID: runtime.NewID(), TenantID: serviceTestTenant, Name: "旧告警策略 " + legacyID,
		Kind: def.Kind, Enabled: true, Definition: def,
		DefinitionHash: domain.DefinitionHash(def), LegacyID: legacyID,
	}
	if err := e.st.CreateRule(e.ctx(), rule); err != nil {
		e.t.Fatalf("create alarm policy: %v", err)
	}
	stored, err := e.st.GetRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil {
		e.t.Fatalf("reload alarm policy: %v", err)
	}
	return stored
}

// ---------------------------------------------------------------------------

func TestADryRunWritesNothingButReportsEveryProblem(t *testing.T) {
	e := newEnv(t)
	before := e.ruleCount()

	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "只看看不落库",
		Scenes: []LegacySceneInput{
			legacySceneWriting("dry-ok", e.dev1, "switch", "1"),
			legacySceneWithService("dry-service", e.dev1),
			{ID: "dry-empty", Name: "空场景"},
		},
		Devices: []string{e.dev1},
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if got := e.ruleCount(); got != before {
		t.Fatalf("试运行不应该写任何规则，规则数从 %d 变成了 %d", before, got)
	}
	if resp.Applied != 0 {
		t.Fatalf("试运行不应该应用任何一条，实际 %d", resp.Applied)
	}
	if resp.Total != 3 {
		t.Fatalf("三条旧场景都应该出现在报告里，实际 total=%d", resp.Total)
	}
	// Every problem is reported in one pass, not one at a time.
	if it := itemOf(t, resp, "dry-ok"); it.Status != store.MigrationPlanned {
		t.Fatalf("能迁移的场景应该是 planned，实际 %s（%s）", it.Status, it.Reason)
	}
	if it := itemOf(t, resp, "dry-service"); it.Status != store.MigrationUnsupported {
		t.Fatalf("服务型动作应该被标成 unsupported，实际 %s", it.Status)
	} else if !strings.Contains(it.Reason, "服务型动作") {
		t.Fatalf("原因里应该点名服务型动作，实际 %q", it.Reason)
	}
	if it := itemOf(t, resp, "dry-empty"); it.Status != store.MigrationUnsupported {
		t.Fatalf("空场景应该被标成 unsupported，实际 %s", it.Status)
	}
	if resp.Unresolved != 2 {
		t.Fatalf("两条无法迁移的应该单独计数，实际 unresolved=%d", resp.Unresolved)
	}
}

func TestTheSecondApplyDoesNotDuplicateRules(t *testing.T) {
	e := newEnv(t)
	before := e.ruleCount()
	scene := legacySceneWriting("twice-scene", e.dev1, "switch", "1")

	first, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "第一次", Scenes: []LegacySceneInput{scene}, Devices: []string{e.dev1},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	applied, err := e.svc().ApplyMigration(e.ctx(), first.ID, &MigrationApplyRequest{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Applied != 1 {
		t.Fatalf("应该应用一条，实际 %d", applied.Applied)
	}
	afterFirst := e.ruleCount()
	if afterFirst != before+1 {
		t.Fatalf("应该只多出一条规则，实际多了 %d 条", afterFirst-before)
	}

	// Same run, applied again: nothing left in the batch, nothing duplicated.
	if _, err := e.svc().ApplyMigration(e.ctx(), first.ID, &MigrationApplyRequest{}); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if got := e.ruleCount(); got != afterFirst {
		t.Fatalf("同一次迁移应用两遍不应该再建规则，规则数从 %d 变成 %d", afterFirst, got)
	}

	// A brand new run over the same snapshot: the rule already exists and matches,
	// so the item is skipped rather than created again.
	second, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "第二次", Scenes: []LegacySceneInput{scene}, Devices: []string{e.dev1}, Apply: true,
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := e.ruleCount(); got != afterFirst {
		t.Fatalf("重跑迁移不应该重复建规则，规则数从 %d 变成 %d", afterFirst, got)
	}
	if it := itemOf(t, second, "twice-scene"); it.Status != store.MigrationSkipped {
		t.Fatalf("已经迁移过且定义一致应该是 skipped，实际 %s", it.Status)
	}
	if second.Applied != 0 {
		t.Fatalf("重跑不应该把已存在的算成新应用，实际 applied=%d", second.Applied)
	}
}

func TestAnUnsupportedSceneIsNotCountedAsMigrated(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "一半能迁一半不能",
		Scenes: []LegacySceneInput{
			legacySceneWriting("mix-ok", e.dev1, "switch", "1"),
			legacySceneWithService("mix-bad", e.dev1),
		},
		Devices: []string{e.dev1},
		Apply:   true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.Total != 2 || resp.Applied != 1 || resp.Unresolved != 1 {
		t.Fatalf("应该 total=2 applied=1 unresolved=1，实际 total=%d applied=%d unresolved=%d",
			resp.Total, resp.Applied, resp.Unresolved)
	}
	// The point: applied + failed != total. If a report only counted successes,
	// the scene the customer will miss would be invisible.
	if resp.Applied+resp.Failed == resp.Total {
		t.Fatal("未解决项被算进了已处理，报告会掩盖掉客户真正会丢的东西")
	}
	found := false
	for _, n := range resp.Notes {
		if strings.Contains(n, "无法自动迁移") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes 里应该说明有无法迁移的对象，实际 %v", resp.Notes)
	}
}

func TestVerifyFindsNoOrphanRulesAfterAMigration(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "对账", Scenes: []LegacySceneInput{
			legacySceneWriting("verify-a", e.dev1, "switch", "1"),
			legacySceneWriting("verify-b", e.dev1, "switch", "0"),
		},
		Devices: []string{e.dev1}, Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.Applied != 2 {
		t.Fatalf("两条都应该应用，实际 %d", resp.Applied)
	}

	v, err := e.svc().VerifyMigration(e.ctx(), resp.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !v.Complete {
		t.Fatalf("刚迁移完应该对账通过，实际 missing=%v mismatch=%v orphans=%v unresolved=%v",
			v.Missing, v.DisabledMismatch, v.Orphans, v.Unresolved)
	}
	if len(v.Orphans) != 0 || len(v.Missing) != 0 {
		t.Fatalf("不应该有孤儿或缺失，实际 orphans=%v missing=%v", v.Orphans, v.Missing)
	}
	if v.DuplicateActiveAlarms != 0 {
		t.Fatalf("不应该有重复的活动告警，实际 %d", v.DuplicateActiveAlarms)
	}
}

func TestVerifyReportsAnEditedRuleWithoutFailing(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "迁完再改", Scenes: []LegacySceneInput{
			legacySceneWriting("edit-a", e.dev1, "switch", "1"),
		},
		Devices: []string{e.dev1}, Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "edit-a")
	rule, err := e.st.GetRule(e.ctx(), serviceTestTenant, it.RuleID)
	if err != nil {
		t.Fatalf("load rule: %v", err)
	}
	// The customer tunes the migrated scene. That is allowed.
	edited := rule.Definition
	edited.Steps[0].Value = "0"
	edited.Normalize()
	if err := e.st.UpdateRule(e.ctx(), &store.Rule{
		ID: rule.ID, TenantID: serviceTestTenant, Name: rule.Name, Kind: rule.Kind,
		Definition: edited, DefinitionHash: domain.DefinitionHash(edited),
	}, rule.Version); err != nil {
		t.Fatalf("update: %v", err)
	}

	v, err := e.svc().VerifyMigration(e.ctx(), resp.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(v.Changed) != 1 || v.Changed[0] != "edit-a" {
		t.Fatalf("改过的规则应该出现在 changed 里，实际 %v", v.Changed)
	}
	if !v.Complete {
		t.Fatal("改过迁移来的规则是允许的，不应该让对账失败")
	}
}

func TestASecondAlarmForTheSameSubjectIsSkippedNotOpened(t *testing.T) {
	e := newEnv(t)
	policy := e.migratedAlarmPolicy("legacy-pol-a31")
	before := e.outboxCount()

	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "导入活动告警",
		Alarms: []LegacyAlarmInput{
			{ID: "alarm-a", PolicyLegacyID: "legacy-pol-a31", Subject: e.dev1,
				Status: "M", Level: "M", OpenedAt: "2026-10-20T08:00:00Z"},
			// The same problem again: the old table can hold two rows for one
			// physical fault, and opening both would double the alarm list.
			{ID: "alarm-b", PolicyLegacyID: "legacy-pol-a31", Subject: e.dev1,
				Status: "H", Level: "H", OpenedAt: "2026-10-20T09:00:00Z"},
		},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if got := e.activeEpisodes(policy.PolicyKey()); got != 1 {
		t.Fatalf("同一设备同一指标只能有一条活动告警，实际 %d", got)
	}
	if it := itemOf(t, resp, "alarm-a"); it.Status != store.MigrationApplied {
		t.Fatalf("第一条应该被领养，实际 %s（%s）", it.Status, it.Reason)
	}
	if it := itemOf(t, resp, "alarm-b"); it.Status != store.MigrationSkipped {
		t.Fatalf("第二条应该被跳过而不是再开一个，实际 %s", it.Status)
	}

	// Adopting must not send a notification: the customer was already told.
	if got := e.outboxCount(); got != before {
		t.Fatalf("领养告警不应该产生通知，outbox 从 %d 变成 %d", before, got)
	}

	v, err := e.svc().VerifyMigration(e.ctx(), resp.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.DuplicateActiveAlarms != 0 {
		t.Fatalf("不应该有重复活动告警，实际 %d", v.DuplicateActiveAlarms)
	}
}

func TestAnAlarmWhosePolicyWasNotMigratedIsUnresolved(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "策略没迁就导告警",
		Alarms: []LegacyAlarmInput{{
			ID: "alarm-orphan", PolicyLegacyID: "never-migrated", Subject: e.dev1,
			Status: "M", Level: "M",
		}},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "alarm-orphan")
	if it.Status != store.MigrationUnsupported {
		t.Fatalf("策略没迁移时告警应该 unresolved，实际 %s", it.Status)
	}
	if it.ReasonCode != "alarm_policy_not_migrated" {
		t.Fatalf("原因码应该是 alarm_policy_not_migrated，实际 %q", it.ReasonCode)
	}
	if resp.Applied != 0 {
		t.Fatalf("没有策略就不该有告警被领养，实际 applied=%d", resp.Applied)
	}
}

func TestARecoveredOldAlarmIsNotImported(t *testing.T) {
	e := newEnv(t)
	policy := e.migratedAlarmPolicy("legacy-pol-a31-recovered")
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "已恢复的不导",
		Alarms: []LegacyAlarmInput{{
			ID: "alarm-done", PolicyLegacyID: "legacy-pol-a31-recovered",
			Subject: e.dev1, Status: "N", Level: "H",
		}},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if it := itemOf(t, resp, "alarm-done"); it.Status != store.MigrationSkipped {
		t.Fatalf("已恢复的旧告警应该跳过，实际 %s", it.Status)
	}
	if got := e.activeEpisodes(policy.PolicyKey()); got != 0 {
		t.Fatalf("已恢复的告警不能被重新打开，实际有 %d 条活动告警", got)
	}
}

func TestRollbackRefusesToDeleteARuleThatAlreadyRan(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "回退边界", Scenes: []LegacySceneInput{
			legacySceneWriting("rollback-ran", e.dev1, "switch", "1"),
			legacySceneWriting("rollback-idle", e.dev1, "switch", "0"),
		},
		Devices: []string{e.dev1}, Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ranItem := itemOf(t, resp, "rollback-ran")
	idleItem := itemOf(t, resp, "rollback-idle")

	// One of them has already produced a run: a command may well have reached a
	// device, and the rule is the only record of that.
	if _, err := e.st.DB.ExecContext(e.ctx(), `
        INSERT INTO auto_runs (id, tenant_id, rule_id, revision, definition_hash, scope_key,
                               generation, status, started_at, finished_at)
        VALUES ($1,$2,$3,1,'h',$4,1,'succeeded',now(),now())`,
		runtime.NewID(), serviceTestTenant, ranItem.RuleID, "scope-"+ranItem.RuleID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	after, err := e.svc().RollbackMigration(e.ctx(), resp.ID, &MigrationRollbackRequest{})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if it := itemOf(t, after, "rollback-ran"); it.Status != store.MigrationKept {
		t.Fatalf("执行过的规则应该保留，实际 %s（%s）", it.Status, it.Reason)
	}
	if _, err := e.st.GetRule(e.ctx(), serviceTestTenant, ranItem.RuleID); err != nil {
		t.Fatalf("执行过的规则不应该被删除: %v", err)
	}
	if it := itemOf(t, after, "rollback-idle"); it.Status != store.MigrationRolledBack {
		t.Fatalf("没执行过的规则应该被回退，实际 %s", it.Status)
	}
	if _, err := e.st.GetRule(e.ctx(), serviceTestTenant, idleItem.RuleID); err == nil {
		t.Fatal("没执行过的规则应该已经被删除")
	}
}

func TestRollbackWithForceRemovesTheRuleButKeepsTheRuns(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "强制回退", Scenes: []LegacySceneInput{
			legacySceneWriting("force-ran", e.dev1, "switch", "1"),
		},
		Devices: []string{e.dev1}, Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "force-ran")
	if _, err := e.st.DB.ExecContext(e.ctx(), `
        INSERT INTO auto_runs (id, tenant_id, rule_id, revision, definition_hash, scope_key,
                               generation, status, started_at, finished_at)
        VALUES ($1,$2,$3,1,'h',$4,1,'succeeded',now(),now())`,
		runtime.NewID(), serviceTestTenant, it.RuleID, "scope-"+it.RuleID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	after, err := e.svc().RollbackMigration(e.ctx(), resp.ID, &MigrationRollbackRequest{Force: true})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := itemOf(t, after, "force-ran").Status; got != store.MigrationRolledBack {
		t.Fatalf("强制回退应该删掉规则，实际 %s", got)
	}
	if _, err := e.st.GetRule(e.ctx(), serviceTestTenant, it.RuleID); err == nil {
		t.Fatal("强制回退后规则应该不可见")
	}
	// The run row is the record that something was sent. Deleting the rule must
	// not delete the evidence of what it did.
	runs, err := e.st.ListRuns(e.ctx(), serviceTestTenant, it.RuleID, "", 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("执行记录应该保留，实际 %d 条", len(runs))
	}
}

func TestRollbackThenApplyIsRefused(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "回退后再应用", Scenes: []LegacySceneInput{
			legacySceneWriting("after-rollback", e.dev1, "switch", "1"),
		},
		Devices: []string{e.dev1}, Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.svc().RollbackMigration(e.ctx(), resp.ID, &MigrationRollbackRequest{}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	// Continuing a run whose rules were just deleted would recreate half of them
	// and leave the customer with a state nobody approved.
	if _, err := e.svc().ApplyMigration(e.ctx(), resp.ID, &MigrationApplyRequest{}); err == nil {
		t.Fatal("回退之后不应该还能继续应用")
	}
}

func TestTheSameOldIdTwiceIsRefused(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "同一个 id 出现两次",
		Scenes: []LegacySceneInput{
			legacySceneWriting("dup-id", e.dev1, "switch", "1"),
			legacySceneWriting("dup-id", e.dev1, "switch", "0"),
		},
		Devices: []string{e.dev1},
	})
	if err == nil {
		t.Fatal("同一个旧 id 出现两次必须拒绝，否则一条会覆盖另一条")
	}
	if !strings.Contains(err.Error(), "同时出现") {
		t.Fatalf("错误信息应该说明 id 冲突，实际 %q", err.Error())
	}
}

// TestAnAutomationIsConvertedWithItsOperators replaces an earlier test that
// asserted the opposite. That test was written on the belief that the old
// trigger_operator was a number with no consumer; it is a string and the old
// evaluator reads it, so "not converted" was a defect wearing a reason.
//
// What has to be true now: the groups survive as OR-of-AND, the operator is the
// same string, and the rule is a real automation rather than a scene.
func TestAnAutomationIsConvertedWithItsOperators(t *testing.T) {
	e := newEnv(t)
	on := true
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "联动转换",
		Automations: []LegacyAutomationInput{{
			ID: "auto-ok", Name: "温度高或湿度高就开风机", Enabled: &on,
			Conditions: []LegacyTriggerConditionInput{
				{ID: "c1", GroupID: "g1", Type: "10", Source: e.dev1,
					ParamType: "TEL", Param: "temperature", Operator: ">=", Value: "30"},
				{ID: "c2", GroupID: "g2", Type: "10", Source: e.dev1,
					ParamType: "TEL", Param: "humidity", Operator: ">", Value: "80"},
			},
			Actions: []LegacyActionInput{
				{ID: "a1", Target: e.dev1, Type: "10", ParamType: "CMD", Param: "fan", Value: "on"},
			},
		}},
		Devices: []string{e.dev1},
		Apply:   true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "auto-ok")
	if it.Status != store.MigrationApplied {
		t.Fatalf("联动应该被转换，实际 %s：%s", it.Status, it.Reason)
	}
	rule, err := e.st.GetRule(e.ctx(), serviceTestTenant, it.RuleID)
	if err != nil {
		t.Fatalf("读回迁移出来的规则: %v", err)
	}
	if rule.Definition.Kind != domain.KindAutomation {
		t.Fatalf("联动必须迁移成 automation，实际 %s", rule.Definition.Kind)
	}
	if len(rule.Definition.Trigger.Groups) != 2 {
		t.Fatalf("两个 group_id 应该成为两组 OR 条件，实际 %d 组", len(rule.Definition.Trigger.Groups))
	}
	// The operator is the thing that was wrongly declared unknowable. If it
	// drifted, the rule now fires on a different comparison.
	if got := rule.Definition.Trigger.Groups[0].Conditions[0].Operator; got != ">=" {
		t.Fatalf("运算符应该原样保留 >=\uff0c实际 %q", got)
	}
	if got := rule.Definition.Trigger.Groups[1].Conditions[0].Operator; got != ">" {
		t.Fatalf("运算符应该原样保留 >，实际 %q", got)
	}
	if len(rule.Definition.Steps) != 1 {
		t.Fatalf("应该有一个动作步骤，实际 %d", len(rule.Definition.Steps))
	}
	if !rule.Enabled {
		t.Fatal("旧联动是启用的，迁移后不应该变成停用")
	}
}

// TestAnAutomationThatMixesTriggerSetsIsRefused: a config trigger selects a
// target set, a device trigger names one device. Putting both in one rule would
// need a target set the trigger never chose, so the row is refused — and the
// reason names the structure, not a guess about the data.
func TestAnAutomationThatMixesTriggerSetsIsRefused(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "混用触发集",
		Automations: []LegacyAutomationInput{{
			ID: "auto-mixed", Name: "混用",
			Conditions: []LegacyTriggerConditionInput{
				{ID: "c1", Type: "10", Source: e.dev1, ParamType: "TEL", Param: "temperature", Operator: ">", Value: "30"},
				{ID: "c2", Type: "11", Source: "cfg-1", ParamType: "TEL", Param: "temperature", Operator: ">", Value: "30"},
			},
			Actions: []LegacyActionInput{
				{ID: "a1", Target: e.dev1, Type: "10", ParamType: "CMD", Param: "fan", Value: "on"},
			},
		}},
		Devices:       []string{e.dev1},
		DeviceConfigs: []string{"cfg-1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "auto-mixed")
	if it.Status != store.MigrationUnsupported {
		t.Fatalf("混用触发集应该被拒绝，实际 %s", it.Status)
	}
	if it.ReasonCode != "mixed_trigger_sources" {
		t.Fatalf("原因码应该是 mixed_trigger_sources，实际 %q（%s）", it.ReasonCode, it.Reason)
	}
}

// TestATimeRangeTriggerPointsAtSchedules: a time range is when a rule may run,
// which this release models as a schedule. Copying it into a trigger group would
// turn "only between 08:00 and 18:00" into a condition on a device.
func TestATimeRangeTriggerPointsAtSchedules(t *testing.T) {
	e := newEnv(t)
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "时间范围触发",
		Automations: []LegacyAutomationInput{{
			ID: "auto-time", Name: "只在白天",
			Conditions: []LegacyTriggerConditionInput{
				{ID: "c1", Type: "22", Value: "137|08:00:00+00:00|18:00:00+00:00"},
			},
			Actions: []LegacyActionInput{
				{ID: "a1", Target: e.dev1, Type: "10", ParamType: "CMD", Param: "fan", Value: "on"},
			},
		}},
		Devices: []string{e.dev1},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "auto-time")
	if it.Status != store.MigrationUnsupported {
		t.Fatalf("时间范围触发应该被拒绝，实际 %s", it.Status)
	}
	if it.ReasonCode != "time_range_trigger_needs_schedule" {
		t.Fatalf("原因码应该是 time_range_trigger_needs_schedule，实际 %q", it.ReasonCode)
	}
	if !strings.Contains(it.Reason, "排程") {
		t.Fatalf("原因里应该说清楚它属于排程，实际 %q", it.Reason)
	}
}

// TestAnAlarmConfigBecomesAnEventOnlyPolicy: the old alarm_config has no
// condition column, so the only faithful conversion is a policy that the
// detector never evaluates and only the event channel may qualify. The level and
// the derived callers have to survive, because they are what makes an adopted
// active alarm attach to the right policy.
func TestAnAlarmConfigBecomesAnEventOnlyPolicy(t *testing.T) {
	e := newEnv(t)
	on := true
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "第一遍：告警策略",
		AlarmConfigs: []LegacyAlarmConfigInput{{
			ID: "ac-1", Name: "高温告警", Level: "H", Enabled: &on,
			NotifyGroupID: "ng-1",
			Callers:       []string{"scene:sc-1"},
		}},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "ac-1")
	if it.Status != store.MigrationApplied {
		t.Fatalf("告警配置应该被转换成策略，实际 %s：%s", it.Status, it.Reason)
	}
	rule, err := e.st.GetRule(e.ctx(), serviceTestTenant, it.RuleID)
	if err != nil {
		t.Fatalf("读回策略: %v", err)
	}
	if rule.Definition.Kind != domain.KindAlarm {
		t.Fatalf("应该建告警策略，实际 %s", rule.Definition.Kind)
	}
	if rule.Definition.Alarm == nil {
		t.Fatal("告警策略必须带 alarm 段")
	}
	if len(rule.Definition.Alarm.Sources) != 1 || rule.Definition.Alarm.Sources[0] != domain.AlarmSourceEvent {
		t.Fatalf("旧告警配置没有触发条件，只能声明事件通道，实际 %v", rule.Definition.Alarm.Sources)
	}
	if rule.Definition.Alarm.Level != "H" {
		t.Fatalf("等级应该保留 H，实际 %q", rule.Definition.Alarm.Level)
	}
	// "automation" must be there: that is the source name a scene's alarm step
	// reports, so without it the policy would reject the very step that raises
	// it. The derived caller id is kept as evidence but is not what the runtime
	// sends.
	if got := strings.Join(rule.Definition.Alarm.AllowedSources, ","); got != "automation,scene:sc-1" {
		t.Fatalf("允许的调用方应该是 automation 加旧数据里的引用方，实际 %v", rule.Definition.Alarm.AllowedSources)
	}
	// No trigger groups: an event-only policy has no detector condition, and
	// validation has to accept that rather than force an invented one.
	if len(rule.Definition.Trigger.Groups) != 0 {
		t.Fatalf("事件型策略不应该有触发组，实际 %d 组", len(rule.Definition.Trigger.Groups))
	}
	// The old notification group is reported, not silently mapped onto an id
	// from another owner.
	if !strings.Contains(it.Reason, "ng-1") {
		t.Fatalf("提示里应该点名未绑定的旧通知组，实际 %q", it.Reason)
	}
}

// TestAnActiveAlarmAttachesToTheMigratedPolicy is the second pass: after the
// policy exists, the alarm that was open on a device is adopted onto it instead
// of being re-raised, and the identity is the policy key rather than a new one.
func TestAnActiveAlarmAttachesToTheMigratedPolicy(t *testing.T) {
	e := newEnv(t)
	on := true
	before := e.outboxCount()
	if _, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name:         "第一遍：告警策略",
		AlarmConfigs: []LegacyAlarmConfigInput{{ID: "ac-2", Name: "高温告警", Level: "M", Enabled: &on}},
		Apply:        true,
	}); err != nil {
		t.Fatalf("pass 1: %v", err)
	}

	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name: "第二遍：活动告警",
		Alarms: []LegacyAlarmInput{{
			ID: "al-1", PolicyLegacyID: "ac-2", Subject: e.dev1,
			Level: "M", Status: "M", OpenedAt: "2026-10-01T08:00:00Z",
		}},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	it := itemOf(t, resp, "al-1")
	if it.Status != store.MigrationApplied {
		t.Fatalf("活动告警应该被领养，实际 %s：%s", it.Status, it.Reason)
	}
	if it.EpisodeID == "" {
		t.Fatal("领养后应该给出告警实例 id")
	}
	// Adopting must not send anything: the customer was already told.
	if n := e.outboxCount() - before; n != 0 {
		t.Fatalf("领养旧告警不应该发通知，实际新增 %d 条", n)
	}
}

// TestASecondRunDoesNotReportTheFirstRunsRulesAsOrphans is the defect that only
// a second run against a real installation showed: verify only counted rules
// written by *applied* items, so the rules the first run created came back as
// "orphans" on the second run. The check that exists to catch a duplicated rule
// was instead accusing a correct re-run of being one.
func TestASecondRunDoesNotReportTheFirstRunsRulesAsOrphans(t *testing.T) {
	e := newEnv(t)
	snap := func(name string) *MigrationCreateRequest {
		return &MigrationCreateRequest{
			Name:    name,
			Scenes:  []LegacySceneInput{legacySceneWriting("sc-rerun", e.dev1, "switch", "1")},
			Devices: []string{e.dev1},
			Apply:   true,
		}
	}
	if _, err := e.svc().CreateMigration(e.ctx(), snap("第一次")); err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := e.svc().CreateMigration(e.ctx(), snap("第二次"))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	it := itemOf(t, second, "sc-rerun")
	if it.Status != store.MigrationSkipped {
		t.Fatalf("第二次应该跳过，实际 %s", it.Status)
	}
	v, err := e.svc().VerifyMigration(e.ctx(), second.ID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(v.Orphans) != 0 {
		t.Fatalf("第二次运行不应该把第一次建的规则报成重复占用，实际 %v", v.Orphans)
	}
	if !v.Complete {
		t.Fatalf("重跑之后对账应该通过，实际 missing=%v disabled=%v", v.Missing, v.DisabledMismatch)
	}
}

// TestADisabledAlarmConfigIsSkippedNotEnabled: turning something the customer
// switched off back on is worse than leaving it behind, so it is skipped with
// the instruction instead of migrated enabled.
func TestADisabledAlarmConfigIsSkippedNotEnabled(t *testing.T) {
	e := newEnv(t)
	off := false
	resp, err := e.svc().CreateMigration(e.ctx(), &MigrationCreateRequest{
		Name:         "停用的告警配置",
		AlarmConfigs: []LegacyAlarmConfigInput{{ID: "ac-3", Name: "已停用", Level: "L", Enabled: &off}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	it := itemOf(t, resp, "ac-3")
	if it.Status != store.MigrationSkipped {
		t.Fatalf("停用的告警配置应该跳过，实际 %s", it.Status)
	}
	if it.ReasonCode != "alarm_config_disabled" {
		t.Fatalf("原因码应该是 alarm_config_disabled，实际 %q", it.ReasonCode)
	}
}

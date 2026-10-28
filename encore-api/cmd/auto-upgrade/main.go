// Command auto-upgrade moves one installation's automation and alarm
// configuration from the old tables into the rewritten owner.
//
// It exists because the migration endpoint takes a snapshot, and asking an
// operator to assemble that snapshot by hand — seven tables, joined, in the right
// shape — is not an upgrade path. This is the upgrade path: one command, reading
// the old database directly, driving the endpoints that already carry the tested
// planning and applying logic.
//
// Two passes, in this order, and the order is not cosmetic:
//
//	pass 1  alarm_config   → event-only alarm policies
//	pass 2  scene_info + scene_automations + latest_device_alarms
//
// A scene or automation whose action raises an alarm needs the policy to exist
// before it can be planned, and an active alarm needs the policy to exist before
// it can be adopted. Planning is a pure function of the snapshot, so the policy
// cannot be "planned but not yet applied" and still be resolvable. Hence the two
// passes; both are one command.
//
// What is deliberately not migrated:
//
//   - alarm_history and alarm_info are a log, not configuration. The new model
//     has its own transition history, and replaying an old log into it would
//     invent timestamps and outcomes. The counts are printed so the operator
//     knows they are still there in the old tables.
//   - notification group ids are printed, not bound: the notification owner has
//     its own policy ids, and writing a foreign id would produce a policy that
//     looks wired up and sends nothing.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	var (
		oldDSN   = flag.String("old-dsn", os.Getenv("TP_OLD_DSN"), "旧库连接串（PostgreSQL），也可用 TP_OLD_DSN")
		api      = flag.String("api", "http://127.0.0.1:4000", "新服务地址")
		token    = flag.String("token", os.Getenv("TP_TOKEN"), "新服务鉴权令牌，也可用 TP_TOKEN")
		tenant   = flag.String("tenant", "", "要迁移的租户 id；旧库只有一个租户时可以省略")
		name     = flag.String("name", "", "迁移名称，默认按时间生成")
		dryRun   = flag.Bool("dry-run", true, "只出计划不写库；确认后用 --dry-run=false")
		batch    = flag.Int("batch", 100, "每批应用多少条")
		limit    = flag.Int("limit", 0, "每个 pass 最多处理多少条旧对象，0 表示不限")
		listOnly = flag.Bool("list-tenants", false, "只列出旧库里的租户和对象数量，不迁移")
		verbose  = flag.Bool("v", false, "打印每一条的处理结果")
	)
	flag.Parse()

	if err := run(options{
		oldDSN: *oldDSN, api: *api, token: *token, tenant: *tenant, name: *name,
		dryRun: *dryRun, batch: *batch, limit: *limit, listOnly: *listOnly, verbose: *verbose,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "升级失败："+err.Error())
		os.Exit(1)
	}
}

type options struct {
	oldDSN   string
	api      string
	token    string
	tenant   string
	name     string
	dryRun   bool
	batch    int
	limit    int
	listOnly bool
	verbose  bool
}

func run(o options) error {
	if o.oldDSN == "" {
		return errors.New("缺少 --old-dsn（或环境变量 TP_OLD_DSN）：需要旧库连接串才能读出要迁移的配置")
	}
	db, err := sql.Open("pgx", o.oldDSN)
	if err != nil {
		return fmt.Errorf("连接旧库: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("旧库不可用: %w", err)
	}

	if o.listOnly {
		return listTenants(db)
	}

	tenantID := o.tenant
	if tenantID == "" {
		ids, err := tenants(db)
		if err != nil {
			return err
		}
		switch len(ids) {
		case 1:
			tenantID = ids[0]
			fmt.Printf("旧库只有一个租户 %s，按它迁移\n", tenantID)
		case 0:
			return errors.New("旧库里没有任何自动化或告警配置，无需迁移")
		default:
			return fmt.Errorf("旧库有 %d 个租户（%s），必须用 --tenant 指定要迁移哪一个",
				len(ids), strings.Join(ids, ", "))
		}
	}

	snap, err := readSnapshot(db, tenantID)
	if err != nil {
		return err
	}
	snap.report(os.Stdout)

	if snap.total() == 0 {
		fmt.Println("\n没有需要迁移的对象。")
		return nil
	}
	if o.limit > 0 {
		snap.truncate(o.limit)
		fmt.Printf("\n按 --limit=%d 截断后：%d 条\n", o.limit, snap.total())
	}

	if o.dryRun {
		fmt.Println("\n=== 只出计划（--dry-run=true），不写库 ===")
		fmt.Println("注意：第一遍的告警策略还没有写入，所以第二遍里引用策略的场景动作、以及活动告警，" +
			"在这里会被报成「无法迁移」。真正执行时先写第一遍，第二遍就能对上；" +
			"下面的清单用来核对有多少条、为什么，不用来判定最终结果。")
	} else {
		fmt.Println("\n=== 开始写入 ===")
	}

	cli := &client{base: strings.TrimRight(o.api, "/"), token: o.token, http: &http.Client{Timeout: 120 * time.Second}}
	name := o.name
	if name == "" {
		name = "upgrade-" + time.Now().Format("20060102-150405")
	}

	var total passResult

	// Pass 1: policies. Nothing in this pass can reference anything else, so it
	// cannot be blocked by an unresolved object from a later pass.
	pass1 := snapshot{tenant: tenantID, devices: snap.devices, configs: snap.configs,
		alarmConfigs: snap.alarmConfigs}
	if len(pass1.alarmConfigs) > 0 {
		res, err := drive(cli, o, name+" / 第一遍：告警策略", pass1)
		if err != nil {
			return err
		}
		total.failed += res.failed
		total.unresolved += res.unresolved
	}

	// Pass 2: everything that may reference a policy.
	pass2 := snapshot{tenant: tenantID, devices: snap.devices, configs: snap.configs,
		scenes: snap.scenes, automations: snap.automations, alarms: snap.alarms}
	if pass2.total() > 0 {
		res, err := drive(cli, o, name+" / 第二遍：场景、联动、活动告警", pass2)
		if err != nil {
			return err
		}
		total.failed += res.failed
		total.unresolved += res.unresolved
	}

	if o.dryRun {
		fmt.Printf("\n计划已生成，没有写入任何数据。确认后加 --dry-run=false 再跑一次。\n")
		return nil
	}
	// The two numbers are reported apart on purpose. "18 条没迁移" sends an
	// operator looking for a bug; "3 条应用失败，15 条需要人工决定" tells them
	// which pile to start with.
	if total.failed > 0 {
		return fmt.Errorf("有 %d 条应用失败，可以再跑一次重试；如果反复失败，说明需要人工处理", total.failed)
	}
	if total.unresolved > 0 {
		return fmt.Errorf("配置已写入，但有 %d 条旧对象无法自动转换，清单在上面；"+
			"这些需要人工在新界面重建，或者接受不再使用", total.unresolved)
	}
	fmt.Println("\n升级完成，对账通过。")
	return nil
}

// passResult separates "could not be converted" from "failed to apply". The two
// need different actions from the operator, so they must not be added into one
// number that reads as "something broke".
type passResult struct {
	failed     int
	unresolved int
}

// drive runs one migration pass: create, apply in batches, verify.
func drive(cli *client, o options, name string, snap snapshot) (passResult, error) {
	req := snap.request(name, false)
	var created migrationResponse
	if err := cli.post("/v1/migrations", req, &created); err != nil {
		return passResult{}, fmt.Errorf("创建迁移「%s」: %w", name, err)
	}
	fmt.Printf("\n--- %s ---\n", name)

	if o.dryRun {
		// Nothing has been applied, so the counts are all zero. The item list is
		// the plan, and the plan is what the operator is approving.
		summarise(created, true, o.verbose)
		return passResult{unresolved: created.Unresolved}, nil
	}

	run := created
	for run.Status == "applying" || run.Status == "planned" {
		var next migrationResponse
		if err := cli.post("/v1/migrations/"+run.ID+"/apply",
			map[string]int{"limit": o.batch}, &next); err != nil {
			return passResult{}, fmt.Errorf("应用「%s」: %w", name, err)
		}
		if next.Applied+next.Failed+next.Skipped+next.Unresolved == run.Applied+run.Failed+run.Skipped+run.Unresolved {
			// No progress and still not finished: stop rather than spin.
			run = next
			break
		}
		run = next
	}
	summarise(run, false, o.verbose)

	var verify verifyResponse
	if err := cli.post("/v1/migrations/"+run.ID+"/verify", map[string]any{}, &verify); err != nil {
		return passResult{failed: run.Failed, unresolved: run.Unresolved},
			fmt.Errorf("对账「%s」: %w", name, err)
	}
	reportVerify(verify)

	out := passResult{failed: run.Failed, unresolved: run.Unresolved}
	if out.unresolved == 0 && !verify.Complete {
		// The run itself converted everything but the reconciliation disagrees:
		// that is a failure, not an object waiting for a human.
		out.failed += len(verify.Missing) + len(verify.Orphans)
	}
	return out, nil
}

// summarise prints the run and everything a person has to look at.
//
// In a dry run nothing has been applied yet, so the counts are all zero and every
// item is "planned" — which reads as "nothing to do". The full item list is
// printed instead, with "planned" shown as 待写入, because the whole point of the
// dry run is to be the thing the operator approves.
func summarise(r migrationResponse, dryRun, verbose bool) {
	if dryRun {
		fmt.Printf("计划共 %d 条（还没有写入）\n", r.Total)
	} else {
		fmt.Printf("总计 %d：已迁移 %d，跳过 %d，失败 %d，无法自动迁移 %d\n",
			r.Total, r.Applied, r.Skipped, r.Failed, r.Unresolved)
	}
	for _, n := range r.Notes {
		fmt.Println("  · " + n)
	}
	for _, it := range r.Items {
		if !dryRun && (it.Status == "applied" || it.Status == "skipped") && !verbose {
			continue
		}
		status := it.Status
		if dryRun && status == "planned" {
			status = "待写入"
		}
		line := fmt.Sprintf("  [%s] %s %s", status, it.Kind, it.LegacyID)
		if it.Name != "" {
			line += "（" + it.Name + "）"
		}
		if it.ReasonCode != "" {
			line += " " + it.ReasonCode
		}
		if it.Reason != "" {
			line += "：" + it.Reason
		}
		fmt.Println(line)
	}
}

func reportVerify(v verifyResponse) {
	if v.Complete {
		fmt.Println("  对账通过")
		return
	}
	fmt.Println("  对账未通过：")
	printList("    缺失", v.Missing)
	printList("    开关不一致", v.DisabledMismatch)
	printList("    未迁移", v.Unresolved)
	printList("    重复占用旧 id", v.Orphans)
	if v.DuplicateActiveAlarms > 0 {
		fmt.Printf("    同一设备同一指标有多条活动告警：%d\n", v.DuplicateActiveAlarms)
	}
	for _, n := range v.Notes {
		fmt.Println("    · " + n)
	}
}

func printList(label string, ids []string) {
	if len(ids) == 0 {
		return
	}
	fmt.Printf("%s（%d）：%s\n", label, len(ids), strings.Join(ids, ", "))
}

// ---------------------------------------------------------------------------
// reading the old database
// ---------------------------------------------------------------------------

type snapshot struct {
	tenant       string
	devices      []string
	configs      []string
	scenes       []sceneInput
	automations  []automationInput
	alarmConfigs []alarmConfigInput
	alarms       []alarmInput

	// alarmSource names the table the active alarms came from, so the report
	// does not claim one schema when the installation has the other.
	alarmSource string

	// Counts of things this command deliberately does not migrate.
	historyRows int
	infoRows    int
}

func (s snapshot) total() int {
	return len(s.scenes) + len(s.automations) + len(s.alarmConfigs) + len(s.alarms)
}

func (s *snapshot) truncate(n int) {
	if len(s.scenes) > n {
		s.scenes = s.scenes[:n]
	}
	if len(s.automations) > n {
		s.automations = s.automations[:n]
	}
	if len(s.alarmConfigs) > n {
		s.alarmConfigs = s.alarmConfigs[:n]
	}
	if len(s.alarms) > n {
		s.alarms = s.alarms[:n]
	}
}

func (s snapshot) report(w io.Writer) {
	fmt.Fprintf(w, "租户 %s 的旧配置：\n", s.tenant)
	fmt.Fprintf(w, "  场景（scene_info）              %d 条，动作 %d 条\n", len(s.scenes), countActions(s.scenes))
	fmt.Fprintf(w, "  联动（scene_automations）       %d 条，动作 %d 条\n", len(s.automations), countAutomationActions(s.automations))
	fmt.Fprintf(w, "  告警配置（alarm_config）        %d 条\n", len(s.alarmConfigs))
	fmt.Fprintf(w, "  活动告警（%s）%d 条\n", s.alarmSource, len(s.alarms))
	fmt.Fprintf(w, "  设备 %d 台，设备配置 %d 个（用来核对动作引用的目标是否还在）\n", len(s.devices), len(s.configs))
	if s.historyRows > 0 || s.infoRows > 0 {
		fmt.Fprintf(w, "  不迁移：告警历史 %d 条、告警信息 %d 条（是日志，不是配置，留在旧表里可查）\n",
			s.historyRows, s.infoRows)
	}
}

func countActions(scenes []sceneInput) int {
	n := 0
	for _, s := range scenes {
		n += len(s.Actions)
	}
	return n
}

func countAutomationActions(as []automationInput) int {
	n := 0
	for _, a := range as {
		n += len(a.Actions)
	}
	return n
}

func listTenants(db *sql.DB) error {
	ids, err := tenants(db)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("旧库里没有场景、联动或告警配置。")
		return nil
	}
	for _, id := range ids {
		s, err := readSnapshot(db, id)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", id)
		s.report(os.Stdout)
	}
	return nil
}

// tenants lists the tenants that have anything worth migrating.
//
// Only tables that every version of the schema has: the per-device alarm table
// was added later, and a UNION over a missing table would fail the whole command
// on exactly the old installations this is meant for.
func tenants(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
        SELECT tenant_id FROM scene_info
         UNION SELECT tenant_id FROM scene_automations
         UNION SELECT tenant_id FROM alarm_config
         UNION SELECT tenant_id FROM alarm_info`)
	if err != nil {
		return nil, fmt.Errorf("列出旧库租户: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if strings.TrimSpace(id) != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, rows.Err()
}

func readSnapshot(db *sql.DB, tenantID string) (snapshot, error) {
	s := snapshot{tenant: tenantID}

	var err error
	if s.devices, err = idList(db, `SELECT id FROM devices WHERE tenant_id=$1`, tenantID); err != nil {
		return s, fmt.Errorf("读设备: %w", err)
	}
	if s.configs, err = idList(db, `SELECT id FROM device_configs WHERE tenant_id=$1`, tenantID); err != nil {
		return s, fmt.Errorf("读设备配置: %w", err)
	}
	if s.scenes, err = readScenes(db, tenantID); err != nil {
		return s, err
	}
	if s.automations, err = readAutomations(db, tenantID); err != nil {
		return s, err
	}
	if s.alarmConfigs, err = readAlarmConfigs(db, tenantID); err != nil {
		return s, err
	}
	if s.alarms, s.alarmSource, err = readActiveAlarms(db, tenantID); err != nil {
		return s, err
	}
	// The callers of each policy are read from the same data, so pass 1 can state
	// them even though the scenes that use them are only planned in pass 2.
	attachCallers(&s)
	if s.historyRows, err = countRows(db, `SELECT count(*) FROM alarm_history WHERE tenant_id=$1`, tenantID); err != nil {
		return s, err
	}
	if s.infoRows, err = countRows(db, `SELECT count(*) FROM alarm_info WHERE tenant_id=$1`, tenantID); err != nil {
		return s, err
	}
	return s, nil
}

func idList(db *sql.DB, query, tenantID string) ([]string, error) {
	rows, err := db.Query(query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func countRows(db *sql.DB, query, tenantID string) (int, error) {
	var n int
	err := db.QueryRow(query, tenantID).Scan(&n)
	if err != nil {
		// A table that does not exist in this deployment is not a failure: the
		// count is informational.
		return 0, nil
	}
	return n, nil
}

func readScenes(db *sql.DB, tenantID string) ([]sceneInput, error) {
	// No enabled column: the old scene object has no switch at all, which is why
	// a migrated scene always arrives enabled.
	rows, err := db.Query(`
        SELECT id, coalesce(name,''), coalesce(description,'')
          FROM scene_info WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读场景: %w", err)
	}
	defer rows.Close()

	byID := map[string]*sceneInput{}
	var order []string
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			return nil, err
		}
		byID[id] = &sceneInput{ID: id, Name: name}
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// created_at is the order the old console stored, and order is part of what a
	// scene means: "open the valve, then start the pump" is not the same set.
	arows, err := db.Query(`
        SELECT scene_id, id, coalesce(action_target,''), action_type,
               coalesce(action_param_type,''), coalesce(action_param,''), coalesce(action_value,'')
          FROM scene_action_info WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读场景动作: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var sceneID string
		var a actionInput
		if err := arows.Scan(&sceneID, &a.ID, &a.Target, &a.Type, &a.ParamType, &a.Param, &a.Value); err != nil {
			return nil, err
		}
		if sc := byID[sceneID]; sc != nil {
			sc.Actions = append(sc.Actions, a)
		}
	}
	if err := arows.Err(); err != nil {
		return nil, err
	}

	out := make([]sceneInput, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func readAutomations(db *sql.DB, tenantID string) ([]automationInput, error) {
	rows, err := db.Query(`
        SELECT id, coalesce(name,''), coalesce(enabled,'Y')
          FROM scene_automations WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读联动: %w", err)
	}
	defer rows.Close()

	byID := map[string]*automationInput{}
	var order []string
	for rows.Next() {
		var id, name, enabled string
		if err := rows.Scan(&id, &name, &enabled); err != nil {
			return nil, err
		}
		on := strings.EqualFold(strings.TrimSpace(enabled), "Y")
		byID[id] = &automationInput{ID: id, Name: name, Enabled: &on}
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	crows, err := db.Query(`
        SELECT scene_automation_id, id, coalesce(group_id,''), trigger_condition_type,
               coalesce(trigger_source,''), coalesce(trigger_param_type,''),
               coalesce(trigger_param,''), coalesce(trigger_operator,''), coalesce(trigger_value,'')
          FROM device_trigger_condition WHERE tenant_id=$1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读触发条件: %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var autoID string
		var c conditionInput
		if err := crows.Scan(&autoID, &c.ID, &c.GroupID, &c.Type, &c.Source,
			&c.ParamType, &c.Param, &c.Operator, &c.Value); err != nil {
			return nil, err
		}
		if au := byID[autoID]; au != nil {
			au.Conditions = append(au.Conditions, c)
		}
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}

	// action_info has no tenant_id of its own; it is scoped through its parent.
	arows, err := db.Query(`
        SELECT a.scene_automation_id, a.id, coalesce(a.action_target,''), a.action_type,
               coalesce(a.action_param_type,''), coalesce(a.action_param,''), coalesce(a.action_value,'')
          FROM action_info a
          JOIN scene_automations s ON s.id = a.scene_automation_id
         WHERE s.tenant_id=$1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读联动动作: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var autoID string
		var a actionInput
		if err := arows.Scan(&autoID, &a.ID, &a.Target, &a.Type, &a.ParamType, &a.Param, &a.Value); err != nil {
			return nil, err
		}
		if au := byID[autoID]; au != nil {
			au.Actions = append(au.Actions, a)
		}
	}
	if err := arows.Err(); err != nil {
		return nil, err
	}

	out := make([]automationInput, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func readAlarmConfigs(db *sql.DB, tenantID string) ([]alarmConfigInput, error) {
	rows, err := db.Query(`
        SELECT id, coalesce(name,''), coalesce(alarm_level,''),
               coalesce(notification_group_id,''), coalesce(enabled,'Y')
          FROM alarm_config WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("读告警配置: %w", err)
	}
	defer rows.Close()
	var out []alarmConfigInput
	for rows.Next() {
		var id, name, level, group, enabled string
		if err := rows.Scan(&id, &name, &level, &group, &enabled); err != nil {
			return nil, err
		}
		on := strings.EqualFold(strings.TrimSpace(enabled), "Y")
		out = append(out, alarmConfigInput{
			ID: id, Name: name, Level: level, NotifyGroupID: group, Enabled: &on,
		})
	}
	return out, rows.Err()
}

// readActiveAlarms reads the current per-device alarm state.
//
// Two schemas exist in the field, and which one is present decides where the
// device id comes from:
//
//   - newer community installs keep latest_device_alarms, one row per device and
//     configuration, with the device id, the level and the time. That is the
//     table to adopt from.
//   - older installs have only alarm_info, which records the alarm but not which
//     device raised it. The subject is recovered from the content when the
//     content is JSON that carries it, and left empty when it is not — the
//     planner then refuses that row with "no subject" instead of this command
//     inventing a device.
func readActiveAlarms(db *sql.DB, tenantID string) ([]alarmInput, string, error) {
	if hasTable(db, "latest_device_alarms") {
		rows, err := db.Query(`
            SELECT coalesce(id, device_id || ':' || alarm_config_id),
                   alarm_config_id, device_id, alarm_status,
                   to_char(coalesce(create_at, now()) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
              FROM latest_device_alarms
             WHERE tenant_id=$1 AND alarm_status IS NOT NULL AND alarm_status <> 'N'
               AND device_id IS NOT NULL AND alarm_config_id IS NOT NULL`, tenantID)
		if err != nil {
			return nil, "latest_device_alarms", fmt.Errorf("读活动告警: %w", err)
		}
		defer rows.Close()
		var out []alarmInput
		for rows.Next() {
			var a alarmInput
			if err := rows.Scan(&a.ID, &a.PolicyLegacyID, &a.Subject, &a.Status, &a.OpenedAt); err != nil {
				return nil, "latest_device_alarms", err
			}
			a.Level = a.Status
			out = append(out, a)
		}
		return out, "latest_device_alarms", rows.Err()
	}

	rows, err := db.Query(`
        SELECT id, alarm_config_id, coalesce(alarm_level,''), coalesce(content,''),
               to_char(coalesce(alarm_time, now()) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
          FROM alarm_info
         WHERE tenant_id=$1 AND processing_result = 'UND'`, tenantID)
	if err != nil {
		return nil, "alarm_info", fmt.Errorf("读活动告警（alarm_info）: %w", err)
	}
	defer rows.Close()
	var out []alarmInput
	for rows.Next() {
		var a alarmInput
		var content string
		if err := rows.Scan(&a.ID, &a.PolicyLegacyID, &a.Status, &content, &a.OpenedAt); err != nil {
			return nil, "alarm_info", err
		}
		a.Level = a.Status
		a.Subject = deviceIDFromContent(content)
		out = append(out, a)
	}
	return out, "alarm_info", rows.Err()
}

// deviceIDFromContent recovers the device an old alarm_info row was about, when
// the content is JSON that says so. It returns "" rather than guessing.
func deviceIDFromContent(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "{") {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return ""
	}
	for _, key := range []string{"device_id", "deviceId", "deviceID"} {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func hasTable(db *sql.DB, name string) bool {
	var n int
	err := db.QueryRow(`SELECT count(*) FROM information_schema.tables
                         WHERE table_schema='public' AND table_name=$1`, name).Scan(&n)
	return err == nil && n > 0
}

// attachCallers fills in who raises each alarm configuration.
func attachCallers(s *snapshot) {
	callers := map[string][]string{}
	for _, sc := range s.scenes {
		for _, a := range sc.Actions {
			if a.Type == "30" && a.Target != "" {
				callers[a.Target] = append(callers[a.Target], "scene:"+sc.ID)
			}
		}
	}
	for _, au := range s.automations {
		for _, a := range au.Actions {
			if a.Type == "30" && a.Target != "" {
				callers[a.Target] = append(callers[a.Target], "automation:"+au.ID)
			}
		}
	}
	for i := range s.alarmConfigs {
		if c, ok := callers[s.alarmConfigs[i].ID]; ok {
			sort.Strings(c)
			s.alarmConfigs[i].Callers = c
		}
	}
}

// ---------------------------------------------------------------------------
// the wire shape
// ---------------------------------------------------------------------------

type actionInput struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Type      string `json:"type"`
	ParamType string `json:"param_type,omitempty"`
	Param     string `json:"param,omitempty"`
	Value     string `json:"value,omitempty"`
}

type sceneInput struct {
	ID      string        `json:"id"`
	Name    string        `json:"name,omitempty"`
	Actions []actionInput `json:"actions"`
}

type conditionInput struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type"`
	GroupID   string `json:"group_id,omitempty"`
	Source    string `json:"source,omitempty"`
	ParamType string `json:"param_type,omitempty"`
	Param     string `json:"param,omitempty"`
	Operator  string `json:"operator,omitempty"`
	Value     string `json:"value,omitempty"`
}

type automationInput struct {
	ID         string           `json:"id"`
	Name       string           `json:"name,omitempty"`
	Enabled    *bool            `json:"enabled,omitempty"`
	Conditions []conditionInput `json:"conditions,omitempty"`
	Actions    []actionInput    `json:"actions,omitempty"`
}

type alarmConfigInput struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Level         string   `json:"level,omitempty"`
	NotifyGroupID string   `json:"notify_group_id,omitempty"`
	Callers       []string `json:"callers,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
}

type alarmInput struct {
	ID             string `json:"id"`
	PolicyLegacyID string `json:"policy_legacy_id"`
	Subject        string `json:"subject"`
	Dimension      string `json:"dimension,omitempty"`
	Level          string `json:"level,omitempty"`
	Status         string `json:"status"`
	OpenedAt       string `json:"opened_at,omitempty"`
}

func (s snapshot) request(name string, apply bool) map[string]any {
	body := map[string]any{
		"name":  name,
		"apply": apply,
	}
	if len(s.scenes) > 0 {
		body["scenes"] = s.scenes
	}
	if len(s.automations) > 0 {
		body["automations"] = s.automations
	}
	if len(s.alarmConfigs) > 0 {
		body["alarm_configs"] = s.alarmConfigs
	}
	if len(s.alarms) > 0 {
		body["alarms"] = s.alarms
	}
	if len(s.devices) > 0 {
		body["devices"] = s.devices
	}
	if len(s.configs) > 0 {
		body["device_configs"] = s.configs
	}
	return body
}

type migrationItem struct {
	LegacyID   string `json:"legacy_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	RuleID     string `json:"rule_id"`
	EpisodeID  string `json:"episode_id"`
	ReasonCode string `json:"reason_code"`
	Reason     string `json:"reason"`
}

type migrationResponse struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	Total      int             `json:"total"`
	Applied    int             `json:"applied"`
	Skipped    int             `json:"skipped"`
	Failed     int             `json:"failed"`
	Unresolved int             `json:"unresolved"`
	Items      []migrationItem `json:"items"`
	Notes      []string        `json:"notes"`
}

type verifyResponse struct {
	Complete              bool     `json:"complete"`
	Missing               []string `json:"missing"`
	DisabledMismatch      []string `json:"disabled_mismatch"`
	Changed               []string `json:"changed"`
	Orphans               []string `json:"orphans"`
	Unresolved            []string `json:"unresolved"`
	DuplicateActiveAlarms int      `json:"duplicate_active_alarms"`
	Notes                 []string `json:"notes"`
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

type client struct {
	base  string
	token string
	http  *http.Client
}

func (c *client) post(path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		// Both headers, because the dev auth mode and the legacy JWT mode read
		// different ones and the operator should not have to know which is on.
		req.Header.Set("x-token", c.token)
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s 返回 %d：%s", path, resp.StatusCode, apiError(payload))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("解析 %s 的响应: %w（原文 %s）", path, err, truncate(string(payload), 200))
	}
	return nil
}

// apiError renders the error body the way the console shows it, so the operator
// sees the reason code instead of a bare status.
func apiError(payload []byte) string {
	var e struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(payload, &e); err == nil && e.Message != "" {
		out := e.Message
		if c, ok := e.Details["code"].(string); ok && c != "" {
			out += "（" + c + "）"
		}
		return out
	}
	return truncate(string(payload), 300)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const version = "2.0.0"

// ---------------------------------------------------------------------------
// Command description
// ---------------------------------------------------------------------------
//
// Every command is one row: a method, a path, a set of flags and a function
// that turns those flags into a request. There is no second implementation of
// any behaviour here — a command that needs a rule to exist calls the same
// endpoint the web page calls, so a CLI user and a browser user cannot get
// different answers to the same question.

type flagSpec struct {
	name string
	kind string // string | int | bool | kv | repeat
	help string
	def  string
}

type command struct {
	name   string
	usage  string
	method string
	path   string // ":id" placeholders, filled from positional args
	args   []string
	flags  []flagSpec
	build  func(in *invocation) (any, map[string][]string, error)
	// sideEffect marks commands that can change something. They get an
	// idempotency key automatically; reads do not, because a read that is
	// retried twice is not a problem worth a header.
	sideEffect bool
	// summary is the one-line help.
	summary string
	// verdict turns a 200 response into a non-zero exit when the answer is
	// "no". A validation command that exits 0 for an invalid body is useless in
	// CI, which is the whole reason it exists.
	verdict func(data []byte) *apiError
	// legacy marks the old tpctl spellings. They are kept so existing scripts
	// keep working; the mapping is printed by `tpctl help legacy`.
	legacy bool
}

type invocation struct {
	cmd    *command
	args   []string
	values map[string][]string
	global globalFlags
}

func (in *invocation) last(name string) string {
	v := in.values[name]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

func (in *invocation) has(name string) bool {
	_, ok := in.values[name]
	return ok
}

func (in *invocation) num(name string) (int, error) {
	raw := in.last(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("--%s 需要整数，收到 %q", name, raw)
	}
	return n, nil
}

func (in *invocation) boolean(name string) bool {
	raw := in.last(name)
	if raw == "" {
		return false
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return b
}

// jsonFile reads --file/--samples. "-" means stdin, which is what makes
// `cat rule.json | tpctl rules create --file -` work.
func (in *invocation) jsonFile(name string) (map[string]any, error) {
	path := in.last(name)
	if path == "" {
		return nil, fmt.Errorf("需要 --%s", name)
	}
	raw, err := readFileOrStdin(path)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		// An array is accepted for --samples, wrapped into the request shape.
		var arr []any
		if json.Unmarshal(raw, &arr) == nil {
			return map[string]any{name: arr}, nil
		}
		return nil, fmt.Errorf("--%s 不是合法 JSON 对象: %v", name, err)
	}
	return obj, nil
}

func readFileOrStdin(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// pairs collects a repeatable --k=v flag into a map, and a repeatable
// --role name=id,id into a map of lists.
func (in *invocation) pairs(name string) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range in.values[name] {
		i := strings.Index(raw, "=")
		if i <= 0 {
			return nil, fmt.Errorf("--%s 需要 k=v，收到 %q", name, raw)
		}
		out[raw[:i]] = raw[i+1:]
	}
	return out, nil
}

func (in *invocation) listPairs(name string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, raw := range in.values[name] {
		i := strings.Index(raw, "=")
		if i <= 0 {
			return nil, fmt.Errorf("--%s 需要 name=id[,id]，收到 %q", name, raw)
		}
		var ids []string
		for _, id := range strings.Split(raw[i+1:], ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		out[raw[:i]] = append(out[raw[:i]], ids...)
	}
	return out, nil
}

// queryOf turns string flags into a query string, skipping the empty ones so a
// default never silently becomes a filter.
func queryOf(in *invocation, names ...string) map[string][]string {
	out := map[string][]string{}
	for _, n := range names {
		if v := in.last(n); v != "" {
			out[n] = []string{v}
		}
	}
	return out
}

func withQuery(base map[string][]string, extra map[string][]string) map[string][]string {
	if base == nil {
		base = map[string][]string{}
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// ---------------------------------------------------------------------------
// The table
// ---------------------------------------------------------------------------

func commands() commandsList {
	fileFlag := flagSpec{name: "file", kind: "string", help: "请求体 JSON 文件，- 表示标准输入"}

	return []*command{
		{
			name: "rules.list", method: "GET", path: "/v1/rules",
			summary: "列出规则（分页）",
			flags: []flagSpec{
				{name: "kind", kind: "string", help: "automation|alarm|scene"},
				{name: "device", kind: "string", help: "按目标设备过滤"},
				{name: "device-config", kind: "string", help: "按设备模板过滤"},
				{name: "name", kind: "string", help: "名称包含"},
				{name: "enabled", kind: "string", help: "true|false"},
				{name: "limit", kind: "int", def: "50"},
				{name: "offset", kind: "int"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				q := queryOf(in, "kind", "name", "enabled", "limit", "offset")
				if v := in.last("device"); v != "" {
					q["device_id"] = []string{v}
				}
				if v := in.last("device-config"); v != "" {
					q["device_config_id"] = []string{v}
				}
				return nil, q, nil
			},
		},
		{
			name: "rules.get", method: "GET", path: "/v1/rules/:id",
			args: []string{"id"}, summary: "读取一条规则",
		},
		{
			name: "rules.create", method: "POST", path: "/v1/rules",
			sideEffect: true, summary: "创建规则（请求体来自 --file）",
			flags: []flagSpec{
				fileFlag,
				{name: "name", kind: "string", help: "覆盖请求体里的 name"},
				{name: "enabled", kind: "bool", help: "创建后立即启用"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				body, err := in.jsonFile("file")
				if err != nil {
					return nil, nil, err
				}
				if v := in.last("name"); v != "" {
					body["name"] = v
				}
				if in.has("enabled") {
					body["enabled"] = in.boolean("enabled")
				}
				if _, ok := body["definition"]; !ok {
					return nil, nil, fmt.Errorf("请求体缺少 definition（用 --file 提供完整规则）")
				}
				return body, nil, nil
			},
		},
		{
			name: "rules.update", method: "PUT", path: "/v1/rules/:id",
			args: []string{"id"}, sideEffect: true, summary: "更新规则（乐观版本）",
			flags: []flagSpec{
				fileFlag,
				{name: "version", kind: "int", help: "客户端最后看到的版本"},
				{name: "preview-token", kind: "string", help: "变更预览返回的令牌"},
				{name: "force", kind: "bool", help: "明知会让设备值失效也要保存"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				body, err := in.jsonFile("file")
				if err != nil {
					return nil, nil, err
				}
				n, err := in.num("version")
				if err != nil {
					return nil, nil, err
				}
				if n > 0 {
					body["version"] = n
				}
				if v := in.last("preview-token"); v != "" {
					body["previewToken"] = v
				}
				if in.boolean("force") {
					body["force"] = true
				}
				return body, nil, nil
			},
		},
		{
			name: "rules.enable", method: "POST", path: "/v1/rules/:id/enabled",
			args: []string{"id"}, sideEffect: true, summary: "启用规则",
			flags: []flagSpec{{name: "version", kind: "int"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return enabledBody(in, true)
			},
		},
		{
			name: "rules.disable", method: "POST", path: "/v1/rules/:id/enabled",
			args: []string{"id"}, sideEffect: true, summary: "停用规则",
			flags: []flagSpec{{name: "version", kind: "int"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return enabledBody(in, false)
			},
		},
		{
			name: "rules.delete", method: "DELETE", path: "/v1/rules/:id",
			args: []string{"id"}, sideEffect: true, summary: "删除规则（被引用时拒绝）",
			flags: []flagSpec{{name: "force", kind: "bool", help: "明知有引用也要删"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return map[string]any{"force": in.boolean("force")}, nil, nil
			},
		},
		{
			name: "rules.validate", method: "POST", path: "/v1/rule-validation",
			summary: "只校验不保存（与网页同一套校验）",
			flags:   []flagSpec{fileFlag},
			build: func(in *invocation) (any, map[string][]string, error) {
				return definitionBody(in)
			},
			verdict: invalidVerdict,
		},
		{
			name: "rules.simulate", method: "POST", path: "/v1/rules/:id/simulate",
			args: []string{"id"}, summary: "无副作用模拟（不控设备、不发通知）",
			flags: []flagSpec{
				{name: "samples", kind: "string", help: "样本 JSON 文件或 -"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				body, err := in.jsonFile("samples")
				if err != nil {
					return nil, nil, err
				}
				if _, ok := body["samples"]; !ok {
					return nil, nil, fmt.Errorf("--samples 需要 {\"samples\":[...]} 或直接一个数组")
				}
				return body, nil, nil
			},
		},
		{
			name: "rules.preview", method: "POST", path: "/v1/rules/:id/change-preview",
			args: []string{"id"}, summary: "变更预览（保存前看影响范围与令牌）",
			flags: []flagSpec{fileFlag},
			build: func(in *invocation) (any, map[string][]string, error) {
				return definitionBody(in)
			},
		},
		{
			name: "rules.check", method: "POST", path: "/v1/rules/:id/revision-check",
			args: []string{"id"}, summary: "新正文能不能套用已有设备值",
			flags: []flagSpec{fileFlag},
			build: func(in *invocation) (any, map[string][]string, error) {
				return definitionBody(in)
			},
			verdict: incompatibleVerdict,
		},
		{
			name: "rules.explain", method: "GET", path: "/v1/rules/:id/explain",
			args: []string{"id"}, summary: "为什么没跑（含没有运行记录的情况）",
			flags: []flagSpec{
				{name: "device", kind: "string"},
				{name: "from", kind: "string", help: "RFC3339"},
				{name: "to", kind: "string", help: "RFC3339"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				q := queryOf(in, "from", "to")
				if v := in.last("device"); v != "" {
					q["device_id"] = []string{v}
				}
				return nil, q, nil
			},
		},
		{
			name: "rules.run", method: "POST", path: "/v1/rules/:id/run",
			args: []string{"id"}, sideEffect: true, summary: "真实执行一次（会控设备）",
			flags: []flagSpec{{name: "device", kind: "string"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				body := map[string]any{}
				if v := in.last("device"); v != "" {
					body["device_id"] = v
				}
				return body, nil, nil
			},
		},
		{
			name: "rules.runs", method: "GET", path: "/v1/rules/:id/runs",
			args: []string{"id"}, summary: "运行日志（分页 + 状态集合 + 时间窗）",
			flags: []flagSpec{
				{name: "status", kind: "string", help: "逗号分隔的集合"},
				{name: "limit", kind: "int", def: "50"},
				{name: "offset", kind: "int"},
				{name: "before", kind: "string"},
				{name: "after", kind: "string"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				return nil, queryOf(in, "status", "limit", "offset", "before", "after"), nil
			},
		},
		{
			name: "runs.get", method: "GET", path: "/v1/runs/:runId",
			args: []string{"runId"}, summary: "运行详情（步骤、目标、出站意图）",
		},
		{
			name: "bindings.list", method: "GET", path: "/v1/rules/:id/bindings",
			args: []string{"id"}, summary: "一条规则的绑定",
		},
		{
			name: "bindings.attach", method: "POST", path: "/v1/rules/:id/bindings",
			args: []string{"id"}, sideEffect: true, summary: "挂到设备或模板（可带参数覆盖）",
			flags: []flagSpec{
				{name: "scope", kind: "string", help: "device|device_config"},
				{name: "scope-id", kind: "string"},
				{name: "params", kind: "kv", help: "k=v，可重复"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				scope := in.last("scope")
				if scope == "" {
					scope = "device"
				}
				if scope != "device" && scope != "device_config" {
					return nil, nil, fmt.Errorf("--scope 只能是 device 或 device_config")
				}
				id := in.last("scope-id")
				if id == "" {
					return nil, nil, fmt.Errorf("需要 --scope-id")
				}
				params, err := in.pairs("params")
				if err != nil {
					return nil, nil, err
				}
				return map[string]any{
					"scope_type": scope, "scope_id": id, "params": params,
				}, nil, nil
			},
		},
		{
			name: "bindings.detach", method: "DELETE", path: "/v1/bindings/:id",
			args: []string{"id"}, sideEffect: true, summary: "脱离（保留证据，不删行）",
		},
		{
			name: "bindings.device", method: "GET", path: "/v1/device-automation/:deviceId",
			args: []string{"deviceId"}, summary: "这台设备真正生效的规则",
		},
		{
			name: "bindings.template", method: "GET", path: "/v1/device-configs/:id/bindings",
			args: []string{"id"}, summary: "模板的绑定与对账",
		},
		{
			name: "bindings.reconcile", method: "POST", path: "/v1/binding-reconcile",
			sideEffect: true, summary: "孤儿绑定与幽灵等待窗对账（默认只报告）",
			flags: []flagSpec{{name: "apply", kind: "bool", help: "真的脱离并清理"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return map[string]any{"apply": in.boolean("apply")}, nil, nil
			},
		},
		{
			name: "bundles.export", method: "POST", path: "/v1/bundles/export",
			summary: "导出资源包（规则以业务键、设备以角色出包）",
			flags: []flagSpec{
				{name: "rules", kind: "string", help: "逗号分隔的规则 id"},
				{name: "key", kind: "string", help: "包的业务键"},
				{name: "version", kind: "string"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				ids := splitList(in.last("rules"))
				if len(ids) == 0 {
					return nil, nil, fmt.Errorf("需要 --rules id[,id]")
				}
				body := map[string]any{"ruleIds": ids}
				if v := in.last("key"); v != "" {
					body["bundleKey"] = v
				}
				if v := in.last("version"); v != "" {
					body["version"] = v
				}
				return body, nil, nil
			},
		},
		{
			name: "bundles.preview", method: "POST", path: "/v1/bundles/preview",
			summary: "预演导入（不写库）",
			flags:   bundleFlags(),
			build: func(in *invocation) (any, map[string][]string, error) {
				return bundleBody(in, false)
			},
		},
		{
			name: "bundles.import", method: "POST", path: "/v1/bundles/import",
			sideEffect: true, summary: "导入资源包（失败可 --resume）",
			flags: append(bundleFlags(), flagSpec{name: "resume", kind: "bool"}),
			build: func(in *invocation) (any, map[string][]string, error) {
				return bundleBody(in, in.boolean("resume"))
			},
		},
		{
			name: "bundles.imports", method: "GET", path: "/v1/bundles/imports",
			summary: "导入历史",
		},
		{
			name: "bundles.importGet", method: "GET", path: "/v1/bundles/imports/:id",
			args: []string{"id"}, summary: "一次导入的详情与恢复点",
		},
		{
			name: "alarms.list", method: "GET", path: "/v1/alarms",
			summary: "告警查询",
			flags: []flagSpec{
				{name: "policy", kind: "string"},
				{name: "subject", kind: "string"},
				{name: "state", kind: "string"},
				{name: "limit", kind: "int", def: "50"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				q := queryOf(in, "state", "limit")
				if v := in.last("policy"); v != "" {
					q["policy_id"] = []string{v}
				}
				if v := in.last("subject"); v != "" {
					q["subject_id"] = []string{v}
				}
				return nil, q, nil
			},
		},
		{
			name: "alarms.get", method: "GET", path: "/v1/alarms/:id",
			args: []string{"id"}, summary: "一条告警",
		},
		{
			name: "alarms.ack", method: "POST", path: "/v1/alarms/:id/ack",
			args: []string{"id"}, sideEffect: true, summary: "确认（只记“有人知道了”）",
			flags: []flagSpec{{name: "actor", kind: "string"}, {name: "note", kind: "string"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				body := map[string]any{}
				if v := in.last("actor"); v != "" {
					body["actor"] = v
				}
				if v := in.last("note"); v != "" {
					body["note"] = v
				}
				return body, nil, nil
			},
		},
		{
			name: "alarms.transitions", method: "GET", path: "/v1/alarms/:id/transitions",
			args: []string{"id"}, summary: "这条告警的转换历史",
		},
		{
			name: "blueprints.list", method: "GET", path: "/v1/blueprints",
			summary: "能力与模板发现",
		},
		{
			name: "blueprints.preview", method: "POST", path: "/v1/blueprint-preview",
			summary: "模板预演（不落库）",
			flags: []flagSpec{
				{name: "key", kind: "string"}, {name: "version", kind: "int"},
				{name: "params", kind: "kv"},
				{name: "device", kind: "string"}, {name: "device-config", kind: "string"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				params, err := in.pairs("params")
				if err != nil {
					return nil, nil, err
				}
				body := map[string]any{"blueprintKey": in.last("key"), "params": params}
				if n, _ := in.num("version"); n > 0 {
					body["version"] = n
				}
				if v := in.last("device"); v != "" {
					body["deviceIds"] = []string{v}
				}
				if v := in.last("device-config"); v != "" {
					body["deviceConfigIds"] = []string{v}
				}
				return body, nil, nil
			},
		},
		{
			name: "blueprints.instantiate", method: "POST", path: "/v1/blueprints/:key/instantiate",
			args: []string{"key"}, sideEffect: true, summary: "按模板创建规则",
			flags: []flagSpec{
				{name: "name", kind: "string"}, {name: "version", kind: "int"},
				{name: "params", kind: "kv"},
				{name: "device", kind: "string"}, {name: "device-config", kind: "string"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				params, err := in.pairs("params")
				if err != nil {
					return nil, nil, err
				}
				body := map[string]any{"params": params}
				if v := in.last("name"); v != "" {
					body["name"] = v
				}
				if n, _ := in.num("version"); n > 0 {
					body["version"] = n
				}
				if v := in.last("device"); v != "" {
					body["deviceIds"] = []string{v}
				}
				if v := in.last("device-config"); v != "" {
					body["deviceConfigIds"] = []string{v}
				}
				return body, nil, nil
			},
		},

		// ---- legacy spellings -------------------------------------------------
		{
			name: "automations.create", method: "POST", path: "/v1/rules",
			legacy: true, sideEffect: true, summary: "旧名，等价于 rules.create",
			flags: []flagSpec{fileFlag, {name: "name", kind: "string"},
				{name: "enabled", kind: "bool"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return commands().find("rules.create").build(in)
			},
		},
		{
			name: "automations.list", method: "GET", path: "/v1/rules",
			legacy: true, summary: "旧名，等价于 rules.list",
			flags: []flagSpec{{name: "limit", kind: "int", def: "50"},
				{name: "offset", kind: "int"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return nil, queryOf(in, "limit", "offset"), nil
			},
		},
		{
			name: "automations.switch", method: "POST", path: "/v1/rules/:id/enabled",
			args: []string{"id"}, legacy: true, sideEffect: true,
			summary: "旧名，等价于 rules.enable/rules.disable",
			flags: []flagSpec{
				{name: "version", kind: "int"},
				{name: "on", kind: "bool", help: "启用（默认）"},
				{name: "off", kind: "bool", help: "停用"},
			},
			build: func(in *invocation) (any, map[string][]string, error) {
				return enabledBody(in, !in.boolean("off"))
			},
		},
		{
			name: "automations.delete", method: "DELETE", path: "/v1/rules/:id",
			args: []string{"id"}, legacy: true, sideEffect: true,
			summary: "旧名，等价于 rules.delete",
			flags:   []flagSpec{{name: "force", kind: "bool"}},
			build: func(in *invocation) (any, map[string][]string, error) {
				return map[string]any{"force": in.boolean("force")}, nil, nil
			},
		},
	}
}

func enabledBody(in *invocation, enabled bool) (any, map[string][]string, error) {
	body := map[string]any{"enabled": enabled}
	n, err := in.num("version")
	if err != nil {
		return nil, nil, err
	}
	if n > 0 {
		body["version"] = n
	}
	return body, nil, nil
}

func definitionBody(in *invocation) (any, map[string][]string, error) {
	body, err := in.jsonFile("file")
	if err != nil {
		return nil, nil, err
	}
	if _, ok := body["definition"]; ok {
		return body, nil, nil
	}
	// A file that is itself the definition is the common case when somebody
	// exports one body from the web page, so accept it as such.
	return map[string]any{"definition": body}, nil, nil
}

func bundleFlags() []flagSpec {
	return []flagSpec{
		{name: "file", kind: "string", help: "资源包 JSON，- 表示标准输入"},
		{name: "role", kind: "repeat", help: "角色=设备id[,id]，可重复"},
		{name: "template", kind: "kv", help: "模板键=本地模板 id"},
		{name: "slot", kind: "kv", help: "插槽=本地通知策略 id"},
	}
}

func bundleBody(in *invocation, resume bool) (any, map[string][]string, error) {
	pkg, err := in.jsonFile("file")
	if err != nil {
		return nil, nil, err
	}
	roles, err := in.listPairs("role")
	if err != nil {
		return nil, nil, err
	}
	templates, err := in.pairs("template")
	if err != nil {
		return nil, nil, err
	}
	slots, err := in.pairs("slot")
	if err != nil {
		return nil, nil, err
	}
	manifest := pkg
	if m, ok := pkg["manifest"]; ok {
		if mm, ok := m.(map[string]any); ok {
			manifest = mm
		}
	}
	body := map[string]any{"manifest": manifest}
	if len(roles) > 0 {
		body["roleMapping"] = roles
	}
	if len(templates) > 0 {
		body["templateMapping"] = templates
	}
	if len(slots) > 0 {
		body["notifyChoices"] = slots
	}
	if resume {
		body["resume"] = true
	}
	return body, nil, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (cs commandsList) find(name string) *command {
	for _, c := range cs {
		if c.name == name {
			return c
		}
	}
	return nil
}

type commandsList []*command

// ---------------------------------------------------------------------------
// Global flags and dispatch
// ---------------------------------------------------------------------------

type globalFlags struct {
	baseURL        string
	token          string
	tenant         string
	timeout        time.Duration
	idempotencyKey string
	raw            bool
	pretty         bool
}

func defaultGlobal() globalFlags {
	base := os.Getenv("TPCTL_BASE_URL")
	if base == "" {
		base = "http://localhost:4000"
	}
	token := os.Getenv("TPCTL_TOKEN")
	if token == "" {
		token = os.Getenv("TP_AUTH_TOKEN")
	}
	return globalFlags{
		baseURL: base,
		token:   token,
		tenant:  os.Getenv("TPCTL_TENANT"),
		timeout: 30 * time.Second,
		pretty:  true,
	}
}

// run is the whole program, minus os.Exit. Keeping it a function is what makes
// the tests able to assert on exit codes without spawning a process.
func run(argv []string, stdout, stderr io.Writer) int {
	g := defaultGlobal()
	args, code := parseGlobals(argv, &g, stderr)
	if code != exitOK {
		return code
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout, args)
		return exitOK
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintf(stdout, "tpctl %s\n", version)
		return exitOK
	}
	if args[0] == "legacy" {
		printLegacy(stdout)
		return exitOK
	}

	cs := commands()
	cmd, rest, err := pickCommand(cs, args)
	if err != nil {
		writeError(stdout, stderr, g, "", "", &apiError{
			Code: "usage", Message: err.Error(), ExitCode: exitUsage,
		})
		return exitUsage
	}

	in, perr := parseCommandFlags(cmd, rest, &g)
	if perr != nil {
		writeError(stdout, stderr, g, cmd.name, "", &apiError{
			Code: "usage", Message: perr.Error(), ExitCode: exitUsage,
		})
		return exitUsage
	}
	in.cmd = cmd
	in.global = g

	body, query, berr := buildRequest(in)
	if berr != nil {
		writeError(stdout, stderr, g, cmd.name, "", &apiError{
			Code: "usage", Message: berr.Error(), ExitCode: exitUsage,
		})
		return exitUsage
	}

	path, perr2 := fillPath(cmd, in.args)
	if perr2 != nil {
		writeError(stdout, stderr, g, cmd.name, "", &apiError{
			Code: "usage", Message: perr2.Error(), ExitCode: exitUsage,
		})
		return exitUsage
	}

	idem := ""
	if cmd.sideEffect {
		idem = g.idempotencyKey
		if idem == "" {
			idem = intentKey(intentLabel(cmd), cmd.method+" "+cmd.path, body)
		}
		// The key goes into the body as well as the header. Encore's typed
		// handlers cannot see headers, so a header-only key is a key the
		// service never reads — the retry would create a second run.
		body = withRequestID(body, idem)
	}
	requestID := newRequestID()

	c := newClient(g.baseURL, g.token, g.tenant, g.timeout)
	resp, apiErr := c.call(cmd.method, path, toValues(query), body, idem, requestID)
	if apiErr != nil {
		if resp != nil && resp.requestID != "" {
			requestID = resp.requestID
		}
		writeError(stdout, stderr, g, cmd.name, requestID, apiErr)
		return apiErr.ExitCode
	}
	if resp.requestID != "" {
		requestID = resp.requestID
	}
	if cmd.verdict != nil {
		if v := cmd.verdict(resp.body); v != nil {
			writeVerdict(stdout, stderr, g, cmd.name, requestID, resp, v)
			return v.ExitCode
		}
	}
	writeSuccess(stdout, g, cmd.name, requestID, idem, resp)
	return exitOK
}

// invalidVerdict reads the validation report and turns "valid": false into a
// failing exit code. The report itself is still printed: "why" is the useful
// part, "no" is only the signal.
func invalidVerdict(data []byte) *apiError {
	var report struct {
		Valid  bool `json:"valid"`
		Issues []struct {
			Field   string `json:"field"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil
	}
	if report.Valid {
		return nil
	}
	return &apiError{
		Code: "invalid_definition", AppCode: "invalid_definition",
		Message:  fmt.Sprintf("定义不合法：%d 处问题", len(report.Issues)),
		Fields:   toFieldErrors(report.Issues),
		ExitCode: exitInvalid,
	}
}

// incompatibleVerdict does the same for "can this body carry the device values
// that already exist?".
func incompatibleVerdict(data []byte) *apiError {
	var report struct {
		Compatible bool `json:"compatible"`
		Issues     []struct {
			Field   string `json:"field"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil
	}
	if report.Compatible {
		return nil
	}
	return &apiError{
		Code: "failed_precondition", AppCode: "binding_incompatible",
		Message:  "新版本套不上已有设备的参数值",
		Fields:   toFieldErrors(report.Issues),
		ExitCode: exitConflict,
	}
}

func toFieldErrors(issues []struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}) []fieldError {
	out := make([]fieldError, 0, len(issues))
	for _, i := range issues {
		out = append(out, fieldError{Field: i.Field, Code: i.Code, Message: i.Message})
	}
	return out
}

func buildRequest(in *invocation) (any, map[string][]string, error) {
	if in.cmd.build == nil {
		return nil, nil, nil
	}
	return in.cmd.build(in)
}

// withRequestID copies the derived idempotency key into the request body.
//
// The header stays (it is what a proxy or a log reads), but the service reads
// the body: an Encore typed handler has no access to headers, so a key that
// only travels in one is a key the service never sees.
func withRequestID(body any, key string) any {
	m, ok := body.(map[string]any)
	if !ok || key == "" {
		return body
	}
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out["request_id"] = key
	return out
}

// intentLabel names an endpoint in the key by its first spelling, so an alias
// and its replacement do not produce two different keys for one request.
func intentLabel(cmd *command) string {
	if cmd.legacy {
		for _, c := range commands() {
			if c.method == cmd.method && c.path == cmd.path && !c.legacy {
				return c.name
			}
		}
	}
	return cmd.name
}

func fillPath(cmd *command, args []string) (string, error) {
	if len(args) < len(cmd.args) {
		return "", fmt.Errorf("%s 需要参数: %s", cmd.name, strings.Join(cmd.args, " "))
	}
	path := cmd.path
	for i, name := range cmd.args {
		if strings.TrimSpace(args[i]) == "" {
			// An empty id would turn /v1/rules/:id/enabled into
			// /v1/rules//enabled, which is a 404 that reads like "this rule
			// does not exist" instead of "you forgot the id".
			return "", fmt.Errorf("%s 的 <%s> 不能为空", cmd.name, name)
		}
		path = strings.Replace(path, ":"+name, args[i], 1)
	}
	return path, nil
}

func parseGlobals(argv []string, g *globalFlags, stderr io.Writer) ([]string, int) {
	var out []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if !strings.HasPrefix(a, "--") {
			out = append(out, argv[i:]...)
			break
		}
		name, value, hasValue := splitFlag(a)
		next := func() (string, bool) {
			if hasValue {
				return value, true
			}
			if i+1 < len(argv) {
				i++
				return argv[i], true
			}
			return "", false
		}
		switch name {
		case "base-url":
			v, ok := next()
			if !ok {
				return nil, usageErr(stderr, "--base-url 需要值")
			}
			g.baseURL = v
		case "token":
			v, ok := next()
			if !ok {
				return nil, usageErr(stderr, "--token 需要值")
			}
			g.token = v
		case "tenant":
			v, ok := next()
			if !ok {
				return nil, usageErr(stderr, "--tenant 需要值")
			}
			g.tenant = v
		case "timeout":
			v, ok := next()
			if !ok {
				return nil, usageErr(stderr, "--timeout 需要值")
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, usageErr(stderr, "--timeout 需要 Go duration，例如 30s")
			}
			g.timeout = d
		case "idempotency-key":
			v, ok := next()
			if !ok {
				return nil, usageErr(stderr, "--idempotency-key 需要值")
			}
			g.idempotencyKey = v
		case "raw":
			g.raw = true
		case "pretty":
			g.pretty = true
		case "compact":
			g.pretty = false
		case "help", "h":
			out = append(out, "help")
			return out, exitOK
		default:
			// Not a global flag: let the command parser deal with it (and
			// complain about it if it does not know it).
			out = append(out, argv[i:]...)
			return out, exitOK
		}
	}
	return out, exitOK
}

func usageErr(stderr io.Writer, msg string) int {
	fmt.Fprintln(stderr, "用法错误: "+msg)
	return exitUsage
}

func splitFlag(a string) (name, value string, hasValue bool) {
	s := strings.TrimLeft(a, "-")
	if i := strings.Index(s, "="); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

// pickCommand resolves "rules list" and "rules.list" to the same row, and
// reports an unknown command with the closest names instead of a stack trace.
func pickCommand(cs commandsList, args []string) (*command, []string, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("缺少子命令")
	}
	candidates := []string{args[0]}
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		candidates = append(candidates, args[0]+"."+args[1])
	}
	for _, name := range candidates {
		if c := cs.find(name); c != nil {
			consumed := strings.Count(name, ".") + 1
			return c, args[consumed:], nil
		}
	}
	return nil, nil, fmt.Errorf("未知子命令 %q，试试 `tpctl help`", strings.Join(args[:min(2, len(args))], " "))
}

// parseCommandFlags reads a command's flags.
//
// Global flags are accepted here too, not just before the command name: people
// write `tpctl rules list --raw` and `tpctl --raw rules list` interchangeably,
// and refusing the first spelling is a papercut with no upside.
func parseCommandFlags(cmd *command, args []string, g *globalFlags) (*invocation, error) {
	in := &invocation{values: map[string][]string{}}
	spec := map[string]flagSpec{}
	for _, f := range cmd.flags {
		spec[f.name] = f
		if f.def != "" && f.kind != "bool" {
			in.values[f.name] = []string{f.def}
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			in.args = append(in.args, a)
			continue
		}
		name, value, hasValue := splitFlag(a)
		if isGlobalFlag(name) {
			consumed, err := applyGlobalFlag(name, value, hasValue, args, &i, g)
			if err != nil {
				return nil, err
			}
			_ = consumed
			continue
		}
		f, ok := spec[name]
		if !ok {
			return nil, fmt.Errorf("%s 不认识 --%s", cmd.name, name)
		}
		if f.kind == "bool" {
			if !hasValue {
				value = "true"
			}
			if _, err := strconv.ParseBool(value); err != nil {
				return nil, fmt.Errorf("--%s 需要 true/false", name)
			}
			in.values[name] = []string{value}
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s 需要值", name)
			}
			i++
			value = args[i]
		}
		in.values[name] = append(in.values[name], value)
	}
	return in, nil
}

// toValues keeps the query as url.Values, which is the same type with a
// different name; naming it here means the command table never has to import
// net/url just to describe a filter.
// isGlobalFlag reports whether a flag belongs to the program rather than to a
// command. Keeping one list means the two parsing paths cannot drift.
func isGlobalFlag(name string) bool {
	switch name {
	case "base-url", "token", "tenant", "timeout", "idempotency-key",
		"raw", "pretty", "compact":
		return true
	}
	return false
}

// applyGlobalFlag sets one global flag found after the command name. `i` is the
// caller's loop index, advanced when the value is the next argument.
func applyGlobalFlag(name, value string, hasValue bool, args []string, i *int, g *globalFlags) (bool, error) {
	next := func() (string, error) {
		if hasValue {
			return value, nil
		}
		if *i+1 < len(args) {
			*i++
			return args[*i], nil
		}
		return "", fmt.Errorf("--%s 需要值", name)
	}
	switch name {
	case "base-url":
		v, err := next()
		if err != nil {
			return false, err
		}
		g.baseURL = v
	case "token":
		v, err := next()
		if err != nil {
			return false, err
		}
		g.token = v
	case "tenant":
		v, err := next()
		if err != nil {
			return false, err
		}
		g.tenant = v
	case "timeout":
		v, err := next()
		if err != nil {
			return false, err
		}
		d, derr := time.ParseDuration(v)
		if derr != nil {
			return false, fmt.Errorf("--timeout 需要 Go duration，例如 30s")
		}
		g.timeout = d
	case "idempotency-key":
		v, err := next()
		if err != nil {
			return false, err
		}
		g.idempotencyKey = v
	case "raw":
		g.raw = true
	case "pretty":
		g.pretty = true
	case "compact":
		g.pretty = false
	}
	return true, nil
}

func toValues(q map[string][]string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = v
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

type envelope struct {
	OK             bool            `json:"ok"`
	Command        string          `json:"command"`
	RequestID      string          `json:"request_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	HTTPStatus     int             `json:"http_status,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	Error          *apiError       `json:"error,omitempty"`
}

func writeSuccess(w io.Writer, g globalFlags, command, requestID, idem string, resp *response) {
	if g.raw {
		w.Write(resp.body)
		if len(resp.body) > 0 && resp.body[len(resp.body)-1] != '\n' {
			fmt.Fprintln(w)
		}
		return
	}
	env := envelope{
		OK: true, Command: command, RequestID: requestID,
		IdempotencyKey: idem, HTTPStatus: resp.status,
	}
	if len(resp.body) > 0 {
		env.Data = json.RawMessage(resp.body)
	}
	writeJSON(w, env, g.pretty)
}

// writeVerdict reports a 200 response whose content means "no". Both halves
// are kept: the data says why, the error says which way to branch.
func writeVerdict(stdout, stderr io.Writer, g globalFlags, command, requestID string,
	resp *response, e *apiError) {
	env := envelope{
		OK: false, Command: command, RequestID: requestID,
		HTTPStatus: resp.status, Error: e,
	}
	if len(resp.body) > 0 {
		env.Data = json.RawMessage(resp.body)
	}
	writeJSON(stdout, env, g.pretty)
	if e.Message != "" {
		fmt.Fprintln(stderr, "错误: "+e.Message)
	}
	for _, f := range e.Fields {
		fmt.Fprintf(stderr, "  - %s (%s): %s\n", f.Field, f.Code, f.Message)
	}
}

func writeError(stdout, stderr io.Writer, g globalFlags, command, requestID string, e *apiError) {
	if e.Hint != "" {
		fmt.Fprintln(stderr, "提示: "+e.Hint)
	}
	env := envelope{
		OK: false, Command: command, RequestID: requestID, Error: e,
	}
	// Errors go to stdout as JSON so `tpctl ... | jq` works even when the
	// command failed; the human message also goes to stderr so an interactive
	// user sees it without piping.
	writeJSON(stdout, env, g.pretty)
	if e.Message != "" {
		fmt.Fprintln(stderr, "错误: "+e.Message)
	}
	for _, f := range e.Fields {
		fmt.Fprintf(stderr, "  - %s (%s): %s\n", f.Field, f.Code, f.Message)
	}
}

func writeJSON(w io.Writer, v any, pretty bool) {
	var raw []byte
	var err error
	if pretty {
		raw, err = json.MarshalIndent(v, "", "  ")
	} else {
		raw, err = json.Marshal(v)
	}
	if err != nil {
		fmt.Fprintln(w, `{"ok":false,"error":{"code":"encode","message":"结果无法序列化"}}`)
		return
	}
	w.Write(raw)
	fmt.Fprintln(w)
}

func printHelp(w io.Writer, args []string) {
	cs := commands()
	if len(args) > 1 {
		if c := cs.find(args[1]); c != nil {
			printCommandHelp(w, c)
			return
		}
	}
	fmt.Fprintf(w, `tpctl %s — ThingsPanel 自动化命令行

用法:
  tpctl [全局参数] <组> <动作> [参数] [--标志]

全局参数:
  --base-url URL      服务地址（默认 $TPCTL_BASE_URL 或 http://localhost:4000）
  --token TOKEN       访问令牌（默认 $TPCTL_TOKEN）
  --tenant ID         租户（默认 $TPCTL_TENANT）
  --timeout 30s       单次请求超时
  --idempotency-key K 固定幂等键（默认按“命令+请求体”推导）
  --raw               只输出服务端原始 JSON，不包信封
  --compact           单行 JSON

退出码:
  0 成功   2 用法错误   3 不存在   4 参数/校验失败
  5 无权限 6 冲突（版本/预览过期） 7 结果未知（可能已生效） 8 服务不可用

命令组:
  rules      list get create update enable disable delete validate
             simulate preview check explain run runs
  runs       get
  bindings   list attach detach device template reconcile
  bundles    export preview import imports importGet
  alarms     list get ack transitions
  blueprints list preview instantiate

  tpctl legacy      旧命令（automations …）到新命令的映射表
  tpctl help <命令> 单个命令的参数说明
`, version)
}

func printCommandHelp(w io.Writer, c *command) {
	fmt.Fprintf(w, "tpctl %s — %s\n\n用法: tpctl %s", c.name, c.summary,
		strings.ReplaceAll(c.name, ".", " "))
	for _, a := range c.args {
		fmt.Fprintf(w, " <%s>", a)
	}
	fmt.Fprintln(w)
	if len(c.flags) > 0 {
		fmt.Fprintln(w, "\n标志:")
		sorted := append([]flagSpec{}, c.flags...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
		for _, f := range sorted {
			def := ""
			if f.def != "" {
				def = "（默认 " + f.def + "）"
			}
			fmt.Fprintf(w, "  --%-16s %s%s\n", f.name, f.help, def)
		}
	}
	if c.sideEffect {
		fmt.Fprintln(w, "\n这是会改变状态的命令：默认按“命令+请求体”推导幂等键，重试不会重复生效。")
	}
}

func printLegacy(w io.Writer) {
	fmt.Fprint(w, `旧命令映射（脚本可以不改，也可以逐步迁移）

  tpctl automations create --file rule.json   →  tpctl rules create --file rule.json
  tpctl automations list                      →  tpctl rules list
  tpctl automations switch <id> [--off]       →  tpctl rules enable|disable <id>
  tpctl automations delete <id>               →  tpctl rules delete <id>

两者的请求、校验、幂等与错误码完全一致：旧名只是新名的一层壳，没有第二套实现。
区别只有一个——旧名不带新的可选参数（--version/--force/--preview-token），
需要这些能力时改用新名。
`)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

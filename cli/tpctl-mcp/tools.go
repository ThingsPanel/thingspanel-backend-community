package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Tool table
// ---------------------------------------------------------------------------
//
// One row per task-level action, matching the CLI one for one. A tool is not a
// thin alias of an endpoint: it is the shape an assistant should think in
// ("preview this change", "why did it not run"), and the endpoint behind it is
// the same one the web page calls.
//
// Two rules hold for every row:
//
//   - the tenant never comes from the arguments. It comes from the token the
//     server was started with, because a model must not be able to choose which
//     customer it is acting for.
//   - readOnly tools are the ones that cannot move a device. They carry no
//     idempotency key, because a read that is retried twice is not a problem.

type prop struct {
	name  string
	typ   string
	desc  string
	enum  []string
	items string
}

type tool struct {
	name        string
	description string
	readOnly    bool
	props       []prop
	required    []string
	call        func(c *client, args map[string]any) (any, *apiError)
}

func definitionProp() prop {
	return prop{
		name: "definition", typ: "object",
		desc: "规则正文（kind/mode/scope/trigger/freshness/targets/steps）。与网页表单是同一个结构。",
	}
}

func tools() []*tool {
	return []*tool{
		{
			name: "list_rules", readOnly: true,
			description: "列出规则（分页）。回答“有哪些规则”“这台设备挂了哪些规则”。",
			props: []prop{
				{name: "kind", typ: "string", desc: "automation | alarm | scene"},
				{name: "device_id", typ: "string", desc: "按目标设备过滤"},
				{name: "device_config_id", typ: "string", desc: "按设备模板过滤"},
				{name: "name", typ: "string", desc: "名称包含"},
				{name: "enabled", typ: "string", desc: "true | false"},
				{name: "limit", typ: "integer", desc: "默认 50，上限 200"},
				{name: "offset", typ: "integer", desc: "分页偏移"},
			},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				q := url.Values{}
				for _, k := range []string{"kind", "name", "enabled"} {
					if v := r.str(k, false); v != "" {
						q.Set(k, v)
					}
				}
				if v := r.str("device_id", false); v != "" {
					q.Set("device_id", v)
				}
				if v := r.str("device_config_id", false); v != "" {
					q.Set("device_config_id", v)
				}
				if n := r.num("limit"); n > 0 {
					q.Set("limit", strconv.Itoa(n))
				}
				if n := r.num("offset"); n > 0 {
					q.Set("offset", strconv.Itoa(n))
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/rules", q)
			},
		},
		{
			name: "get_rule", readOnly: true,
			description: "读取一条规则（含正文、版本、修订）。改之前先读，拿到 version。",
			props:       []prop{{name: "rule_id", typ: "string", desc: "规则 id"}},
			required:    []string{"rule_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/rules/"+id, nil)
			},
		},
		{
			name: "validate_rule", readOnly: true,
			description: "只校验不保存。返回 valid 与字段级问题；与网页提交时同一套校验。",
			props:       []prop{definitionProp()},
			required:    []string{"definition"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				def := r.obj("definition", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return postJSON(c, "/v1/rule-validation", map[string]any{"definition": def}, "")
			},
		},
		{
			name: "preview_rule_change", readOnly: true,
			description: "变更预览：这次改动会影响哪些设备、依赖是否还在、会不会让设备上已存的值失效，" +
				"并返回一个预览令牌。保存时带上令牌，过期或内容变了会被拒。只算不存。",
			props:    []prop{{name: "rule_id", typ: "string", desc: "规则 id"}, definitionProp()},
			required: []string{"rule_id", "definition"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				def := r.obj("definition", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return postJSON(c, "/v1/rules/"+id+"/change-preview",
					map[string]any{"definition": def}, "")
			},
		},
		{
			name: "create_rule",
			description: "创建规则。定义不合法会被拒，返回字段级问题。",
			props: []prop{
				{name: "name", typ: "string", desc: "规则名称"},
				definitionProp(),
				{name: "enabled", typ: "boolean", desc: "创建后是否启用"},
				{name: "legacy_id", typ: "string", desc: "迁移用：旧 id"},
			},
			required: []string{"name", "definition"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				body := map[string]any{
					"name":       r.str("name", true),
					"definition": r.obj("definition", true),
					"enabled":    r.boolean("enabled"),
				}
				if v := r.str("legacy_id", false); v != "" {
					body["legacy_id"] = v
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return writeJSON(c, "/v1/rules", body, intentKey("create_rule", args))
			},
		},
		{
			name: "update_rule",
			description: "更新规则。version 是乐观锁；带上 preview_token 时，过期或与当前内容不一致会被拒。" +
				"force 只在明知会让设备值失效时使用。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				definitionProp(),
				{name: "name", typ: "string", desc: "新名称（可省略）"},
				{name: "version", typ: "integer", desc: "客户端最后看到的版本"},
				{name: "preview_token", typ: "string", desc: "变更预览返回的令牌"},
				{name: "force", typ: "boolean", desc: "明知有绑定会失效也要保存"},
			},
			required: []string{"rule_id", "definition", "version"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				body := map[string]any{
					"definition": r.obj("definition", true),
					"version":    r.num("version"),
				}
				if v := r.str("name", false); v != "" {
					body["name"] = v
				}
				if v := r.str("preview_token", false); v != "" {
					body["previewToken"] = v
				}
				if r.boolean("force") {
					body["force"] = true
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return putJSON(c, "/v1/rules/"+id, body, intentKey("update_rule", args))
			},
		},
		{
			name: "set_rule_enabled",
			description: "启用或停用规则。停用不会删除运行记录，历史仍然可查。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "enabled", typ: "boolean", desc: "true 启用，false 停用"},
				{name: "version", typ: "integer", desc: "乐观锁版本"},
			},
			required: []string{"rule_id", "enabled"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				body := map[string]any{"enabled": r.boolean("enabled")}
				if n := r.num("version"); n > 0 {
					body["version"] = n
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return writeJSON(c, "/v1/rules/"+id+"/enabled", body, intentKey("set_rule_enabled", args))
			},
		},
		{
			name: "delete_rule",
			description: "删除规则（软删）。被别的规则引用时会拒绝并点名引用者；force 才越权。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "force", typ: "boolean", desc: "明知有引用也要删"},
			},
			required: []string{"rule_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				force := r.boolean("force")
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return deleteJSON(c, "/v1/rules/"+id,
					map[string]any{"force": force}, intentKey("delete_rule", args))
			},
		},
		{
			name: "simulate_rule", readOnly: true,
			description: "无副作用模拟：给一组虚拟样本，回答“会不会触发、会执行哪些步骤、走到哪个分支”。" +
				"不会控设备、不会发通知。要看真实效果请用 run_rule。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "samples", typ: "array", items: "object",
					desc: "样本数组：{at, device_id, signal, key, value, unit?, quality?}，at 形如 0s / 60s / RFC3339"},
			},
			required: []string{"rule_id", "samples"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				samples := r.listOfObjects("samples")
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return postJSON(c, "/v1/rules/"+id+"/simulate",
					map[string]any{"samples": samples}, "")
			},
		},
		{
			name: "explain_rule", readOnly: true,
			description: "解释“为什么没跑”。没有运行记录时也能回答（无触发、数据过期、条件不满足）。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "device_id", typ: "string", desc: "哪台设备"},
				{name: "from", typ: "string", desc: "RFC3339"},
				{name: "to", typ: "string", desc: "RFC3339"},
			},
			required: []string{"rule_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				q := url.Values{}
				if v := r.str("device_id", false); v != "" {
					q.Set("device_id", v)
				}
				if v := r.str("from", false); v != "" {
					q.Set("from", v)
				}
				if v := r.str("to", false); v != "" {
					q.Set("to", v)
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/rules/"+id+"/explain", q)
			},
		},
		{
			name: "run_rule",
			description: "真实执行一次：会下发设备命令、会开告警、会发通知。只想看效果请用 simulate_rule。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "device_id", typ: "string", desc: "哪台设备（规则已限定目标时可省略）"},
			},
			required: []string{"rule_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				body := map[string]any{}
				if v := r.str("device_id", false); v != "" {
					body["device_id"] = v
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return writeJSON(c, "/v1/rules/"+id+"/run", body, intentKey("run_rule", args))
			},
		},
		{
			name: "list_runs", readOnly: true,
			description: "运行日志（分页 + 状态集合 + 时间窗）。排障先看这里，再看 get_run。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "status", typ: "string", desc: "逗号分隔，如 failed,expired"},
				{name: "limit", typ: "integer", desc: "默认 50，上限 200"},
				{name: "offset", typ: "integer"},
				{name: "before", typ: "string", desc: "RFC3339"},
				{name: "after", typ: "string", desc: "RFC3339"},
			},
			required: []string{"rule_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				q := url.Values{}
				for _, k := range []string{"status", "before", "after"} {
					if v := r.str(k, false); v != "" {
						q.Set(k, v)
					}
				}
				if n := r.num("limit"); n > 0 {
					q.Set("limit", strconv.Itoa(n))
				}
				if n := r.num("offset"); n > 0 {
					q.Set("offset", strconv.Itoa(n))
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/rules/"+id+"/runs", q)
			},
		},
		{
			name: "get_run", readOnly: true,
			description: "一次运行的详情：每一步的状态、冻结的目标快照、每个出站意图到底走到哪一步。",
			props:       []prop{{name: "run_id", typ: "string", desc: "运行 id"}},
			required:    []string{"run_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("run_id", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/runs/"+id, nil)
			},
		},
		{
			name: "list_bindings", readOnly: true,
			description: "一条规则挂到了哪些设备/模板，每台的参数覆盖值是多少。",
			props:       []prop{{name: "rule_id", typ: "string", desc: "规则 id"}},
			required:    []string{"rule_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/rules/"+id+"/bindings", nil)
			},
		},
		{
			name: "attach_binding",
			description: "把规则挂到一台设备或一个模板，可带参数覆盖。挂模板后新设备自动继承。",
			props: []prop{
				{name: "rule_id", typ: "string", desc: "规则 id"},
				{name: "scope_type", typ: "string", desc: "device | device_config", enum: []string{"device", "device_config"}},
				{name: "scope_id", typ: "string", desc: "设备 id 或模板 id"},
				{name: "params", typ: "object", desc: "参数覆盖，如 {\"limit\":\"35\"}"},
			},
			required: []string{"rule_id", "scope_type", "scope_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("rule_id", true)
				scope := r.str("scope_type", true)
				scopeID := r.str("scope_id", true)
				params := r.strMap("params")
				if scope != "" && scope != "device" && scope != "device_config" {
					r.errs = append(r.errs, fieldError{
						Field: "scope_type", Code: "enum", Message: "只能是 device 或 device_config"})
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				body := map[string]any{"scope_type": scope, "scope_id": scopeID, "params": params}
				return writeJSON(c, "/v1/rules/"+id+"/bindings", body, intentKey("attach_binding", args))
			},
		},
		{
			name: "detach_binding",
			description: "脱离绑定。保留证据（detached_at），不删行，历史仍能回答“以前谁驱动它”。",
			props:       []prop{{name: "binding_id", typ: "string", desc: "绑定 id"}},
			required:    []string{"binding_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("binding_id", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return deleteJSON(c, "/v1/bindings/"+id, nil, intentKey("detach_binding", args))
			},
		},
		{
			name: "device_automation", readOnly: true,
			description: "这台设备真正生效的规则：直接绑的、从模板继承的，以及各自最终用的参数值。",
			props:       []prop{{name: "device_id", typ: "string", desc: "设备 id"}},
			required:    []string{"device_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("device_id", true)
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/device-automation/"+id, nil)
			},
		},
		{
			name: "list_alarms", readOnly: true,
			description: "查告警（按策略/对象/状态）。active 表示还在响。",
			props: []prop{
				{name: "policy_id", typ: "string", desc: "告警策略 id"},
				{name: "subject_id", typ: "string", desc: "对象（通常是设备）id"},
				{name: "state", typ: "string", desc: "active | cleared | acked"},
				{name: "limit", typ: "integer"},
			},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				q := url.Values{}
				if v := r.str("policy_id", false); v != "" {
					q.Set("policy_id", v)
				}
				if v := r.str("subject_id", false); v != "" {
					q.Set("subject_id", v)
				}
				if v := r.str("state", false); v != "" {
					q.Set("state", v)
				}
				if n := r.num("limit"); n > 0 {
					q.Set("limit", strconv.Itoa(n))
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return getJSON(c, "/v1/alarms", q)
			},
		},
		{
			name: "ack_alarm",
			description: "确认告警。只记录“有人知道了”，不改变“异常还在”这个事实。",
			props: []prop{
				{name: "alarm_id", typ: "string", desc: "告警 id"},
				{name: "actor", typ: "string", desc: "谁确认的"},
				{name: "note", typ: "string", desc: "备注"},
			},
			required: []string{"alarm_id"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				id := r.str("alarm_id", true)
				body := map[string]any{}
				if v := r.str("actor", false); v != "" {
					body["actor"] = v
				}
				if v := r.str("note", false); v != "" {
					body["note"] = v
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				return writeJSON(c, "/v1/alarms/"+id+"/ack", body, intentKey("ack_alarm", args))
			},
		},
		{
			name: "list_blueprints", readOnly: true,
			description: "能力与模板发现：有哪些内置模板、各自要哪些参数。不确定怎么配时先看这里。",
			call: func(c *client, args map[string]any) (any, *apiError) {
				return getJSON(c, "/v1/blueprints", nil)
			},
		},
		{
			name: "export_bundle", readOnly: true,
			description: "把一组规则打成资源包。设备以角色出包、通知以插槽出包，所以能装进别的环境。",
			props: []prop{
				{name: "rule_ids", typ: "array", items: "string", desc: "要导出的规则 id"},
				{name: "bundle_key", typ: "string", desc: "包的业务键"},
				{name: "version", typ: "string", desc: "包版本"},
			},
			required: []string{"rule_ids"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				r := &argReader{args: args}
				ids := r.strList("rule_ids")
				body := map[string]any{"ruleIds": ids}
				if v := r.str("bundle_key", false); v != "" {
					body["bundleKey"] = v
				}
				if v := r.str("version", false); v != "" {
					body["version"] = v
				}
				if err := r.failed(); err != nil {
					return nil, err.(*apiError)
				}
				if len(ids) == 0 {
					return nil, &apiError{Code: "invalid_arguments", Message: "rule_ids 不能为空"}
				}
				return postJSON(c, "/v1/bundles/export", body, "")
			},
		},
		{
			name: "preview_bundle", readOnly: true,
			description: "预演导入：这个包在目标环境会新建/更新/跳过什么，缺哪些角色和插槽。不写库。",
			props:       bundleProps(),
			required:    []string{"manifest"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				body, apiErr := bundleBody(args, false)
				if apiErr != nil {
					return nil, apiErr
				}
				return postJSON(c, "/v1/bundles/preview", body, "")
			},
		},
		{
			name: "import_bundle",
			description: "导入资源包。会写库。失败时用 resume 续跑，同 content hash 第二次导入返回第一次的结果。",
			props: append(bundleProps(), prop{name: "resume", typ: "boolean", desc: "续跑上次失败的导入"}),
			required: []string{"manifest"},
			call: func(c *client, args map[string]any) (any, *apiError) {
				body, apiErr := bundleBody(args, true)
				if apiErr != nil {
					return nil, apiErr
				}
				return writeJSON(c, "/v1/bundles/import", body, intentKey("import_bundle", args))
			},
		},
	}
}

func bundleProps() []prop {
	return []prop{
		{name: "manifest", typ: "object", desc: "资源包（export_bundle 的输出）"},
		{name: "role_mapping", typ: "object", desc: "角色 → 设备 id 数组，如 {\"door\":[\"dev-1\"]}"},
		{name: "template_mapping", typ: "object", desc: "模板键 → 本地模板 id"},
		{name: "notify_choices", typ: "object", desc: "插槽 → 本地通知策略 id"},
	}
}

func bundleBody(args map[string]any, withResume bool) (map[string]any, *apiError) {
	r := &argReader{args: args}
	manifest := r.obj("manifest", true)
	roles := r.strListMap("role_mapping")
	templates := r.strMap("template_mapping")
	slots := r.strMap("notify_choices")
	resume := false
	if withResume {
		resume = r.boolean("resume")
	}
	if err := r.failed(); err != nil {
		return nil, err.(*apiError)
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
	return body, nil
}

// strListMap reads a map whose values are string arrays (role → devices).
func (r *argReader) strListMap(name string) map[string][]string {
	v, ok := r.args[name]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		r.errs = append(r.errs, fieldError{Field: name, Code: "type", Message: "需要对象"})
		return nil
	}
	out := map[string][]string{}
	for k, raw := range m {
		switch val := raw.(type) {
		case string:
			out[k] = []string{val}
		case []any:
			var ids []string
			for _, item := range val {
				if s, ok := item.(string); ok {
					ids = append(ids, s)
				}
			}
			out[k] = ids
		default:
			r.errs = append(r.errs, fieldError{Field: name + "." + k, Code: "type", Message: "需要字符串或字符串数组"})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func getJSON(c *client, path string, q url.Values) (any, *apiError) {
	return doJSON(c, http.MethodGet, path, q, nil, "")
}

func postJSON(c *client, path string, body any, idem string) (any, *apiError) {
	return doJSON(c, http.MethodPost, path, nil, body, idem)
}

func writeJSON(c *client, path string, body any, idem string) (any, *apiError) {
	return doJSON(c, http.MethodPost, path, nil, withRequestID(body, idem), idem)
}

// putJSON is separate from writeJSON because the save endpoint declares
// method=PUT. Sending POST there returns 404 "endpoint not found", which reads
// like "this rule does not exist" — the most misleading error there is.
func putJSON(c *client, path string, body any, idem string) (any, *apiError) {
	return doJSON(c, http.MethodPut, path, nil, withRequestID(body, idem), idem)
}

func deleteJSON(c *client, path string, body any, idem string) (any, *apiError) {
	return doJSON(c, http.MethodDelete, path, nil, withRequestID(body, idem), idem)
}

// withRequestID puts the derived key where the service can actually read it.
//
// The header is kept for proxies and logs, but an Encore typed handler never
// sees headers — so a key that only travels in one is a key the service never
// reads, and a retried run becomes a second run.
func withRequestID(body any, key string) any {
	if key == "" {
		return body
	}
	m, ok := body.(map[string]any)
	if !ok {
		return body
	}
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out["request_id"] = key
	return out
}

func doJSON(c *client, method, path string, q url.Values, body any, idem string) (any, *apiError) {
	resp, apiErr := c.call(method, path, q, body, idem)
	if apiErr != nil {
		return nil, apiErr
	}
	if len(resp.body) == 0 {
		return map[string]any{}, nil
	}
	var out any
	if err := json.Unmarshal(resp.body, &out); err != nil {
		return map[string]any{"raw": string(resp.body)}, nil
	}
	return out, nil
}

// toolSchema builds the JSON Schema a host shows the model.
func toolSchema(t *tool) map[string]any {
	props := map[string]any{}
	names := make([]string, 0, len(t.props))
	for _, p := range t.props {
		names = append(names, p.name)
		entry := map[string]any{"type": p.typ}
		if p.desc != "" {
			entry["description"] = p.desc
		}
		if len(p.enum) > 0 {
			entry["enum"] = p.enum
		}
		if p.typ == "array" && p.items != "" {
			entry["items"] = map[string]any{"type": p.items}
		}
		props[p.name] = entry
	}
	sort.Strings(names)
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(t.required) > 0 {
		schema["required"] = t.required
	}
	return schema
}

// toolDescriptor is one entry of tools/list.
func toolDescriptor(t *tool) map[string]any {
	annotations := map[string]any{
		"title":           t.name,
		"readOnlyHint":    t.readOnly,
		"destructiveHint": !t.readOnly && strings.Contains(t.name, "delete"),
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
	return map[string]any{
		"name":        t.name,
		"description": t.description,
		"inputSchema": toolSchema(t),
		"annotations": annotations,
	}
}

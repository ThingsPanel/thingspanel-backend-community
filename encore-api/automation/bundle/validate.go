package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"encore.app/automation/domain"
)

// ---------------------------------------------------------------------------
// Validation: is this a well-formed package?
// ---------------------------------------------------------------------------
//
// Structural validation answers questions the package can answer on its own:
// are the keys unique, do the internal references close, does every binding
// name a role the manifest declares. It says nothing about the target
// environment — that is Plan's job.

// Validate checks the manifest as a document.
func Validate(m *Manifest) []domain.Issue {
	var out []domain.Issue
	add := func(field, code, msg string) {
		out = append(out, domain.Issue{Field: field, Code: code, Message: msg})
	}
	if m == nil {
		add("manifest", "empty_manifest", "没有包内容")
		return out
	}
	if m.ContractVersion != ContractVersion {
		add("contractVersion", "unsupported_contract",
			fmt.Sprintf("包格式版本是 %q，本版本只认识 %s", m.ContractVersion, ContractVersion))
	}
	if strings.TrimSpace(m.BundleKey) == "" {
		add("bundleKey", "missing_bundle_key", "包必须有一个稳定的 bundleKey，否则重复导入无法识别")
	}
	if strings.TrimSpace(m.Version) == "" {
		add("version", "missing_version", "包必须有版本号")
	}
	if len(m.Resources.Rules) == 0 {
		add("resources.rules", "empty_bundle", "包里没有任何规则")
	}

	seen := map[string]bool{}
	for i, r := range m.Resources.Rules {
		field := fmt.Sprintf("resources.rules[%d]", i)
		if strings.TrimSpace(r.ResourceKey) == "" {
			add(field+".resourceKey", "missing_resource_key",
				"每条规则都要有稳定的 resourceKey；用行 id 会让换环境导入变成新建")
		} else if seen[r.ResourceKey] {
			add(field+".resourceKey", "duplicate_resource_key",
				"resourceKey 重复："+r.ResourceKey)
		}
		seen[r.ResourceKey] = true

		if !validKind(r.Kind) {
			add(field+".kind", "invalid_kind", "未知的规则类型："+string(r.Kind))
		}
		if r.Hash != "" && r.Hash != Hash(r.Definition) {
			add(field+".hash", "hash_mismatch",
				"规则正文与包内记录的 hash 不一致，包可能被改过")
		}
		// A package is not a place to smuggle an invalid rule in. If the body
		// would not publish locally, it does not import either.
		if verr := domain.Validate(&r.Definition); verr != nil {
			var ve *domain.ValidationError
			if errors.As(verr, &ve) {
				for _, i := range ve.Issues {
					out = append(out, domain.Issue{
						Field: field + ".definition." + i.Field, Code: i.Code, Message: i.Message,
					})
				}
			} else {
				add(field+".definition", "invalid_definition", verr.Error())
			}
		}
		// A parameter with a default can legitimately run on that default; one
		// without a default has no value at all, and the rule would publish with
		// an empty placeholder.
		if len(r.Bindings) == 0 {
			for _, p := range r.Definition.Params {
				if strings.TrimSpace(p.Default) == "" {
					add(field+".bindings", "params_without_binding",
						"参数 "+p.Key+" 没有默认值，也没有任何作用域提供值")
				}
			}
		}

	}

	roles := map[string]bool{}
	for _, r := range m.Dependencies.Roles {
		if r.Key == "" {
			add("dependencies.roles", "missing_role_key", "角色必须有 key")
			continue
		}
		if roles[r.Key] {
			add("dependencies.roles", "duplicate_role_key", "角色重复："+r.Key)
		}
		roles[r.Key] = true
	}

	slots := map[string]bool{}
	for _, s := range m.Resources.NotifySlots {
		if s.SlotKey == "" {
			add("resources.notifySlots", "missing_slot_key", "通知插槽必须有 slotKey")
			continue
		}
		if slots[s.SlotKey] {
			add("resources.notifySlots", "duplicate_slot_key", "通知插槽重复："+s.SlotKey)
		}
		slots[s.SlotKey] = true
	}

	// Every binding and every target must name a declared role, and every
	// definition's slot must be declared. An undeclared reference is the package
	// saying "trust me".
	for i, r := range m.Resources.Rules {
		for j, b := range r.Bindings {
			if !roles[b.ScopeRole] {
				add(fmt.Sprintf("resources.rules[%d].bindings[%d].scopeRole", i, j), "undeclared_role",
					"绑定的作用域角色没有在 dependencies.roles 里声明："+b.ScopeRole)
			}
		}
		for j, tr := range r.TargetRoles {
			if !roles[tr] {
				add(fmt.Sprintf("resources.rules[%d].targetRoles[%d]", i, j), "undeclared_role",
					"目标角色没有在 dependencies.roles 里声明："+tr)
			}
		}
		for _, key := range DefinitionSlots(r.Definition) {
			if !slots[key] {
				add(fmt.Sprintf("resources.rules[%d]", i), "undeclared_slot",
					"规则引用了未声明的通知插槽："+SlotPrefix+key)
			}
		}
	}

	// Internal references have to close inside the package, otherwise the
	// import produces a rule that can never succeed.
	keys := map[string]bool{}
	for _, r := range m.Resources.Rules {
		keys[r.ResourceKey] = true
	}
	for i, r := range m.Resources.Rules {
		for _, e := range DefinitionRefs(r.Definition) {
			if !keys[e.To] {
				add(fmt.Sprintf("resources.rules[%d]", i), "unresolved_reference",
					"这条规则引用的 "+e.Field+" 不在包内："+e.To+"；导入后会变成永远失败的步骤")
			}
		}
	}
	for i, e := range m.Dependencies.Edges {
		if !keys[e.From] || !keys[e.To] {
			add(fmt.Sprintf("dependencies.edges[%d]", i), "dangling_edge",
				"依赖边指向包外："+e.From+" -> "+e.To)
		}
	}

	// Security: the promise is structural, so the only way to break it is to
	// declare it wrongly.
	if m.Security.ContainsSecrets {
		add("security.containsSecrets", "bundle_has_secrets",
			"包里含有凭据；凭据属于环境，不属于模板")
	}
	if m.Security.ContainsRecipients {
		add("security.containsRecipients", "bundle_has_recipients",
			"包里含有实际接收人；导入后应当由目标环境自己选择通知策略")
	}
	if m.Security.ContainsRuntimeData {
		add("security.containsRuntimeData", "bundle_has_runtime_data",
			"包里含有运行记录或活动告警；这些数据换环境后没有意义")
	}
	if leaked := ScanSecrets(m); len(leaked) > 0 {
		add("security.containsSecrets", "secret_in_definition",
			"规则正文里看起来有凭据："+strings.Join(leaked, ", "))
	}
	return out
}

// validKind keeps this package from depending on a method the domain type does
// not have. It lists the kinds a package may carry, which is a different
// question from "can this build execute it".
func validKind(k domain.RuleKind) bool {
	switch k {
	case domain.KindAutomation, domain.KindScene, domain.KindAlarm:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Secret scan
// ---------------------------------------------------------------------------
//
// The scan is deliberately narrow. A broad "does the JSON contain the word key"
// check would refuse every rule that writes a telemetry key, and a check that
// refuses everything is a check nobody keeps.

var secretName = regexp.MustCompile(`(?i)^(password|passwd|pwd|secret|token|api_?key|access_?key|credential|auth|sig|signature)$`)

// urlish matches the beginnings of things that could be a URL.
var urlish = regexp.MustCompile(`(?i)^(https?|mqtt|mqtts|ws|wss|tcp|udp)://`)

// ScanSecrets lists the places a definition appears to carry a credential.
//
// It looks at string values that are URLs and checks their query parameters and
// userinfo, because that is where a webhook token actually lives. It does not
// look at field names in general: `{"identifier":"key"}` is a telemetry key,
// not a secret.
func ScanSecrets(m *Manifest) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range m.Resources.Rules {
		body, err := jsonStrings(r.Definition)
		if err != nil {
			continue
		}
		for _, s := range body {
			if !urlish.MatchString(s) {
				continue
			}
			u, err := url.Parse(s)
			if err != nil {
				continue
			}
			bad := ""
			if u.User != nil {
				if _, has := u.User.Password(); has {
					bad = "userinfo"
				}
			}
			if bad == "" {
				for name := range u.Query() {
					if secretName.MatchString(name) {
						bad = name
						break
					}
				}
			}
			if bad == "" {
				continue
			}
			where := r.ResourceKey + " (" + bad + ")"
			if !seen[where] {
				seen[where] = true
				out = append(out, where)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Rewriting references
// ---------------------------------------------------------------------------

// RewriteRefs replaces rule references inside a definition.
//
// The same function runs in both directions: export passes a map from local row
// id to resource key, import passes resource key to local row id. One
// implementation means the two directions cannot drift.
func RewriteRefs(d domain.Definition, fn func(string) string) domain.Definition {
	var walk func(steps []domain.Step) []domain.Step
	walk = func(steps []domain.Step) []domain.Step {
		out := make([]domain.Step, len(steps))
		for i, s := range steps {
			if s.AlarmPolicyID != "" {
				s.AlarmPolicyID = fn(s.AlarmPolicyID)
			}
			if s.Kind == domain.StepActivateScene && s.Value != "" {
				s.Value = fn(s.Value)
			}
			s.Then = walk(s.Then)
			s.Else = walk(s.Else)
			out[i] = s
		}
		return out
	}
	d.Steps = walk(d.Steps)
	return d
}

// RewriteSlots replaces notification policy ids with slot references (export)
// or slot references with local policy ids (import).
//
// An empty replacement means "do not notify". A notify step whose target was
// declined is removed rather than kept as an empty step: a step that exists only
// to notify has nothing left to do, and leaving it behind would fail validation
// on the next publish with a message that points at the importer's choice
// instead of at the package.
func RewriteSlots(d domain.Definition, fn func(string) string) domain.Definition {
	var walk func(steps []domain.Step) []domain.Step
	walk = func(steps []domain.Step) []domain.Step {
		out := make([]domain.Step, 0, len(steps))
		for _, s := range steps {
			if s.NotificationPolicyID != "" {
				s.NotificationPolicyID = fn(s.NotificationPolicyID)
				if s.NotificationPolicyID == "" && s.Kind == domain.StepNotify {
					continue
				}
			}
			s.Then = walk(s.Then)
			s.Else = walk(s.Else)
			out = append(out, s)
		}
		return out
	}
	d.Steps = walk(d.Steps)
	if d.Alarm != nil && d.Alarm.NotifyPolicyID != "" {
		d.Alarm.NotifyPolicyID = fn(d.Alarm.NotifyPolicyID)
	}
	if d.Alarm != nil && d.Alarm.NotifyAliasID != "" {
		_, wasSlot := SlotOf(d.Alarm.NotifyAliasID)
		d.Alarm.NotifyAliasID = fn(d.Alarm.NotifyAliasID)
		if _, stillSlot := SlotOf(d.Alarm.NotifyAliasID); wasSlot && !stillSlot {
			// Import choices contain native IDs, not source-deployment aliases.
			d.Alarm.NotifyPolicyID = d.Alarm.NotifyAliasID
			d.Alarm.NotifyAliasID = ""
		}
	}
	return d
}

// ---------------------------------------------------------------------------
// Planning
// ---------------------------------------------------------------------------

// Action is what an import would do with one resource.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionSkip    Action = "skip"
	ActionBlocked Action = "blocked"
)

// Existing is what the target environment already has for a resource key.
type Existing struct {
	RuleID     string
	Name       string
	Hash       string
	Definition domain.Definition
}

// Target is the importer's view of the destination.
type Target struct {
	// Existing is keyed by resource key.
	Existing map[string]*Existing
	// Roles maps a role key to the local device ids that fill it.
	Roles map[string][]string
	// Slots maps a slot key to a local notification policy id. An empty string
	// is a deliberate "do not notify", which is different from a missing entry.
	Slots map[string]string
	// DeviceConfigs maps a template key to the local template id.
	DeviceConfigs map[string]string
	// CanDo reports whether a device supports a capability. A nil function
	// means "unknown", and the plan then warns instead of blocking: refusing on
	// unproven knowledge is how a good package gets rejected.
	CanDo func(deviceID, capability string) bool
}

// ResourcePlan is one line of the report.
type ResourcePlan struct {
	ResourceKey string         `json:"resourceKey"`
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`
	Action      Action         `json:"action"`
	RuleID      string         `json:"ruleId,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Issues      []domain.Issue `json:"issues,omitempty"`
}

// Plan is the whole preview.
type Plan struct {
	OK            bool           `json:"ok"`
	Resources     []ResourcePlan `json:"resources"`
	MissingRoles  []string       `json:"missingRoles,omitempty"`
	MissingSlots  []string       `json:"missingSlots,omitempty"`
	DeclinedSlots []string       `json:"declinedSlots,omitempty"`
	Issues        []domain.Issue `json:"issues,omitempty"`
	CreateCount   int            `json:"createCount"`
	UpdateCount   int            `json:"updateCount"`
	SkipCount     int            `json:"skipCount"`
}

// Preview computes what an import would do, without doing it.
func Preview(m *Manifest, t *Target) *Plan {
	p := &Plan{}
	if t == nil {
		t = &Target{}
	}
	for _, i := range Validate(m) {
		p.Issues = append(p.Issues, i)
	}

	// Roles: a required role with no device blocks; an optional one only warns.
	for _, r := range m.Dependencies.Roles {
		if len(t.Roles[r.Key]) > 0 {
			continue
		}
		if r.Kind == RoleTemplate && t.DeviceConfigs[r.TemplateKey] != "" {
			// A template role is filled by the template itself: every device in
			// it inherits the binding, so no explicit device is needed.
			continue
		}
		if r.Required {
			p.MissingRoles = append(p.MissingRoles, r.Key)
			p.Issues = append(p.Issues, domain.Issue{
				Field: "roles." + r.Key, Code: "role_unmapped",
				Message: "这个包需要一个设备来填 " + roleLabel(r) + "，还没有选择",
			})
		}
	}

	// Slots: required and unanswered blocks. Optional and unanswered is a
	// decision ("do not notify"), recorded rather than silently accepted.
	for _, s := range m.Resources.NotifySlots {
		if _, given := t.Slots[s.SlotKey]; given {
			if t.Slots[s.SlotKey] == "" {
				p.DeclinedSlots = append(p.DeclinedSlots, s.SlotKey)
			}
			continue
		}
		if s.Required {
			p.MissingSlots = append(p.MissingSlots, s.SlotKey)
			p.Issues = append(p.Issues, domain.Issue{
				Field: "notifySlots." + s.SlotKey, Code: "slot_unresolved",
				Message: "这个包会发通知，但还没选择用哪个通知策略（" + s.Label + "）",
			})
		} else {
			p.DeclinedSlots = append(p.DeclinedSlots, s.SlotKey)
		}
	}

	// Capabilities: only refused when the directory can prove the device
	// cannot do it.
	for _, c := range m.Dependencies.Capabilities {
		if t.CanDo == nil {
			continue
		}
		for _, dev := range t.Roles[c.RoleKey] {
			if !t.CanDo(dev, c.Capability) {
				p.Issues = append(p.Issues, domain.Issue{
					Field: "capabilities." + c.RoleKey, Code: "capability_missing",
					Message: fmt.Sprintf("设备 %s 不具备 %s 能力（%s）", dev, c.Capability, c.Why),
				})
			}
		}
	}

	for _, r := range m.Resources.Rules {
		row := ResourcePlan{ResourceKey: r.ResourceKey, Name: r.Name, Kind: string(r.Kind)}
		existing := t.Existing[r.ResourceKey]
		switch {
		case existing == nil:
			row.Action = ActionCreate
			p.CreateCount++
		case existing.Hash == r.Hash && existing.Hash != "":
			// Same content: an import that changes nothing must not rewrite the
			// rule, because a rewrite bumps the revision and detaches nothing
			// but confuses everyone reading the history.
			row.Action = ActionSkip
			row.RuleID = existing.RuleID
			row.Reason = "内容相同，不需要改动"
			p.SkipCount++
		default:
			row.Action = ActionUpdate
			row.RuleID = existing.RuleID
			row.Reason = "本地已存在同名资源，将更新到包内版本"
			p.UpdateCount++
		}
		p.Resources = append(p.Resources, row)
	}

	p.OK = len(p.Issues) == 0
	return p
}

func roleLabel(r Role) string {
	if r.DisplayName != "" {
		return r.DisplayName
	}
	if r.Kind == RoleTemplate {
		return "模板 " + r.TemplateKey
	}
	return r.Key
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// jsonStrings collects every string in a JSON document.
func jsonStrings(v any) ([]string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(decoded)
	return out, nil
}

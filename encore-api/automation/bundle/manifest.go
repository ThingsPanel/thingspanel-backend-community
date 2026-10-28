// Package bundle carries the automation half of a market bundle: the rules, the
// templates they came from, the notification slots they need filled, and the
// dependency manifest that makes an import checkable before it writes anything.
//
// Three decisions shape the whole package.
//
// **A resource key is a business key, never a row id.** `rule:door-left-open`
// survives a round trip through another environment; a uuid does not. Because
// the key is stable, "import this bundle twice" is a lookup instead of a
// duplicate, and idempotency is structural rather than a flag somebody has to
// remember to set.
//
// **A device is a role, not an id.** A bundle describes what kind of device it
// needs ("a device from the door-sensor template"), and the importer maps the
// role onto a real device. Exporting device ids would produce a package that
// only works in the environment it came from, and importing one would quietly
// point at nothing.
//
// **A notification target is a slot, not a policy id.** A notification policy
// id belongs to the exporter's tenant. Carrying it over would either fail or —
// worse — resolve to somebody else's group. The package declares
// "slot:on-call, required", and the importer either picks a local policy or
// explicitly declines.
//
// Nothing here touches the database. The manifest is a value, the plan is a
// value, and the service layer is what decides to write.
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"encore.app/automation/domain"
)

// ContractVersion is the manifest schema this build writes and understands.
//
// It is separate from the bundle's own `Version`: a customer may publish 1.4.2
// of their bundle, while the shape of the manifest is still contract 1.
const ContractVersion = "1"

// RoleKind distinguishes "some device" from "a device of this template".
type RoleKind string

const (
	RoleDevice   RoleKind = "device"
	RoleTemplate RoleKind = "template"
)

// SlotPrefix marks a notification slot reference inside a definition. A value
// starting with this prefix is not a policy id and must be resolved before the
// rule is allowed to run.
const SlotPrefix = "slot:"

// Manifest is one exportable automation package.
type Manifest struct {
	ContractVersion string       `json:"contractVersion"`
	BundleKey       string       `json:"bundleKey"`
	Version         string       `json:"version"`
	ContentHash     string       `json:"contentHash"`
	Resources       Resources    `json:"resources"`
	Dependencies    Dependencies `json:"dependencies"`
	Security        Security     `json:"security"`
}

// Resources is the payload. Every list is ordered so that the content hash is
// stable across exports of the same configuration.
type Resources struct {
	Blueprints  []BlueprintRef `json:"blueprints,omitempty"`
	Rules       []Rule         `json:"rules"`
	NotifySlots []NotifySlot   `json:"notifySlots,omitempty"`
}

// BlueprintRef pins the template version a rule was instantiated from, so an
// importer can tell "this package expects template v2" from "this package
// happens to look like v3".
type BlueprintRef struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
}

// Rule is one exported rule.
type Rule struct {
	ResourceKey string            `json:"resourceKey"`
	Name        string            `json:"name"`
	Kind        domain.RuleKind   `json:"kind"`
	Revision    int               `json:"revision"`
	Hash        string            `json:"hash"`
	Definition  domain.Definition `json:"definition"`
	Bindings    []Binding         `json:"bindings,omitempty"`
	Blueprint   *BlueprintOrigin  `json:"blueprint,omitempty"`
	// TargetRoles holds what Definition.Targets used to hold before export: the
	// devices and templates this rule is pointed at, expressed as roles. The
	// definition itself ships with its target lists empty, because a package
	// that names a device only works in the environment it came from.
	TargetRoles []string `json:"targetRoles,omitempty"`
}

// BlueprintOrigin records that a rule came from a template, and with which
// values. It is what makes "upgrade this package's template" possible without
// guessing which parameters the customer chose.
type BlueprintOrigin struct {
	Key     string            `json:"key"`
	Version int               `json:"version"`
	Params  map[string]string `json:"params,omitempty"`
}

// Binding is one scope attachment, expressed as a role.
type Binding struct {
	ScopeRole string            `json:"scopeRole"`
	Params    map[string]string `json:"params,omitempty"`
	Revision  int               `json:"revision"`
}

// NotifySlot is a logical notification target. It deliberately has no field for
// recipients: a package that could carry them would eventually carry them.
type NotifySlot struct {
	SlotKey  string `json:"slotKey"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// Dependencies is the manifest proper: what has to exist before the payload can
// mean anything.
type Dependencies struct {
	Roles           []Role          `json:"roles,omitempty"`
	DeviceTemplates []TemplateDep   `json:"deviceTemplates,omitempty"`
	Capabilities    []CapabilityDep `json:"capabilities,omitempty"`
	Edges           []Edge          `json:"edges,omitempty"`
}

// Role is one device slot the importer has to fill.
type Role struct {
	Key         string   `json:"key"`
	Kind        RoleKind `json:"kind"`
	TemplateKey string   `json:"templateKey,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	Required    bool     `json:"required"`
}

// TemplateDep is a device-template dependency, referenced by business key.
type TemplateDep struct {
	Key     string `json:"key"`
	Version string `json:"version,omitempty"`
	Hash    string `json:"hash,omitempty"`
}

// CapabilityDep says "the device in this role must be able to do X". Import
// checks it against the target environment's directory instead of discovering
// at runtime that the command does not exist.
type CapabilityDep struct {
	RoleKey    string `json:"roleKey"`
	Capability string `json:"capability"`
	Why        string `json:"why,omitempty"`
}

// Edge is one "this rule points at that rule" reference, carried explicitly so
// an importer can refuse a package whose internal references do not close.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Field string `json:"field"` // alarm_policy_id | scene
}

// Security is what the package promises not to contain. Export computes these
// from the payload; import refuses a package that claims false and isn't.
type Security struct {
	ContainsSecrets     bool `json:"containsSecrets"`
	ContainsRecipients  bool `json:"containsRecipients"`
	ContainsRuntimeData bool `json:"containsRuntimeData"`
}

// Clean reports whether the package is safe to hand to another tenant.
func (s Security) Clean() bool {
	return !s.ContainsSecrets && !s.ContainsRecipients && !s.ContainsRuntimeData
}

// ---------------------------------------------------------------------------
// hashing
// ---------------------------------------------------------------------------

// Hash is a stable sha256 over a JSON value.
//
// domain.DefinitionHash is a 32-bit FNV, which is fine as a change detector
// inside one database and useless as a cross-environment identity. A package
// hash has to survive being emailed, so it is sha256 over canonical JSON.
func Hash(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		// Marshalling a struct we control cannot fail; if it does, an empty
		// hash makes the comparison visibly wrong instead of silently equal.
		return ""
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ComputeHash fills ContentHash from the payload.
//
// Metadata is excluded on purpose: renaming a package or fixing a typo in its
// description is not a new content version, and treating it as one would make
// every import look like a change.
func (m *Manifest) ComputeHash() {
	m.ContentHash = Hash(struct {
		Resources    Resources    `json:"resources"`
		Dependencies Dependencies `json:"dependencies"`
	}{m.Resources, m.Dependencies})
}

// Sort orders every list deterministically so two exports of the same
// configuration produce the same bytes.
func (m *Manifest) Sort() {
	sort.Slice(m.Resources.Rules, func(i, j int) bool {
		return m.Resources.Rules[i].ResourceKey < m.Resources.Rules[j].ResourceKey
	})
	for i := range m.Resources.Rules {
		r := &m.Resources.Rules[i]
		sort.Slice(r.Bindings, func(a, b int) bool {
			return r.Bindings[a].ScopeRole < r.Bindings[b].ScopeRole
		})
	}
	sort.Slice(m.Resources.Blueprints, func(i, j int) bool {
		return m.Resources.Blueprints[i].Key < m.Resources.Blueprints[j].Key
	})
	sort.Slice(m.Resources.NotifySlots, func(i, j int) bool {
		return m.Resources.NotifySlots[i].SlotKey < m.Resources.NotifySlots[j].SlotKey
	})
	sort.Slice(m.Dependencies.Roles, func(i, j int) bool {
		return m.Dependencies.Roles[i].Key < m.Dependencies.Roles[j].Key
	})
	sort.Slice(m.Dependencies.DeviceTemplates, func(i, j int) bool {
		return m.Dependencies.DeviceTemplates[i].Key < m.Dependencies.DeviceTemplates[j].Key
	})
	sort.Slice(m.Dependencies.Capabilities, func(i, j int) bool {
		a, b := m.Dependencies.Capabilities[i], m.Dependencies.Capabilities[j]
		if a.RoleKey != b.RoleKey {
			return a.RoleKey < b.RoleKey
		}
		return a.Capability < b.Capability
	})
	sort.Slice(m.Dependencies.Edges, func(i, j int) bool {
		a, b := m.Dependencies.Edges[i], m.Dependencies.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Field < b.Field
	})
}

// ---------------------------------------------------------------------------
// references inside a definition
// ---------------------------------------------------------------------------

// SlotOf returns the slot key a value refers to, if it refers to one.
func SlotOf(v string) (string, bool) {
	if strings.HasPrefix(v, SlotPrefix) {
		return strings.TrimPrefix(v, SlotPrefix), true
	}
	return "", false
}

// SlotValue renders a slot reference as it is stored in a definition.
func SlotValue(key string) string { return SlotPrefix + key }

// DefinitionSlots lists the notification slots a definition needs filled.
//
// It walks steps (including nested branches) and the alarm spec, because a
// notification raised from an else branch is exactly the one that gets
// forgotten.
func DefinitionSlots(d domain.Definition) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		if key, ok := SlotOf(v); ok && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	var walk func(steps []domain.Step)
	walk = func(steps []domain.Step) {
		for _, s := range steps {
			add(s.NotificationPolicyID)
			walk(s.Then)
			walk(s.Else)
		}
	}
	walk(d.Steps)
	if d.Alarm != nil {
		add(d.Alarm.NotifyPolicyID)
		add(d.Alarm.NotifyAliasID)
	}
	sort.Strings(out)
	return out
}

// DefinitionRefs lists the rule ids a definition points at, with the field they
// came from. This is the same two places the delete pre-check looks at (A20),
// kept in one function so a package cannot carry a reference the delete path
// would not see.
func DefinitionRefs(d domain.Definition) []Edge {
	var out []Edge
	var walk func(steps []domain.Step)
	walk = func(steps []domain.Step) {
		for _, s := range steps {
			if s.AlarmPolicyID != "" {
				out = append(out, Edge{To: s.AlarmPolicyID, Field: "alarm_policy_id"})
			}
			if s.Kind == domain.StepActivateScene && s.Value != "" {
				out = append(out, Edge{To: s.Value, Field: "scene"})
			}
			walk(s.Then)
			walk(s.Else)
		}
	}
	walk(d.Steps)
	return out
}

// HasRuntimeData is the structural half of the security promise: the manifest
// type has no field for runs, episodes, pending windows or outbox rows, so a
// package can only fail this by being hand-edited.
func (m *Manifest) HasRuntimeData() bool {
	for _, r := range m.Resources.Rules {
		if r.Revision < 0 {
			return true
		}
	}
	return false
}

// ErrContract is returned for a manifest written by a newer build.
func ErrContract(got string) error {
	return fmt.Errorf("不支持的包格式版本 %q（本版本支持 %s）", got, ContractVersion)
}

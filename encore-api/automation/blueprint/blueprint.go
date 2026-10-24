// Package blueprint holds the four built-in rule templates and the versioned
// parameter contract around them.
//
// A blueprint is a *declaration*, not a second implementation. It produces a
// plain domain.Definition, which then goes through the same domain.Validate gate
// as anything a customer draws by hand. That is the only way "instantiated from
// a template" and "written manually" can be guaranteed to mean the same thing at
// run time: there is one evaluator, one validator and one compiler, and the
// template is just a well-chosen starting point.
//
// Two rules shaped this package:
//
//   - An instance is identified by (key, version, revision) plus its parameter
//     values. The definition body is not copied per device — it is produced once
//     per instance and stored on the rule revision, so a template with a hundred
//     devices does not become a hundred copies of the same AST.
//   - Upgrading is always explicit. Publishing a new template version never
//     reaches into an existing instance: the customer sees a diff and decides.
//     When we cannot carry a choice forward (a parameter they overrode is gone,
//     or their value is no longer allowed), the upgrade is blocked with the
//     specific reason instead of being "close enough".
package blueprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"encore.app/automation/domain"
)

// ParamType is the closed set of parameter shapes a template may declare. Every
// entry point (Web, CLI, MCP) renders and validates from this list, so the same
// value is accepted or rejected in exactly the same way everywhere.
type ParamType string

const (
	// ParamDevice is a device id. It is the only parameter type that may name a
	// real object; no template ships with one filled in.
	ParamDevice ParamType = "device"
	// ParamNumber is a numeric threshold with an optional unit.
	ParamNumber ParamType = "number"
	// ParamDuration is a Go duration string ("30s", "5m").
	ParamDuration ParamType = "duration"
	// ParamEnum is a choice from a fixed option list.
	ParamEnum ParamType = "enum"
	// ParamText is free text (a telemetry identifier, a literal value).
	ParamText ParamType = "text"
	// ParamPolicy is a notification group id. Never defaulted to a real group.
	ParamPolicy ParamType = "policy"
)

// Severity separates "this cannot run" from "we could not check this".
//
// The distinction matters: a capability that the community device list does not
// expose is not evidence that the device lacks it, so it must not block. Saying
// so out loud is better than pretending the check passed.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Capability names one signal a template needs from the device.
type Capability struct {
	// Kind is telemetry, attribute, status or command.
	Kind string `json:"kind"`
	// Key is the identifier. Empty on a parameter means "use this parameter's
	// value", which is how a template says "whatever metric the customer picked
	// must exist on the device".
	Key string `json:"key,omitempty"`
}

func (c Capability) String() string {
	if c.Key == "" {
		return c.Kind
	}
	return c.Kind + ":" + c.Key
}

// Param is one declared template parameter.
type Param struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        ParamType `json:"type"`
	Unit        string    `json:"unit,omitempty"`
	Required    bool      `json:"required"`
	Default     string    `json:"default,omitempty"`
	HasMin      bool      `json:"has_min,omitempty"`
	Min         float64   `json:"min,omitempty"`
	HasMax      bool      `json:"has_max,omitempty"`
	Max         float64   `json:"max,omitempty"`
	Options     []string  `json:"options,omitempty"`
	Description string    `json:"description,omitempty"`
	// Capability, when set, declares that this parameter points at a device
	// signal, so the chosen device can be checked at instantiation time instead
	// of failing at run time with "no data".
	Capability *Capability `json:"capability,omitempty"`
}

// Issue is one template-level problem, in the same spirit as domain.Issue: a
// field, a code and a sentence a customer can act on.
type Issue struct {
	Code     string   `json:"code"`
	Param    string   `json:"param,omitempty"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// Values are the parameter values of one instance, as strings.
//
// Strings keep the transport honest: a number that arrives as 35.0 from JSON and
// as "35" from a form must validate the same way, and a duration must not be
// silently reinterpreted as seconds.
type Values map[string]string

// Blueprint is one version of one template.
type Blueprint struct {
	Key      string `json:"key"`
	Version  int    `json:"version"`
	Revision string `json:"revision"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	// Guidance is shown above the form: what the customer is about to create,
	// in one sentence, without internal vocabulary.
	Guidance string       `json:"guidance"`
	Params   []Param      `json:"params"`
	Requires []Capability `json:"requires,omitempty"`

	build func(Values) (*domain.Definition, []Issue)
}

// RevisionFor computes the fixed revision string of a template version.
//
// It is derived from the declared surface (key, version, params, requirements)
// rather than typed by hand, so it cannot drift from what the form actually
// shows. Two template versions that look identical in the form but differ in the
// produced definition would be a real problem; register() checks that they do
// not share a revision.
func RevisionFor(b *Blueprint) string {
	body, _ := json.Marshal(struct {
		Key      string
		Version  int
		Params   []Param
		Requires []Capability
	}{b.Key, b.Version, b.Params, b.Requires})
	return fmt.Sprintf("%s@%d-%08x", b.Key, b.Version, fnv32a(body))
}

func fnv32a(b []byte) uint32 {
	var h uint32 = 2166136261
	for _, c := range b {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

var registry = map[string][]*Blueprint{}

// register adds one version. It panics on a duplicate key/version or a duplicate
// revision, because both are programming errors that would make "which template
// is this instance from" ambiguous — and this file is the only place that can
// create them.
func register(b *Blueprint) {
	b.Revision = RevisionFor(b)
	for _, existing := range registry[b.Key] {
		if existing.Version == b.Version {
			panic(fmt.Sprintf("blueprint %s: duplicate version %d", b.Key, b.Version))
		}
		if existing.Revision == b.Revision {
			panic(fmt.Sprintf("blueprint %s: versions %d and %d share revision %s",
				b.Key, existing.Version, b.Version, b.Revision))
		}
	}
	registry[b.Key] = append(registry[b.Key], b)
	sort.Slice(registry[b.Key], func(i, j int) bool {
		return registry[b.Key][i].Version < registry[b.Key][j].Version
	})
}

// All returns the newest version of every template, ordered by key, so a list
// endpoint is stable across calls.
func All() []*Blueprint {
	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*Blueprint, 0, len(keys))
	for _, k := range keys {
		out = append(out, Latest(k))
	}
	return out
}

// Latest returns the newest version of one template, or nil.
func Latest(key string) *Blueprint {
	versions := registry[key]
	if len(versions) == 0 {
		return nil
	}
	return versions[len(versions)-1]
}

// Get returns one exact version. version 0 means "latest", which is what a
// create request does; an upgrade always names the version it moves to.
func Get(key string, version int) *Blueprint {
	if version == 0 {
		return Latest(key)
	}
	for _, b := range registry[key] {
		if b.Version == version {
			return b
		}
	}
	return nil
}

// Versions lists the published versions of a template in ascending order.
func Versions(key string) []int {
	out := make([]int, 0, len(registry[key]))
	for _, b := range registry[key] {
		out = append(out, b.Version)
	}
	return out
}

// ---------------------------------------------------------------------------
// Instantiation
// ---------------------------------------------------------------------------

// Instance is one rule created from a template.
//
// It carries the template identity and the values, not a copy of the template:
// the produced definition lives on the rule revision. That is what keeps a
// template with many instances cheap.
type Instance struct {
	BlueprintKey      string             `json:"blueprint_key"`
	BlueprintVersion  int                `json:"blueprint_version"`
	BlueprintRevision string             `json:"blueprint_revision"`
	Values            Values             `json:"values"`
	Overrides         Values             `json:"overrides,omitempty"`
	Capabilities      []Capability       `json:"capabilities,omitempty"`
	Definition        *domain.Definition `json:"definition,omitempty"`
}

// Instantiate validates the values and produces the definition.
//
// It returns the issues even when it succeeds (warnings only), so a caller can
// show "we could not verify the device exposes this metric" next to a rule that
// was created anyway.
func Instantiate(key string, version int, in Values) (*Instance, []Issue) {
	b := Get(key, version)
	if b == nil {
		return nil, []Issue{{
			Code: "unknown_blueprint", Severity: SeverityError,
			Message: fmt.Sprintf("没有这个模板：%s（可用版本 %v）", key, Versions(key)),
		}}
	}
	values, issues := resolve(b, in)
	if HasError(issues) {
		return nil, issues
	}
	def, buildIssues := b.build(values)
	issues = append(issues, buildIssues...)
	if HasError(issues) {
		return nil, issues
	}
	def.Normalize()
	if err := domain.Validate(def); err != nil {
		// A template that produces an invalid definition is our bug, not the
		// customer's. Reporting it as a field error keeps the failure in the same
		// shape as everything else instead of turning into a 500.
		issues = append(issues, fromValidation(err)...)
		return nil, issues
	}
	return &Instance{
		BlueprintKey:      b.Key,
		BlueprintVersion:  b.Version,
		BlueprintRevision: b.Revision,
		Values:            values,
		Overrides:         overridesOf(b, values),
		Capabilities:      capabilitiesOf(b, values),
		Definition:        def,
	}, issues
}

// resolve fills defaults, rejects unknown parameters and validates each value
// against its declared type.
func resolve(b *Blueprint, in Values) (Values, []Issue) {
	var issues []Issue
	out := Values{}
	for _, p := range b.Params {
		out[p.Key] = p.Default
	}
	declared := make(map[string]bool, len(b.Params))
	for _, p := range b.Params {
		declared[p.Key] = true
	}
	// Unknown parameters are rejected rather than ignored: a typo in a CLI flag
	// would otherwise create a rule that quietly ignores the value the customer
	// thought they set.
	unknown := make([]string, 0)
	for k := range in {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		issues = append(issues, Issue{
			Code: "unknown_param", Param: k, Severity: SeverityError,
			Message: fmt.Sprintf("模板 %s@%d 没有参数 %q", b.Key, b.Version, k),
		})
	}
	for _, p := range b.Params {
		if v, ok := in[p.Key]; ok {
			out[p.Key] = strings.TrimSpace(v)
		}
	}
	for _, p := range b.Params {
		v := out[p.Key]
		if v == "" {
			if p.Required {
				issues = append(issues, Issue{
					Code: "missing_param", Param: p.Key, Severity: SeverityError,
					Message: fmt.Sprintf("%s（%s）必须填写", p.Label, p.Key),
				})
			}
			continue
		}
		issues = append(issues, validateValue(p, v)...)
	}
	return out, issues
}

func validateValue(p Param, v string) []Issue {
	switch p.Type {
	case ParamNumber:
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return []Issue{{Code: "invalid_number", Param: p.Key, Severity: SeverityError,
				Message: fmt.Sprintf("%s（%s）必须是数字，收到 %q", p.Label, p.Key, v)}}
		}
		if p.HasMin && n < p.Min {
			return []Issue{{Code: "out_of_range", Param: p.Key, Severity: SeverityError,
				Message: fmt.Sprintf("%s（%s）不能小于 %g%s，收到 %g", p.Label, p.Key, p.Min, p.Unit, n)}}
		}
		if p.HasMax && n > p.Max {
			return []Issue{{Code: "out_of_range", Param: p.Key, Severity: SeverityError,
				Message: fmt.Sprintf("%s（%s）不能大于 %g%s，收到 %g", p.Label, p.Key, p.Max, p.Unit, n)}}
		}
	case ParamDuration:
		d, err := time.ParseDuration(v)
		if err != nil {
			return []Issue{{Code: "invalid_duration", Param: p.Key, Severity: SeverityError,
				Message: fmt.Sprintf("%s（%s）必须是时长（如 30s、5m），收到 %q", p.Label, p.Key, v)}}
		}
		if d <= 0 {
			return []Issue{{Code: "invalid_duration", Param: p.Key, Severity: SeverityError,
				Message: fmt.Sprintf("%s（%s）必须大于 0", p.Label, p.Key)}}
		}
		if d > 24*time.Hour {
			return []Issue{{Code: "duration_too_long", Param: p.Key, Severity: SeverityError,
				Message: fmt.Sprintf("%s（%s）不能超过 24 小时", p.Label, p.Key)}}
		}
	case ParamEnum:
		for _, o := range p.Options {
			if o == v {
				return nil
			}
		}
		return []Issue{{Code: "invalid_option", Param: p.Key, Severity: SeverityError,
			Message: fmt.Sprintf("%s（%s）只能是 %s 之一，收到 %q",
				p.Label, p.Key, strings.Join(p.Options, "/"), v)}}
	case ParamDevice, ParamPolicy, ParamText:
		// Shape checks for these belong to their owners: the device directory
		// decides whether a device id exists, the notification core whether a
		// group exists. Duplicating those checks here would create a second
		// opinion that can disagree.
	}
	return nil
}

// overridesOf reports the values the customer chose, i.e. everything that
// differs from the template default. Only these are "their choice" and only
// these can block an upgrade.
func overridesOf(b *Blueprint, values Values) Values {
	out := Values{}
	for _, p := range b.Params {
		if v := values[p.Key]; v != "" && v != p.Default {
			out[p.Key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// capabilitiesOf expands the declared capabilities, substituting a parameter
// value where the declaration left the key open.
func capabilitiesOf(b *Blueprint, values Values) []Capability {
	out := make([]Capability, 0, len(b.Requires)+2)
	out = append(out, b.Requires...)
	for _, p := range b.Params {
		if p.Capability == nil {
			continue
		}
		c := *p.Capability
		if c.Key == "" {
			c.Key = values[p.Key]
		}
		if c.Key == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// HasError reports whether any issue blocks the operation.
func HasError(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

func fromValidation(err error) []Issue {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		out := make([]Issue, 0, len(ve.Issues))
		for _, i := range ve.Issues {
			out = append(out, Issue{
				Code: "template_invalid", Param: i.Field, Severity: SeverityError,
				Message: i.Code + ": " + i.Message,
			})
		}
		return out
	}
	return []Issue{{Code: "template_invalid", Severity: SeverityError, Message: err.Error()}}
}

// ---------------------------------------------------------------------------
// Version diff and upgrade
// ---------------------------------------------------------------------------

// ParamChange is one parameter-level difference between two versions.
type ParamChange struct {
	Param string `json:"param"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Diff is the visible difference between two template versions. It exists so an
// upgrade is a decision, not a surprise: the customer sees exactly which
// parameters appear, disappear or change default before anything is republished.
type Diff struct {
	From            int           `json:"from"`
	To              int           `json:"to"`
	AddedParams     []string      `json:"added_params,omitempty"`
	RemovedParams   []string      `json:"removed_params,omitempty"`
	DefaultsChanged []ParamChange `json:"defaults_changed,omitempty"`
	// RangesChanged shows a narrowed or widened allowed range. It is separate
	// from DefaultsChanged because it is the one change that can invalidate a
	// value the customer already chose — and an upgrade that fails must be able
	// to point at the range, not at "the template changed".
	RangesChanged     []ParamChange `json:"ranges_changed,omitempty"`
	RequiresAdded     []string      `json:"requires_added,omitempty"`
	RequiresRemoved   []string      `json:"requires_removed,omitempty"`
	DefinitionChanged bool          `json:"definition_changed"`
}

// DiffVersions compares two versions of the same template.
func DiffVersions(key string, from, to int) (*Diff, []Issue) {
	a, b := Get(key, from), Get(key, to)
	if a == nil || b == nil {
		return nil, []Issue{{Code: "unknown_blueprint", Severity: SeverityError,
			Message: fmt.Sprintf("模板 %s 没有版本 %d 或 %d", key, from, to)}}
	}
	d := &Diff{From: a.Version, To: b.Version}
	ap := paramIndex(a)
	bp := paramIndex(b)
	for k, pb := range bp {
		pa, ok := ap[k]
		if !ok {
			d.AddedParams = append(d.AddedParams, k)
			continue
		}
		if pa.Default != pb.Default {
			d.DefaultsChanged = append(d.DefaultsChanged, ParamChange{Param: k, From: pa.Default, To: pb.Default})
		}
		if ar, br := rangeText(pa), rangeText(pb); ar != br {
			d.RangesChanged = append(d.RangesChanged, ParamChange{Param: k, From: ar, To: br})
		}
	}
	for k := range ap {
		if _, ok := bp[k]; !ok {
			d.RemovedParams = append(d.RemovedParams, k)
		}
	}
	sort.Strings(d.AddedParams)
	sort.Strings(d.RemovedParams)
	sort.Slice(d.DefaultsChanged, func(i, j int) bool { return d.DefaultsChanged[i].Param < d.DefaultsChanged[j].Param })
	sort.Slice(d.RangesChanged, func(i, j int) bool { return d.RangesChanged[i].Param < d.RangesChanged[j].Param })

	ar := capabilitySet(a.Requires)
	br := capabilitySet(b.Requires)
	for c := range br {
		if !ar[c] {
			d.RequiresAdded = append(d.RequiresAdded, c)
		}
	}
	for c := range ar {
		if !br[c] {
			d.RequiresRemoved = append(d.RequiresRemoved, c)
		}
	}
	sort.Strings(d.RequiresAdded)
	sort.Strings(d.RequiresRemoved)

	// The definition is the thing that actually runs, so compare it directly
	// instead of inferring "nothing important changed" from an unchanged form.
	da, ia := a.build(defaultsOf(a))
	db, ib := b.build(defaultsOf(b))
	if ia == nil && ib == nil && da != nil && db != nil {
		da.Normalize()
		db.Normalize()
		d.DefinitionChanged = domain.DefinitionHash(*da) != domain.DefinitionHash(*db)
	}
	return d, nil
}

func paramIndex(b *Blueprint) map[string]Param {
	out := make(map[string]Param, len(b.Params))
	for _, p := range b.Params {
		out[p.Key] = p
	}
	return out
}

func capabilitySet(cs []Capability) map[string]bool {
	out := make(map[string]bool, len(cs))
	for _, c := range cs {
		out[c.String()] = true
	}
	return out
}

func defaultsOf(b *Blueprint) Values {
	out := Values{}
	for _, p := range b.Params {
		out[p.Key] = p.Default
	}
	return out
}

// rangeText renders a parameter's allowed range for the diff. An open end is
// spelled out rather than left blank, so "0..200 → 0..120" cannot be mistaken
// for "no constraint".
func rangeText(p Param) string {
	if !p.HasMin && !p.HasMax {
		return ""
	}
	lo, hi := "不限", "不限"
	if p.HasMin {
		lo = strconv.FormatFloat(p.Min, 'g', -1, 64)
	}
	if p.HasMax {
		hi = strconv.FormatFloat(p.Max, 'g', -1, 64)
	}
	return lo + ".." + hi
}

// Upgrade is the result of planning a version move. Instance is nil when the
// move is blocked; Issues explains why, in the customer's terms.
type Upgrade struct {
	From     TemplateRef `json:"from"`
	To       TemplateRef `json:"to"`
	Diff     *Diff       `json:"diff"`
	Instance *Instance   `json:"instance,omitempty"`
	Issues   []Issue     `json:"issues,omitempty"`
	Blocked  bool        `json:"blocked"`
}

// TemplateRef is the identity of one template version, without its body.
type TemplateRef struct {
	Key      string `json:"key"`
	Version  int    `json:"version"`
	Revision string `json:"revision"`
}

// PlanUpgrade computes what moving an existing instance to another version would
// do, and refuses when it cannot be done without losing a customer's choice.
//
// The rules, in order:
//
//  1. Only forward moves. Re-pinning to an older version would resurrect
//     behaviour the customer already left behind.
//  2. A parameter the customer overrode must still exist. If it is gone we
//     cannot guess what they meant, so the upgrade stops here.
//  3. The resulting values must still pass validation — a new version that
//     narrows a range blocks the instances whose override is now out of range.
//  4. Values the customer did not touch follow the new defaults. That is the
//     whole point of a template version, and the diff shows it.
func PlanUpgrade(cur Instance, targetVersion int, in Values) *Upgrade {
	from := TemplateRef{Key: cur.BlueprintKey, Version: cur.BlueprintVersion, Revision: cur.BlueprintRevision}
	up := &Upgrade{From: from}
	target := Get(cur.BlueprintKey, targetVersion)
	if target == nil {
		up.Blocked = true
		up.Issues = []Issue{{Code: "unknown_blueprint", Severity: SeverityError,
			Message: fmt.Sprintf("模板 %s 没有版本 %d（可用版本 %v）",
				cur.BlueprintKey, targetVersion, Versions(cur.BlueprintKey))}}
		return up
	}
	up.To = TemplateRef{Key: target.Key, Version: target.Version, Revision: target.Revision}
	if target.Version <= cur.BlueprintVersion {
		up.Blocked = true
		up.Issues = []Issue{{Code: "not_an_upgrade", Severity: SeverityError,
			Message: fmt.Sprintf("实例已经是 %s@%d，不能升级到 %d",
				cur.BlueprintKey, cur.BlueprintVersion, target.Version)}}
		return up
	}

	d, diffIssues := DiffVersions(cur.BlueprintKey, cur.BlueprintVersion, target.Version)
	up.Diff = d
	up.Issues = append(up.Issues, diffIssues...)

	declared := paramIndex(target)
	for k, v := range cur.Overrides {
		if _, ok := declared[k]; !ok {
			up.Issues = append(up.Issues, Issue{
				Code: "override_param_removed", Param: k, Severity: SeverityError,
				Message: fmt.Sprintf("你为「%s」设置过自己的值 %q，新版本已移除该参数，无法判断它应换成什么", k, v),
			})
		}
	}

	// The move keeps the customer's own choices and takes the new defaults
	// everywhere else. Copying the old values wholesale would pin every value the
	// previous template happened to default to — which is the opposite of an
	// upgrade, and would make a "default changed" note in the diff a lie.
	values := Values{}
	for k, v := range cur.Overrides {
		values[k] = v
	}
	for k, v := range in {
		values[k] = v
	}
	inst, instIssues := Instantiate(target.Key, target.Version, values)
	up.Issues = append(up.Issues, instIssues...)
	if inst == nil || HasError(up.Issues) {
		up.Blocked = true
		return up
	}
	up.Instance = inst
	return up
}

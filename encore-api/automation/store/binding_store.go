package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Binding is "this rule applies to this device, or to every device of this
// template, with these parameter values".
//
// It is deliberately small: the rule body stays one row in auto_rules and one
// row per revision. A template with 500 devices is 500 small binding rows, not
// 500 copies of a rule — which is what makes "change the rule once" actually
// mean once, and what keeps the evaluator from having to reconcile 500 slightly
// different bodies.
type Binding struct {
	ID             string
	BindingKey     string
	TenantID       string
	RuleID         string
	Revision       int
	Source         string
	DeviceID       string
	DeviceConfigID string
	TemplateID     string
	Params         map[string]string
	Enabled        bool
	DetachedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Binding sources. The source is what the device page shows: "inherited from
// the template" and "set on this device" are different promises to the customer.
const (
	BindingSourceTemplate = "template"
	BindingSourceDevice   = "device"
	// BindingSourceLegacy marks a binding created by the migration from the old
	// console, so the reconciliation can tell migrated rows from new ones.
	BindingSourceLegacy = "legacy"
)

// BindingKey is the identity of a binding: one rule, one scope.
//
// It is derived rather than random so that "bind rule R to template T" twice is
// the same binding, not two rows that both fire. That is the difference between
// a migration that is safe to re-run and one that silently doubles every rule.
func BindingKey(tenantID, ruleID, deviceID, deviceConfigID string) string {
	switch {
	case deviceConfigID != "":
		return tenantID + "|" + ruleID + "|cfg:" + deviceConfigID
	case deviceID != "":
		return tenantID + "|" + ruleID + "|dev:" + deviceID
	default:
		return tenantID + "|" + ruleID + "|none"
	}
}

const bindingColumns = `id, binding_key, tenant_id, rule_id, revision, source,
       coalesce(device_id,''), coalesce(device_config_id,''), coalesce(template_id,''),
       params, enabled, detached_at, created_at, updated_at`

func scanBinding(sc interface{ Scan(...any) error }) (*Binding, error) {
	var (
		b      Binding
		params []byte
	)
	if err := sc.Scan(&b.ID, &b.BindingKey, &b.TenantID, &b.RuleID, &b.Revision, &b.Source,
		&b.DeviceID, &b.DeviceConfigID, &b.TemplateID, &params, &b.Enabled,
		&b.DetachedAt, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &b.Params)
	}
	return &b, nil
}

// UpsertBinding creates or updates one binding, keyed by BindingKey.
//
// Re-attaching a previously detached binding clears detached_at and re-enables
// it: "detach, then attach again" is a normal correction, not a new object.
func (s *Store) UpsertBinding(ctx context.Context, b *Binding) error {
	if b.BindingKey == "" {
		b.BindingKey = BindingKey(b.TenantID, b.RuleID, b.DeviceID, b.DeviceConfigID)
	}
	if b.Params == nil {
		b.Params = map[string]string{}
	}
	params, err := json.Marshal(b.Params)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
        INSERT INTO auto_bindings (id, binding_key, tenant_id, rule_id, revision, source,
                                   device_id, device_config_id, template_id, params, enabled)
        VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11)
        ON CONFLICT (binding_key) DO UPDATE
           SET revision=EXCLUDED.revision, params=EXCLUDED.params, enabled=EXCLUDED.enabled,
               source=EXCLUDED.source, detached_at=NULL, updated_at=now()`,
		b.ID, b.BindingKey, b.TenantID, b.RuleID, b.Revision, b.Source,
		b.DeviceID, b.DeviceConfigID, b.TemplateID, params, b.Enabled)
	return err
}

// GetBinding loads one binding inside a tenant.
func (s *Store) GetBinding(ctx context.Context, tenantID, id string) (*Binding, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+bindingColumns+` FROM auto_bindings WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	b, err := scanBinding(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// ListBindingsForRule returns every binding of a rule, attached or detached.
//
// Detached rows are returned on purpose: "this template used to be bound and was
// detached on 2026-10-24" is the answer to "why did it stop running", and
// deleting the row would throw that answer away.
func (s *Store) ListBindingsForRule(ctx context.Context, tenantID, ruleID string) ([]*Binding, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+bindingColumns+` FROM auto_bindings
         WHERE tenant_id=$1 AND rule_id=$2
         ORDER BY enabled DESC, created_at`, tenantID, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Binding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListBindingsForScope returns the bindings of one device or one template.
func (s *Store) ListBindingsForScope(ctx context.Context, tenantID, deviceID, deviceConfigID string) ([]*Binding, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+bindingColumns+` FROM auto_bindings
         WHERE tenant_id=$1
           AND ((device_id IS NOT NULL AND device_id=$2)
                OR (device_config_id IS NOT NULL AND device_config_id=$3))
         ORDER BY enabled DESC, created_at`, tenantID, deviceID, deviceConfigID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Binding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BindingsForDevice returns the enabled bindings that apply to a concrete
// device, template-level first and device-level second.
//
// The order is the merge order: the caller applies them in sequence, so a
// device-level row overrides the template row for the same rule without needing
// a second "override" table. Two rows for one rule is exactly the intended
// shape of an override.
func (s *Store) BindingsForDevice(ctx context.Context, tenantID, deviceID, deviceConfigID string) ([]*Binding, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+bindingColumns+` FROM auto_bindings
         WHERE tenant_id=$1 AND enabled
           AND ((device_id IS NOT NULL AND device_id=$2)
                OR (device_config_id IS NOT NULL AND $3 <> '' AND device_config_id=$3))
         ORDER BY (device_id IS NOT NULL), created_at`,
		tenantID, deviceID, deviceConfigID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Binding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DetachBinding stops a binding without erasing it.
//
// Future triggers stop; runs already accepted keep their side effects, because
// cancelling configuration is not the same as recalling a command a device may
// already have executed.
func (s *Store) DetachBinding(ctx context.Context, tenantID, id string) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_bindings SET enabled=false, detached_at=now(), updated_at=now()
         WHERE id=$1 AND tenant_id=$2 AND enabled`, id, tenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Either it does not exist or it was already detached. Both are "no
		// change", but only the first is an error the caller should hear about.
		var exists bool
		if err := s.DB.QueryRowContext(ctx,
			`SELECT true FROM auto_bindings WHERE id=$1 AND tenant_id=$2`, id, tenantID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return ErrAlreadyDone
	}
	return nil
}

// CountBindings reports how many bindings a scope has, split by state.
func (s *Store) CountBindings(ctx context.Context, tenantID, deviceConfigID string) (attached, detached int, err error) {
	err = s.DB.QueryRowContext(ctx, `
        SELECT count(*) FILTER (WHERE enabled), count(*) FILTER (WHERE NOT enabled)
          FROM auto_bindings WHERE tenant_id=$1 AND device_config_id=$2`,
		tenantID, deviceConfigID).Scan(&attached, &detached)
	return
}

// HasTemplateBindings reports whether a tenant uses template-level bindings at
// all. The ingest path uses it to skip the device→template lookup entirely for
// the (common) tenant that only has device-level bindings.
func (s *Store) HasTemplateBindings(ctx context.Context, tenantID string) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM auto_bindings WHERE tenant_id=$1 AND device_config_id IS NOT NULL AND enabled)`,
		tenantID).Scan(&ok)
	return ok, err
}

// MergeBindingParams folds one binding into a per-rule parameter map.
//
// Later bindings win, which is why BindingsForDevice orders template rows first.
func MergeBindingParams(into map[string]map[string]string, b *Binding) {
	if into == nil || !b.Enabled {
		return
	}
	cur := into[b.RuleID]
	if cur == nil {
		cur = map[string]string{}
		into[b.RuleID] = cur
	}
	for k, v := range b.Params {
		cur[k] = v
	}
}

// SortedParamKeys is used in audit output so two runs of the same check produce
// the same text.
func SortedParamKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EffectiveScope describes, for one device, which rules apply and why.
type EffectiveScope struct {
	RuleID   string
	Source   string
	BindingID string
	Params   map[string]string
}

// EffectiveBindings resolves the full picture for one device: template rows,
// device rows, and which one won each rule.
func (s *Store) EffectiveBindings(ctx context.Context, tenantID, deviceID, deviceConfigID string) ([]EffectiveScope, error) {
	rows, err := s.BindingsForDevice(ctx, tenantID, deviceID, deviceConfigID)
	if err != nil {
		return nil, err
	}
	byRule := map[string]*EffectiveScope{}
	order := []string{}
	for _, b := range rows {
		e, ok := byRule[b.RuleID]
		if !ok {
			e = &EffectiveScope{RuleID: b.RuleID, Params: map[string]string{}}
			byRule[b.RuleID] = e
			order = append(order, b.RuleID)
		}
		if b.DeviceID != "" {
			e.Source = BindingSourceDevice
		} else {
			e.Source = BindingSourceTemplate
		}
		e.BindingID = b.ID
		for k, v := range b.Params {
			e.Params[k] = v
		}
	}
	out := make([]EffectiveScope, 0, len(order))
	for _, id := range order {
		out = append(out, *byRule[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return out, nil
}

// BindingSummary is one line of the reconciliation report.
type BindingSummary struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Source   string `json:"source"`
	Enabled  bool   `json:"enabled"`
	Params   int    `json:"params"`
}

// SummarizeBindings joins bindings with rule names for the reconciliation view.
func (s *Store) SummarizeBindings(ctx context.Context, tenantID, deviceConfigID string) ([]BindingSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT b.rule_id, coalesce(r.name,''), b.source, b.enabled, coalesce(jsonb_object_length(b.params),0)
          FROM auto_bindings b
          LEFT JOIN auto_rules r ON r.id=b.rule_id
         WHERE b.tenant_id=$1 AND b.device_config_id=$2
         ORDER BY b.enabled DESC, r.name`, tenantID, deviceConfigID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BindingSummary
	for rows.Next() {
		var x BindingSummary
		if err := rows.Scan(&x.RuleID, &x.RuleName, &x.Source, &x.Enabled, &x.Params); err != nil {
			return nil, fmt.Errorf("scan binding summary: %w", err)
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

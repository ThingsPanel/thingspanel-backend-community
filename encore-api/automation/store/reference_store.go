package store

import (
	"context"
	"fmt"
	"time"
)

// A20: what happens to a rule's references when the thing they point at goes
// away.
//
// Deleting configuration is not the same as recalling a physical action. These
// helpers only ever change what will happen *next*; runs that were already
// accepted keep their records, and a command a device may already have executed
// stays executed. The point is that nothing keeps firing from a reference that
// no longer resolves, and that "why did this stop" has an answer.

// ReferencingRules returns the rules whose steps point at targetID.
//
// The match is a recursive JSON path because references are nested: a scene may
// be activated inside a branch, and an alarm policy may be raised from an else
// branch. A shallow scan would report "not referenced" for exactly the cases
// that are hardest to notice.
//
// Only two places can hold a rule id. `alarm_policy_id` is an alarm raised by a
// step; `value` holds the scene id when the step kind is `activate_scene` (the
// legacy "激活场景" action maps onto it). `identifier` is deliberately not
// matched: it is the telemetry key an action writes, so a customer who happens
// to use a uuid-shaped key would otherwise make their own rule undeletable.
func (s *Store) ReferencingRules(ctx context.Context, tenantID, targetID string) ([]*Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT DISTINCT `+ruleColumns+`
          FROM auto_rules r
         WHERE r.tenant_id = $1
           AND r.deleted_at IS NULL
           AND r.id <> $2
           AND jsonb_path_exists(
                 r.definition,
                 '$.** ? (@.alarm_policy_id == $id
                         || (@.kind == "activate_scene" && @.value == $id))',
                 jsonb_build_object('id', $2::text))
         ORDER BY r.name`, tenantID, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DetachBindingsForRule stops every binding of a rule.
//
// It is used when the rule itself is deleted. The rows stay so the device page
// can still say "this used to be driven by a rule that was deleted", instead of
// the device silently becoming unmanaged.
func (s *Store) DetachBindingsForRule(ctx context.Context, tenantID, ruleID, reason string) (int, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_bindings SET enabled=false, detached_at=now(), updated_at=now()
         WHERE tenant_id=$1 AND rule_id=$2 AND enabled`, tenantID, ruleID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RebindBindings moves a rule's enabled bindings to a new revision.
//
// Bindings pin the revision they were authored against, so a rule edit does not
// silently change what a device runs. Moving them is an explicit act: the
// caller has already checked that the stored values still fit the new body.
func (s *Store) RebindBindings(ctx context.Context, tenantID, ruleID string, revision int) (int, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_bindings SET revision=$3, updated_at=now()
         WHERE tenant_id=$1 AND rule_id=$2 AND enabled`, tenantID, ruleID, revision)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// StaleBindings returns the bindings whose pinned revision is behind the rule.
func (s *Store) StaleBindings(ctx context.Context, tenantID, ruleID string) ([]*Binding, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+bindingColumns+`
          FROM auto_bindings b
          JOIN auto_rules r ON r.id = b.rule_id
         WHERE b.tenant_id=$1 AND b.rule_id=$2 AND b.enabled AND b.revision < r.revision`,
		tenantID, ruleID)
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

// ClearPendingForRule removes the pending windows of one rule.
//
// A deleted rule must not leave a deadline that fires later. The window's own
// timer row would be cancelled anyway, but the pending row is the thing the
// sweep reads, so it has to go with the rule.
func (s *Store) ClearPendingForRule(ctx context.Context, tenantID, ruleID string) (int, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM auto_pending WHERE tenant_id=$1 AND rule_id=$2`, tenantID, ruleID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ClearPendingForDevice removes the pending windows of one device.
func (s *Store) ClearPendingForDevice(ctx context.Context, tenantID, deviceID string) (int, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM auto_pending WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ClearPendingOne removes one device's window for one rule.
//
// The reconciliation uses this instead of ClearPendingForDevice: when a single
// rule loses its binding, the device's other deadlines are still perfectly
// resolvable and dropping them would turn a cleanup into data loss.
func (s *Store) ClearPendingOne(ctx context.Context, tenantID, deviceID, ruleID string) (int, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM auto_pending WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3`,
		tenantID, deviceID, ruleID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PendingDevices lists the devices that currently hold a pending window.
func (s *Store) PendingDevices(ctx context.Context, tenantID string, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT DISTINCT device_id FROM auto_pending
         WHERE tenant_id=$1
         ORDER BY device_id
         LIMIT $2`, tenantID, limit)
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

// OrphanBinding is a binding whose scope no longer exists.
type OrphanBinding struct {
	Binding *Binding
	Reason  string
	Detail  string
}

// ReconcileBindings checks the device-scoped bindings of a tenant against the
// device directory and reports the ones that can never match again.
//
// Only device-level bindings can be orphaned: a template binding does not name
// a device, so "the template currently has no devices" is a normal state (that
// is what inheritance means) and not an orphan. Reporting it would push an
// operator to delete a binding that is about to start working.
func (s *Store) ReconcileBindings(ctx context.Context, tenantID string, knownDevices map[string]bool) ([]OrphanBinding, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+bindingColumns+` FROM auto_bindings
         WHERE tenant_id=$1 AND enabled AND device_id IS NOT NULL
         ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrphanBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		if knownDevices[b.DeviceID] {
			continue
		}
		out = append(out, OrphanBinding{Binding: b, Reason: "device_missing",
			Detail: "设备已不存在，这条绑定不会再匹配任何上报"})
	}
	return out, rows.Err()
}

// PendingGhost is a pending window whose device is gone.
type PendingGhost struct {
	DeviceID string
	RuleIDs  []string
	Deadline time.Time
}

// GhostPending finds pending windows for devices that are not in the directory.
//
// This is the "幽灵 timer" case: the device was deleted, the deadline stayed,
// and the sweep would fire a rule for a device that no longer exists. It is
// reported rather than silently deleted so the operator sees how many were
// cleared.
func (s *Store) GhostPending(ctx context.Context, tenantID string, known map[string]bool) ([]PendingGhost, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT device_id, rule_id, min(deadline)
          FROM auto_pending
         WHERE tenant_id=$1
         GROUP BY device_id, rule_id
         ORDER BY device_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDevice := map[string]*PendingGhost{}
	order := []string{}
	for rows.Next() {
		var dev, ruleID string
		var deadline time.Time
		if err := rows.Scan(&dev, &ruleID, &deadline); err != nil {
			return nil, err
		}
		if known[dev] {
			continue
		}
		g, ok := byDevice[dev]
		if !ok {
			g = &PendingGhost{DeviceID: dev, Deadline: deadline}
			byDevice[dev] = g
			order = append(order, dev)
		}
		g.RuleIDs = append(g.RuleIDs, ruleID)
		if deadline.Before(g.Deadline) {
			g.Deadline = deadline
		}
	}
	out := make([]PendingGhost, 0, len(order))
	for _, dev := range order {
		out = append(out, *byDevice[dev])
	}
	return out, rows.Err()
}

// DeleteRuleTombstone records why a rule was deleted.
//
// The row is soft-deleted by DeleteRule, so the identity survives; the reason is
// what turns "it is gone" into "it was deleted on purpose, because ...". Runs,
// transitions and detached bindings keep pointing at the same id and stay
// explainable.
func (s *Store) DeleteRuleTombstone(ctx context.Context, tenantID, ruleID, reason string) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_rules
           SET deleted_at = now(),
               enabled = false,
               version = version + 1,
               delete_reason = NULLIF($3,'')
         WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, ruleID, tenantID, reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// BindingReference is one "this rule is used by that rule" edge, for the delete
// pre-check message.
type BindingReference struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Field    string `json:"field"`
}

func (r BindingReference) String() string { return fmt.Sprintf("%s(%s)", r.RuleName, r.Field) }

// DeviceScopedBindingIDs lists the devices that have an enabled device-level
// binding. The reconciliation only has to check these, not the whole tenant.
func (s *Store) DeviceScopedBindingIDs(ctx context.Context, tenantID string, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT DISTINCT device_id FROM auto_bindings
         WHERE tenant_id=$1 AND enabled AND device_id IS NOT NULL
         ORDER BY device_id
         LIMIT $2`, tenantID, limit)
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

// PendingRef is one pending window, reduced to what the reconciliation needs.
type PendingRef struct {
	DeviceID string
	RuleID   string
	Deadline time.Time
}

// PendingRefs lists the pending windows of a tenant, bounded.
func (s *Store) PendingRefs(ctx context.Context, tenantID string, limit int) ([]PendingRef, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT device_id, rule_id, deadline FROM auto_pending
         WHERE tenant_id=$1
         ORDER BY deadline
         LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingRef
	for rows.Next() {
		var p PendingRef
		if err := rows.Scan(&p.DeviceID, &p.RuleID, &p.Deadline); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Package store is the persistence layer for the automation and alarm owners.
// It owns every SQL statement for these tables; no other package writes them.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"encore.app/automation/domain"
)

// ErrNotFound is returned for a missing row.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an optimistic version check fails.
var ErrConflict = errors.New("version conflict")

// ErrAlreadyDone is returned when the requested change is already true.
//
// It is separate from ErrNotFound because the two lead to different messages:
// "there is no such binding" is a mistake, "it is already detached" is the
// outcome the caller asked for.
var ErrAlreadyDone = errors.New("already done")

// Store wraps the database handle.
type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store { return &Store{DB: db} }

// Rule is a stored rule header.
type Rule struct {
	ID             string
	TenantID       string
	Name           string
	Kind           domain.RuleKind
	Enabled        bool
	Revision       int
	Definition     domain.Definition
	DefinitionHash string
	Version        int
	LegacyID       string
	// ResourceKey is the bundle identity of this rule (A21). It is a business
	// key, not a row id, so importing the same package into another tenant — or
	// into the same tenant twice — finds the rule it already created instead of
	// making a second one.
	ResourceKey string
	// BundleHash is the hash of the package-form definition that produced this
	// rule (A21). It is a different value from DefinitionHash on purpose: the
	// stored definition has local ids and resolved notification targets, while
	// the package has resource keys and slots. Comparing the two would make
	// every import look like a change.
	BundleHash string
	CreatedAt  time.Time
	UpdatedAt   time.Time

	// The three fields below are not columns. They are filled in when the rule
	// is materialized for one device (A19), so that the run and the pending
	// window can freeze "which binding, which values" next to the record they
	// belong to. A rule loaded straight from the database has them empty, which
	// is exactly right: it describes the shared body, not one device's copy.
	BindingID     string
	BindingSource string
	BindingParams map[string]string
}

// NextSeq returns a monotonic sequence number used for ordering and dedupe.
func (s *Store) NextSeq(ctx context.Context) (int64, error) {
	var seq int64
	if err := s.DB.QueryRowContext(ctx, `SELECT nextval('auto_seq')`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("next seq: %w", err)
	}
	return seq, nil
}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

// CreateRule inserts a new rule at revision 1 and publishes the revision row.
func (s *Store) CreateRule(ctx context.Context, r *Rule) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error { return s.createRuleTx(ctx, tx, r) })
}

// createRuleTx writes the rule and its first revision inside a caller's
// transaction, so a rule created from a template and the record of which
// template it came from cannot be committed one without the other.
func (s *Store) createRuleTx(ctx context.Context, tx *sql.Tx, r *Rule) error {
	def, err := json.Marshal(r.Definition)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO auto_rules (id, tenant_id, name, kind, enabled, revision, definition,
                                definition_hash, version, legacy_id, resource_key, bundle_hash,
                                created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,1,$6,$7,1,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),now(),now())`,
		r.ID, r.TenantID, r.Name, string(r.Kind), r.Enabled, def, r.DefinitionHash,
		r.LegacyID, r.ResourceKey, r.BundleHash); err != nil {
		return fmt.Errorf("insert rule: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO auto_rule_revisions (rule_id, revision, definition, definition_hash)
        VALUES ($1,1,$2,$3)`, r.ID, def, r.DefinitionHash); err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}
	return nil
}

// UpdateRule writes a new revision when expectedVersion matches.
func (s *Store) UpdateRule(ctx context.Context, r *Rule, expectedVersion int) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error { return s.updateRuleTx(ctx, tx, r, expectedVersion) })
}

// updateRuleTx is UpdateRule inside a caller's transaction.
func (s *Store) updateRuleTx(ctx context.Context, tx *sql.Tx, r *Rule, expectedVersion int) error {
	def, err := json.Marshal(r.Definition)
	if err != nil {
		return err
	}
	var nextRev int
	if err := tx.QueryRowContext(ctx,
		`SELECT revision + 1 FROM auto_rules WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		r.ID, r.TenantID).Scan(&nextRev); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	res, err := tx.ExecContext(ctx, `
        UPDATE auto_rules
           SET name=$3, definition=$4, definition_hash=$5, revision=$6,
               version=version+1, updated_at=now(),
               bundle_hash=COALESCE(NULLIF($8,''), bundle_hash)
         WHERE id=$1 AND tenant_id=$2 AND version=$7 AND deleted_at IS NULL`,
		r.ID, r.TenantID, r.Name, def, r.DefinitionHash, nextRev, expectedVersion, r.BundleHash)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO auto_rule_revisions (rule_id, revision, definition, definition_hash)
        VALUES ($1,$2,$3,$4)`, r.ID, nextRev, def, r.DefinitionHash); err != nil {
		return err
	}
	r.Revision = nextRev
	r.Version = expectedVersion + 1
	return nil
}

// SetEnabled flips the enabled flag with an optimistic check.
func (s *Store) SetEnabled(ctx context.Context, tenantID, ruleID string, enabled bool, expectedVersion int) (int, error) {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_rules SET enabled=$3, version=version+1, updated_at=now()
         WHERE id=$1 AND tenant_id=$2 AND version=$4 AND deleted_at IS NULL`,
		ruleID, tenantID, enabled, expectedVersion)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrConflict
	}
	return expectedVersion + 1, nil
}

// DeleteRule soft deletes and disables the rule.
func (s *Store) DeleteRule(ctx context.Context, tenantID, ruleID string) error {
	res, err := s.DB.ExecContext(ctx, `
        UPDATE auto_rules SET deleted_at=now(), enabled=false, version=version+1
         WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, ruleID, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const ruleColumns = `id, tenant_id, name, kind, enabled, revision, definition, definition_hash,
                     version, coalesce(legacy_id,''), coalesce(resource_key,''),
                     coalesce(bundle_hash,''), created_at, updated_at`

func scanRule(sc interface{ Scan(...any) error }) (*Rule, error) {
	var (
		r    Rule
		kind string
		raw  []byte
	)
	if err := sc.Scan(&r.ID, &r.TenantID, &r.Name, &kind, &r.Enabled, &r.Revision,
		&raw, &r.DefinitionHash, &r.Version, &r.LegacyID, &r.ResourceKey, &r.BundleHash,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Kind = domain.RuleKind(kind)
	if err := json.Unmarshal(raw, &r.Definition); err != nil {
		return nil, fmt.Errorf("decode definition: %w", err)
	}
	return &r, nil
}

// GetRule loads one rule scoped to a tenant.
func (s *Store) GetRule(ctx context.Context, tenantID, ruleID string) (*Rule, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+ruleColumns+` FROM auto_rules WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		ruleID, tenantID)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// GetRuleRevision loads an exact published revision (used by runs so an
// in-flight run never sees a newer body).
func (s *Store) GetRuleRevision(ctx context.Context, ruleID string, revision int) (*Rule, error) {
	row := s.DB.QueryRowContext(ctx, `
        SELECT r.id, r.tenant_id, r.name, r.kind, r.enabled, rev.revision, rev.definition,
               rev.definition_hash, r.version, coalesce(r.legacy_id,''),
               coalesce(r.resource_key,''), coalesce(r.bundle_hash,''), r.created_at, r.updated_at
          FROM auto_rules r
          JOIN auto_rule_revisions rev ON rev.rule_id = r.id AND rev.revision = $2
         WHERE r.id = $1`, ruleID, revision)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// ListRules returns the tenant's rules, optionally filtered by kind.
// RuleFilter narrows a rule listing.
//
// The old UI never showed "all rules of the tenant": every entry point was a
// context — a device, a device template, an alarm tab, a name search. A listing
// that can only filter by kind turns those four tabs into a full-tenant dump,
// which is why these filters live in the store and not in the caller.
type RuleFilter struct {
	Kind string
	// DeviceID matches rules that act on the device or whose conditions watch it.
	DeviceID string
	// DeviceConfigID matches rules scoped to a template (template tabs).
	DeviceConfigID string
	// Name is a case-insensitive substring match.
	Name string
	// Enabled is nil for "any".
	Enabled *bool
	// HasAlarmStep matches rules whose program contains an alarm step. The old
	// "告警规则" tab was exactly this: automations with an alarm action, not a
	// separate object type.
	HasAlarmStep bool
	Limit        int
	Offset       int
}

func (s *Store) ListRules(ctx context.Context, tenantID string, f RuleFilter) ([]*Rule, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + ruleColumns + ` FROM auto_rules
	       WHERE tenant_id=$1 AND deleted_at IS NULL`
	args := []any{tenantID}
	add := func(cond string, val any) {
		args = append(args, val)
		q += fmt.Sprintf(` AND `+cond, len(args))
	}
	if f.Kind != "" {
		add(`kind=$%d`, f.Kind)
	}
	if f.Name != "" {
		add(`name ILIKE '%%' || $%d || '%%'`, f.Name)
	}
	if f.Enabled != nil {
		add(`enabled=$%d`, *f.Enabled)
	}
	if f.DeviceID != "" {
		// A rule belongs to a device when it targets it or watches it. Both
		// halves are needed: a device automation usually watches the device and
		// acts on it, but a scene can act without watching.
		add(`(
            definition->'targets'->'device_ids' @> to_jsonb($%[1]d::text)
            OR EXISTS (
                SELECT 1
                  FROM jsonb_array_elements(coalesce(definition->'trigger'->'groups','[]'::jsonb)) g,
                       jsonb_array_elements(coalesce(g->'conditions','[]'::jsonb)) c
                 WHERE c->>'device_id' = $%[1]d
            )
        )`, f.DeviceID)
	}
	if f.DeviceConfigID != "" {
		add(`definition->'targets'->'device_config_ids' @> to_jsonb($%[1]d::text)`,
			f.DeviceConfigID)
	}
	if f.HasAlarmStep {
		// Recursive descent so an alarm step nested inside a branch still counts.
		q += ` AND jsonb_path_exists(definition, '$.**.kind ? (@ == "alarm")')`
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d OFFSET %d`, limit, f.Offset)

	rows, err := s.DB.QueryContext(ctx, q, args...)
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

// CandidateRules returns the enabled rules that could react to a device/signal.
// The candidate index is derived from the definition's condition device ids and
// from the bindings, so evaluation never scans every tenant rule.
//
// deviceConfigID may be empty when the tenant has no template bindings at all;
// the caller skips the device→template lookup in that case rather than paying
// for it on every uplink.
func (s *Store) CandidateRules(ctx context.Context, tenantID, deviceID, deviceConfigID, signal, key string) ([]*Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT DISTINCT `+ruleColumns+`
          FROM auto_rules r
         WHERE r.tenant_id = $1
           AND r.deleted_at IS NULL
           AND r.enabled
           AND (
             EXISTS (
               SELECT 1 FROM jsonb_array_elements(r.definition->'trigger'->'groups') g,
                             jsonb_array_elements(g->'conditions') c
                WHERE (c->>'source' = 'self' OR c->>'source' IS NULL)
                  AND c->>'signal' = $4
                  AND c->>'key' = $5
             )
             OR EXISTS (
               SELECT 1 FROM auto_bindings b
                WHERE b.rule_id = r.id AND b.enabled
                  AND ((b.device_id IS NOT NULL AND b.device_id = $2)
                       OR (b.device_config_id IS NOT NULL AND $3 <> '' AND b.device_config_id = $3))
             )
           )`, tenantID, deviceID, deviceConfigID, signal, key)
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

// ---------------------------------------------------------------------------
// Bindings live in binding_store.go
// ---------------------------------------------------------------------------

// GetRuleByLegacyID resolves a rule by the id it had in the old console.
//
// The migration uses it to keep the link between an old object and the new one
// explicit. Without it the only way to find the policy for a legacy alarm id is to
// match on name, which is exactly the kind of guess that silently attaches an
// action to the wrong alarm.
func (s *Store) GetRuleByLegacyID(ctx context.Context, tenantID, legacyID string) (*Rule, error) {
	if legacyID == "" {
		return nil, ErrNotFound
	}
	var id string
	err := s.DB.QueryRowContext(ctx, `
        SELECT id FROM auto_rules
         WHERE tenant_id=$1 AND legacy_id=$2 AND deleted_at IS NULL
         ORDER BY created_at LIMIT 1`, tenantID, legacyID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetRule(ctx, tenantID, id)
}

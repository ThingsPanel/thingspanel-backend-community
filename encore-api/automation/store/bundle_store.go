package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// A21: the persistent side of a package import.
//
// The row is deliberately chatty: it stores the manifest, the role mapping the
// operator chose, the notification choices they made, and the rules already
// created. That is what lets a half-finished import be resumed instead of
// guessed at, and what stops "failed" from being reported as "done".

// Bundle import status values.
const (
	BundleStaged     = "staged"
	BundleActivating = "activating"
	BundleActivated  = "activated"
	BundleFailed     = "failed"
)

// BundleImport is one attempted import of one package content hash.
type BundleImport struct {
	ID            string
	TenantID      string
	BundleKey     string
	Version       string
	ContentHash   string
	Status        string
	Manifest      []byte
	RoleMapping   map[string][]string
	NotifyChoices map[string]string
	CreatedRules  map[string]string
	Report        []byte
	ReasonCode    string
	ReasonMsg     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ActivatedAt   *time.Time
}

// Done reports whether this import already finished. A finished import is what
// makes a second identical request a no-op instead of a duplicate.
func (b *BundleImport) Done() bool { return b.Status == BundleActivated }

// Resumable reports whether a retry can pick up where this one stopped.
func (b *BundleImport) Resumable() bool {
	return b.Status == BundleFailed || b.Status == BundleStaged || b.Status == BundleActivating
}

const bundleImportColumns = `id, tenant_id, bundle_key, version, content_hash, status,
                             manifest, role_mapping, notify_choices, created_rules,
                             coalesce(report,'null'::jsonb), coalesce(reason_code,''),
                             coalesce(reason_message,''), created_at, updated_at, activated_at`

func scanBundleImport(sc interface{ Scan(...any) error }) (*BundleImport, error) {
	var (
		b                     BundleImport
		manifest, report      []byte
		roles, choices, rules []byte
		activated             sql.NullTime
	)
	if err := sc.Scan(&b.ID, &b.TenantID, &b.BundleKey, &b.Version, &b.ContentHash, &b.Status,
		&manifest, &roles, &choices, &rules, &report, &b.ReasonCode, &b.ReasonMsg,
		&b.CreatedAt, &b.UpdatedAt, &activated); err != nil {
		return nil, err
	}
	b.Manifest = manifest
	b.Report = report
	if len(roles) > 0 {
		_ = json.Unmarshal(roles, &b.RoleMapping)
	}
	if len(choices) > 0 {
		_ = json.Unmarshal(choices, &b.NotifyChoices)
	}
	if len(rules) > 0 {
		_ = json.Unmarshal(rules, &b.CreatedRules)
	}
	if activated.Valid {
		t := activated.Time
		b.ActivatedAt = &t
	}
	return &b, nil
}

// StageBundleImport records the intent to import, or returns the row that is
// already there.
//
// `created` tells the caller which happened. That distinction is the whole
// point: "we started something new" and "this exact package was already
// imported" lead to different answers, and only one of them should create
// resources.
func (s *Store) StageBundleImport(ctx context.Context, b *BundleImport) (stored *BundleImport, created bool, err error) {
	roles, err := jsonOrNull(b.RoleMapping)
	if err != nil {
		return nil, false, err
	}
	choices, err := jsonOrNull(b.NotifyChoices)
	if err != nil {
		return nil, false, err
	}
	rules, err := jsonOrNull(b.CreatedRules)
	if err != nil {
		return nil, false, err
	}
	if rules == nil {
		rules = []byte(`{}`)
	}
	var report any
	if len(b.Report) > 0 {
		report = b.Report
	}

	row := s.DB.QueryRowContext(ctx, `
        INSERT INTO auto_bundle_imports
            (id, tenant_id, bundle_key, version, content_hash, status, manifest,
             role_mapping, notify_choices, created_rules, report)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
        ON CONFLICT (tenant_id, bundle_key, version, content_hash) DO NOTHING
        RETURNING `+bundleImportColumns,
		b.ID, b.TenantID, b.BundleKey, b.Version, b.ContentHash, b.Status, b.Manifest,
		roles, choices, rules, report)
	stored, err = scanBundleImport(row)
	if err == nil {
		return stored, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	// Conflict: somebody already staged (or finished) this exact package.
	existing, gerr := s.GetBundleImportByIdentity(ctx, b.TenantID, b.BundleKey, b.Version, b.ContentHash)
	if gerr != nil {
		return nil, false, gerr
	}
	return existing, false, nil
}

// GetBundleImportByIdentity finds the row for one package content hash.
func (s *Store) GetBundleImportByIdentity(ctx context.Context, tenantID, bundleKey, version, hash string) (*BundleImport, error) {
	row := s.DB.QueryRowContext(ctx, `
        SELECT `+bundleImportColumns+` FROM auto_bundle_imports
         WHERE tenant_id=$1 AND bundle_key=$2 AND version=$3 AND content_hash=$4`,
		tenantID, bundleKey, version, hash)
	b, err := scanBundleImport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// GetBundleImport loads one import by id.
func (s *Store) GetBundleImport(ctx context.Context, tenantID, id string) (*BundleImport, error) {
	row := s.DB.QueryRowContext(ctx, `
        SELECT `+bundleImportColumns+` FROM auto_bundle_imports
         WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	b, err := scanBundleImport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// ListBundleImports lists recent imports, newest first.
func (s *Store) ListBundleImports(ctx context.Context, tenantID string, limit int) ([]*BundleImport, error) {
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+bundleImportColumns+` FROM auto_bundle_imports
         WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BundleImport
	for rows.Next() {
		b, err := scanBundleImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MarkBundleImport moves the import to a new status.
//
// createdRules is written whole rather than merged: the caller holds the
// authoritative map, and a merge would let a stale retry resurrect a mapping to
// a rule that was rolled back.
func (s *Store) MarkBundleImport(ctx context.Context, tenantID, id, status string,
	createdRules map[string]string, reasonCode, reasonMsg string, report []byte) error {
	rules, err := jsonOrNull(createdRules)
	if err != nil {
		return err
	}
	if rules == nil {
		rules = []byte(`{}`)
	}
	var rep any
	if len(report) > 0 {
		rep = report
	}
	_, err = s.DB.ExecContext(ctx, `
        UPDATE auto_bundle_imports
           SET status=$3,
               created_rules=$4,
               reason_code=NULLIF($5,''),
               reason_message=NULLIF($6,''),
               report=COALESCE($7, report),
               updated_at=now(),
               activated_at=CASE WHEN $3='activated' THEN now() ELSE activated_at END
         WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, status, rules, reasonCode, reasonMsg, rep)
	return err
}

// RuleByResourceKey finds the rule a package created earlier.
//
// Soft-deleted rules are excluded: if an operator deleted an imported rule, the
// next import creates a fresh one instead of silently resurrecting the deleted
// row, because "I deleted this" is an instruction, not a gap.
func (s *Store) RuleByResourceKey(ctx context.Context, tenantID, key string) (*Rule, error) {
	row := s.DB.QueryRowContext(ctx, `
        SELECT `+ruleColumns+` FROM auto_rules
         WHERE tenant_id=$1 AND resource_key=$2 AND deleted_at IS NULL`, tenantID, key)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// RulesByResourceKeys resolves a batch of keys in one query.
//
// Import planning looks up every rule in the package; doing that one row at a
// time is the difference between one round trip and two hundred.
func (s *Store) RulesByResourceKeys(ctx context.Context, tenantID string, keys []string) (map[string]*Rule, error) {
	out := map[string]*Rule{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
        SELECT `+ruleColumns+` FROM auto_rules
         WHERE tenant_id=$1 AND deleted_at IS NULL AND resource_key = ANY($2)`, tenantID, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out[r.ResourceKey] = r
	}
	return out, rows.Err()
}

// jsonOrNull marshals an optional map, returning nil for an empty one so the
// column stays NULL rather than storing "{}" that means nothing.
func jsonOrNull(m any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	switch v := m.(type) {
	case map[string]string:
		if len(v) == 0 {
			return nil, nil
		}
	case map[string][]string:
		if len(v) == 0 {
			return nil, nil
		}
	}
	return json.Marshal(m)
}

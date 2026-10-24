package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// BlueprintInstance records where a rule came from.
//
// It stores the template identity and the parameter values, not a copy of the
// template body: the definition lives on the rule revision, which is what keeps
// a template with many instances cheap and makes "the template changed" a
// question about a reference instead of a diff across hundreds of rows.
//
// It is a separate table rather than columns on auto_rules because most rules
// have no template at all, and a nullable half-filled block on the hot table
// invites reading "no template" as "template removed".
type BlueprintInstance struct {
	RuleID            string
	TenantID          string
	BlueprintKey      string
	BlueprintVersion  int
	BlueprintRevision string
	Params            map[string]string
	Overrides         map[string]string
	Capabilities      []string
	CreatedAt         time.Time
	UpgradedAt        *time.Time
}

const blueprintInstanceColumns = `rule_id, tenant_id, blueprint_key, blueprint_version,
	blueprint_revision, params, overrides, capabilities, created_at, upgraded_at`

// SaveBlueprintInstance inserts or replaces the origin of one rule.
func (s *Store) SaveBlueprintInstance(ctx context.Context, tx *sql.Tx, in *BlueprintInstance) error {
	params, err := json.Marshal(in.Params)
	if err != nil {
		return fmt.Errorf("encode blueprint params: %w", err)
	}
	overrides, err := json.Marshal(in.Overrides)
	if err != nil {
		return fmt.Errorf("encode blueprint overrides: %w", err)
	}
	caps, err := json.Marshal(in.Capabilities)
	if err != nil {
		return fmt.Errorf("encode blueprint capabilities: %w", err)
	}
	_, err = s.exec(ctx, tx, `
		INSERT INTO auto_blueprint_instances
			(rule_id, tenant_id, blueprint_key, blueprint_version, blueprint_revision,
			 params, overrides, capabilities)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb)
		ON CONFLICT (rule_id) DO UPDATE SET
			blueprint_key = EXCLUDED.blueprint_key,
			blueprint_version = EXCLUDED.blueprint_version,
			blueprint_revision = EXCLUDED.blueprint_revision,
			params = EXCLUDED.params,
			overrides = EXCLUDED.overrides,
			capabilities = EXCLUDED.capabilities,
			upgraded_at = now()`,
		in.RuleID, in.TenantID, in.BlueprintKey, in.BlueprintVersion, in.BlueprintRevision,
		string(params), string(overrides), string(caps))
	if err != nil {
		return fmt.Errorf("save blueprint instance: %w", err)
	}
	return nil
}

// GetBlueprintInstance returns the origin of a rule, or (nil, nil) when the rule
// was authored by hand. "No template" is not an error: most rules are.
func (s *Store) GetBlueprintInstance(ctx context.Context, tenantID, ruleID string) (*BlueprintInstance, error) {
	row := s.row(ctx, nil, `SELECT `+blueprintInstanceColumns+`
		FROM auto_blueprint_instances WHERE tenant_id = $1 AND rule_id = $2`, tenantID, ruleID)
	in, err := scanBlueprintInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get blueprint instance: %w", err)
	}
	return in, nil
}

// ListBlueprintInstancesByKey lists every rule a tenant created from one
// template. It answers "who is affected by this version" without touching the
// rule bodies.
func (s *Store) ListBlueprintInstancesByKey(ctx context.Context, tenantID, key string) ([]*BlueprintInstance, error) {
	rows, err := s.query(ctx, nil, `SELECT `+blueprintInstanceColumns+`
		FROM auto_blueprint_instances WHERE tenant_id = $1 AND blueprint_key = $2
		ORDER BY created_at, rule_id`, tenantID, key)
	if err != nil {
		return nil, fmt.Errorf("list blueprint instances: %w", err)
	}
	defer rows.Close()
	var out []*BlueprintInstance
	for rows.Next() {
		in, err := scanBlueprintInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("scan blueprint instance: %w", err)
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanBlueprintInstance(row scanner) (*BlueprintInstance, error) {
	var (
		in         BlueprintInstance
		params     []byte
		overrides  []byte
		caps       []byte
		upgradedAt sql.NullTime
	)
	if err := row.Scan(&in.RuleID, &in.TenantID, &in.BlueprintKey, &in.BlueprintVersion,
		&in.BlueprintRevision, &params, &overrides, &caps, &in.CreatedAt, &upgradedAt); err != nil {
		return nil, err
	}
	in.Params = map[string]string{}
	in.Overrides = map[string]string{}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &in.Params); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
	}
	if len(overrides) > 0 {
		if err := json.Unmarshal(overrides, &in.Overrides); err != nil {
			return nil, fmt.Errorf("decode overrides: %w", err)
		}
	}
	if len(caps) > 0 {
		if err := json.Unmarshal(caps, &in.Capabilities); err != nil {
			return nil, fmt.Errorf("decode capabilities: %w", err)
		}
	}
	if upgradedAt.Valid {
		t := upgradedAt.Time
		in.UpgradedAt = &t
	}
	return &in, nil
}

// CreateRuleWithOrigin creates a rule and records where it came from in one
// transaction. A rule without its origin would be a template instance nobody can
// upgrade; an origin without its rule would point at nothing.
func (s *Store) CreateRuleWithOrigin(ctx context.Context, r *Rule, origin *BlueprintInstance) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.createRuleTx(ctx, tx, r); err != nil {
			return err
		}
		return s.SaveBlueprintInstance(ctx, tx, origin)
	})
}

// UpgradeRuleWithOrigin publishes a new revision and re-pins the origin in one
// transaction, so "the rule runs v2" and "the instance says v2" cannot disagree.
func (s *Store) UpgradeRuleWithOrigin(ctx context.Context, r *Rule, expectedVersion int, origin *BlueprintInstance) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.updateRuleTx(ctx, tx, r, expectedVersion); err != nil {
			return err
		}
		return s.SaveBlueprintInstance(ctx, tx, origin)
	})
}

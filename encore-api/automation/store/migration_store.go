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

// Migration item statuses.
//
// "unsupported" and "ambiguous" are terminal and are deliberately not "failed":
// retrying them changes nothing until a human fixes the legacy row. Keeping them
// in their own status is what stops a migration report from counting them as
// migrated or as transient errors.
const (
	MigrationPlanned     = "planned"
	MigrationApplied     = "applied"
	MigrationSkipped     = "skipped"
	MigrationUnsupported = "unsupported"
	MigrationAmbiguous   = "ambiguous"
	MigrationFailed      = "failed"
	MigrationKept        = "kept"
	MigrationRolledBack  = "rolled_back"
)

// Migration run statuses.
const (
	MigrationStatusPlanned    = "planned"
	MigrationStatusApplying   = "applying"
	MigrationStatusApplied    = "applied"
	MigrationStatusRolledBack = "rolled_back"
	MigrationStatusFailed     = "failed"
)

// Migration item kinds. They match the legacy table the row came from, so a
// report can say "this came from scene_info" without a second lookup.
const (
	MigrationKindScene      = "scene"
	MigrationKindAutomation = "automation"
	// MigrationKindAlarmConfig is an alarm_config row. It becomes an event-only
	// alarm policy, which is why it is a separate kind from MigrationKindAlarm:
	// one creates a policy, the other adopts a live alarm onto it.
	MigrationKindAlarmConfig = "alarm_config"
	MigrationKindAlarm       = "alarm"
)

// Migration is one run.
type Migration struct {
	ID           string
	TenantID     string
	Name         string
	Status       string
	SnapshotHash string
	Total        int
	Applied      int
	Skipped      int
	Failed       int
	Unresolved   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AlarmAdoption is the payload of an alarm item.
//
// It records the policy the alarm belonged to *as resolved at plan time*. Apply
// does not re-read the legacy database and does not re-resolve the policy: if the
// policy has since been deleted, the item fails loudly instead of opening an
// episode that points at nothing.
type AlarmAdoption struct {
	PolicyLegacyID string `json:"policy_legacy_id"`
	PolicyID       string `json:"policy_id"`
	PolicyKey      string `json:"policy_key"`
	Subject        string `json:"subject"`
	Dimension      string `json:"dimension,omitempty"`
	// Level is what the episode is displayed as (the policy's declared level).
	Level string `json:"level,omitempty"`
	// LegacyLevel is what the old alarm_info row said. It is kept because a
	// mismatch is a fact the operator should see, not something to overwrite.
	LegacyLevel string `json:"legacy_level,omitempty"`
	// OpenedAt is when the old alarm first fired. Adopting keeps that time: an
	// alarm that has been open for three days must not look like it just fired.
	OpenedAt     string `json:"opened_at"`
	LegacyStatus string `json:"legacy_status"`
}

// MigrationItem is one legacy object inside a run.
type MigrationItem struct {
	MigrationID string
	LegacyID    string
	Kind        string
	Name        string
	Status      string
	RuleID      string
	// EpisodeID is set only for an adopted alarm.
	EpisodeID  string
	ReasonCode string
	Reason     string
	Definition domain.Definition
	// HasDefinition distinguishes "no definition" from "an empty one", which
	// matters because an empty definition is invalid and must not be applied.
	HasDefinition bool
	// Alarm is the payload of an alarm item, nil for the other kinds.
	Alarm *AlarmAdoption
	// Enabled is the switch the migrated rule is created with. Nil means "on",
	// which is what a scene is: the old scene table has no enable column at all,
	// and a rule that a snapshot never mentioned the switch for must not arrive
	// disabled.
	Enabled   *bool
	UpdatedAt time.Time
}

// RuleEnabled reports the switch the migrated rule should be created with.
func (m *MigrationItem) RuleEnabled() bool {
	return m.Enabled == nil || *m.Enabled
}

// CreateMigration stores a run and its items in one transaction.
//
// All or nothing: a run whose items were only half written would report a total
// that does not match what it can apply, and the operator would be reconciling
// two numbers that were never the same thing.
func (s *Store) CreateMigration(ctx context.Context, m *Migration, items []*MigrationItem) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO auto_migrations
                (id, tenant_id, name, status, snapshot_hash, total, applied, skipped, failed, unresolved)
            VALUES ($1,$2,$3,$4,$5,$6,0,0,0,$7)`,
			m.ID, m.TenantID, m.Name, m.Status, m.SnapshotHash, len(items), countUnresolved(items)); err != nil {
			return fmt.Errorf("insert migration: %w", err)
		}
		for _, it := range items {
			if err := insertMigrationItem(ctx, tx, it); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertMigrationItem(ctx context.Context, tx *sql.Tx, it *MigrationItem) error {
	var def any
	if it.HasDefinition {
		raw, err := json.Marshal(it.Definition)
		if err != nil {
			return err
		}
		def = raw
	}
	var payload any
	if it.Alarm != nil {
		raw, err := json.Marshal(it.Alarm)
		if err != nil {
			return err
		}
		payload = raw
	}
	_, err := tx.ExecContext(ctx, `
        INSERT INTO auto_migration_items
            (migration_id, legacy_id, kind, name, status, rule_id, episode_id,
             reason_code, reason, definition, payload, enabled)
        VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,''),
                NULLIF($8,''),NULLIF($9,''),$10,$11,$12)`,
		it.MigrationID, it.LegacyID, it.Kind, it.Name, it.Status, it.RuleID,
		it.EpisodeID, it.ReasonCode, it.Reason, def, payload, it.RuleEnabled())
	if err != nil {
		return fmt.Errorf("insert migration item %s: %w", it.LegacyID, err)
	}
	return nil
}

func countUnresolved(items []*MigrationItem) int {
	n := 0
	for _, it := range items {
		if it.Status == MigrationUnsupported || it.Status == MigrationAmbiguous {
			n++
		}
	}
	return n
}

// GetMigration reads one run.
func (s *Store) GetMigration(ctx context.Context, tenantID, id string) (*Migration, error) {
	var m Migration
	err := s.DB.QueryRowContext(ctx, `
        SELECT id::text, tenant_id, name, status, snapshot_hash, total, applied, skipped, failed, unresolved,
               created_at, updated_at
          FROM auto_migrations WHERE tenant_id=$1 AND id=$2`, tenantID, id).
		Scan(&m.ID, &m.TenantID, &m.Name, &m.Status, &m.SnapshotHash, &m.Total,
			&m.Applied, &m.Skipped, &m.Failed, &m.Unresolved, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMigrationItems returns the items of a run, optionally filtered by status.
func (s *Store) ListMigrationItems(ctx context.Context, migrationID, status string, limit int) ([]*MigrationItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT migration_id::text, legacy_id, kind, name, status,
                 coalesce(rule_id::text,''), coalesce(episode_id,''),
                 coalesce(reason_code,''), coalesce(reason,''),
                 definition, payload, enabled, updated_at
            FROM auto_migration_items WHERE migration_id=$1`
	args := []any{migrationID}
	if status != "" {
		q += ` AND status=$2`
		args = append(args, status)
	}
	q += fmt.Sprintf(` ORDER BY kind, legacy_id LIMIT %d`, limit)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrationItems(rows)
}

// NextMigrationBatch returns the next items to apply.
//
// Planned and failed are both eligible: a failed item is retried on the next
// call, which is what makes "run the apply endpoint again" the recovery step
// instead of "delete the run and start over". Unsupported and ambiguous are
// never returned — they need a human, and retrying them would only inflate the
// failure count.
func (s *Store) NextMigrationBatch(ctx context.Context, migrationID string, limit int) ([]*MigrationItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`
        SELECT migration_id::text, legacy_id, kind, name, status,
               coalesce(rule_id::text,''), coalesce(episode_id,''),
               coalesce(reason_code,''), coalesce(reason,''),
               definition, payload, enabled, updated_at
          FROM auto_migration_items
         WHERE migration_id=$1 AND status IN ('planned','failed')
         ORDER BY kind, legacy_id LIMIT %d`, limit), migrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMigrationItems(rows)
}

func scanMigrationItems(rows *sql.Rows) ([]*MigrationItem, error) {
	var out []*MigrationItem
	for rows.Next() {
		var it MigrationItem
		var def, payload []byte
		if err := rows.Scan(&it.MigrationID, &it.LegacyID, &it.Kind, &it.Name, &it.Status,
			&it.RuleID, &it.EpisodeID, &it.ReasonCode, &it.Reason, &def, &payload,
			&it.Enabled, &it.UpdatedAt); err != nil {
			return nil, err
		}
		if len(def) > 0 {
			if err := json.Unmarshal(def, &it.Definition); err != nil {
				return nil, err
			}
			it.HasDefinition = true
		}
		if len(payload) > 0 {
			var a AlarmAdoption
			if err := json.Unmarshal(payload, &a); err != nil {
				return nil, err
			}
			it.Alarm = &a
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

// SetMigrationItemStatus records the outcome of one item.
func (s *Store) SetMigrationItemStatus(ctx context.Context, migrationID, legacyID, status, ruleID, reasonCode, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_migration_items
           SET status=$3, rule_id=coalesce(NULLIF($4,'')::uuid, rule_id),
               reason_code=NULLIF($5,''), reason=NULLIF($6,''), updated_at=now()
         WHERE migration_id=$1 AND legacy_id=$2`,
		migrationID, legacyID, status, ruleID, reasonCode, reason)
	return err
}

// SetMigrationAlarmOutcome records the result of adopting one alarm.
func (s *Store) SetMigrationAlarmOutcome(ctx context.Context, migrationID, legacyID, status, episodeID, reasonCode, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
        UPDATE auto_migration_items
           SET status=$3, episode_id=coalesce(NULLIF($4,''), episode_id),
               reason_code=NULLIF($5,''), reason=NULLIF($6,''), updated_at=now()
         WHERE migration_id=$1 AND legacy_id=$2`,
		migrationID, legacyID, status, episodeID, reasonCode, reason)
	return err
}

// RecomputeMigrationCounts refreshes the denormalised counters from the items.
//
// The counters are derived, never incremented: an interrupted apply leaves
// increments that were never committed, and a report that is off by one item is
// a report the operator stops trusting. Recomputing from the rows makes the
// numbers self-healing after any crash.
func (s *Store) RecomputeMigrationCounts(ctx context.Context, migrationID string) (*Migration, error) {
	var applied, skipped, failed, unresolved int
	err := s.DB.QueryRowContext(ctx, `
        SELECT
          count(*) FILTER (WHERE status='applied'),
          count(*) FILTER (WHERE status='skipped'),
          count(*) FILTER (WHERE status='failed'),
          count(*) FILTER (WHERE status IN ('unsupported','ambiguous'))
        FROM auto_migration_items WHERE migration_id=$1`, migrationID).
		Scan(&applied, &skipped, &failed, &unresolved)
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.ExecContext(ctx, `
        UPDATE auto_migrations
           SET applied=$2, skipped=$3, failed=$4, unresolved=$5, updated_at=now()
         WHERE id=$1`, migrationID, applied, skipped, failed, unresolved); err != nil {
		return nil, err
	}
	var m Migration
	err = s.DB.QueryRowContext(ctx, `
        SELECT id::text, tenant_id, name, status, snapshot_hash, total, applied, skipped, failed, unresolved,
               created_at, updated_at
          FROM auto_migrations WHERE id=$1`, migrationID).
		Scan(&m.ID, &m.TenantID, &m.Name, &m.Status, &m.SnapshotHash, &m.Total,
			&m.Applied, &m.Skipped, &m.Failed, &m.Unresolved, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// SetMigrationStatus changes the run's status.
func (s *Store) SetMigrationStatus(ctx context.Context, migrationID, status string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE auto_migrations SET status=$2, updated_at=now() WHERE id=$1`, migrationID, status)
	return err
}

// MigrationItemByLegacyID reads one item.
func (s *Store) MigrationItemByLegacyID(ctx context.Context, migrationID, legacyID string) (*MigrationItem, error) {
	items, err := s.ListMigrationItems(ctx, migrationID, "", 500)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.LegacyID == legacyID {
			return it, nil
		}
	}
	return nil, ErrNotFound
}

// PendingMigrationCount reports how many items still need work.
//
// It is the difference between "the run is done" and "the run ran out of this
// batch". A run that stops after 50 of 300 items and says "applied" would be
// lying about the customer's data.
func (s *Store) PendingMigrationCount(ctx context.Context, migrationID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
        SELECT count(*) FROM auto_migration_items
         WHERE migration_id=$1 AND status IN ('planned','failed')`, migrationID).Scan(&n)
	return n, err
}

// CountDuplicateActiveAlarms counts (policy_key, subject, dimension) that have
// more than one active episode.
//
// The partial unique index makes this impossible, which is exactly why it is
// worth counting: after an import the operator needs to know the index is still
// doing its job. A migration that opened a second episode for a problem the
// customer already had is the one failure mode that shows up as "the alarm list
// doubled" and is very hard to unpick afterwards.
func (s *Store) CountDuplicateActiveAlarms(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
        SELECT count(*) FROM (
            SELECT 1 FROM alarm_episodes
             WHERE tenant_id=$1 AND state='active'
             GROUP BY policy_key, subject_id, dimension
            HAVING count(*) > 1
        ) d`, tenantID).Scan(&n)
	return n, err
}

// RuleRunCount reports how many runs a rule has produced.
//
// Rollback uses it as the boundary: a rule that already ran is the only record
// that a command was sent, and deleting it would delete that record.
func (s *Store) RuleRunCount(ctx context.Context, tenantID, ruleID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM auto_runs WHERE tenant_id=$1 AND rule_id=$2`, tenantID, ruleID).Scan(&n)
	return n, err
}

// RulesByLegacyIDs returns every live rule whose legacy_id is in the set.
//
// Verify uses it to find orphans: a rule carrying a legacy id from this run that
// has no matching item means the mapping was written twice, and the operator has
// two rules where the old console had one.
func (s *Store) RulesByLegacyIDs(ctx context.Context, tenantID string, legacyIDs []string) ([]*Rule, error) {
	if len(legacyIDs) == 0 {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
        SELECT id FROM auto_rules
         WHERE tenant_id=$1 AND deleted_at IS NULL AND legacy_id = ANY($2)
         ORDER BY legacy_id, created_at`, tenantID, legacyIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Rule
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		r, err := s.GetRule(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EpisodeByID reads one episode regardless of state.
func (s *Store) EpisodeByID(ctx context.Context, tenantID, id string) (*Episode, error) {
	e, err := scanEpisode(s.DB.QueryRowContext(ctx,
		`SELECT `+episodeColumns+` FROM alarm_episodes WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

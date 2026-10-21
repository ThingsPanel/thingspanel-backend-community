package northbound

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SourceStatus is what the Community runtime knows about the handoff. It is
// deliberately separate from the northbound service's own overview: "enrolled
// and waiting" is a source-side fact, "capacity rejected" is also a source-side
// fact, and neither may be inferred from the other.
type SourceStatus struct {
	Enabled           bool         `json:"enabled"`
	SourceProfile     string       `json:"sourceProfile"`
	EncoreConfigured  bool         `json:"encoreConfigured"`
	SchemaReady       bool         `json:"schemaReady"`
	Outbox            OutboxCounts `json:"outbox"`
	Rejects           RejectCounts `json:"rejects"`
	Relay             RelayState   `json:"relay"`
	SubscribedTenants int64        `json:"subscribedTenants"`
	CheckedAt         time.Time    `json:"checkedAt"`
}

type OutboxCounts struct {
	Pending      int64      `json:"pending"`
	Leased       int64      `json:"leased"`
	HandedOff    int64      `json:"handedOff"`
	Failed       int64      `json:"failed"`
	OldestAt     *time.Time `json:"oldestAt,omitempty"`
	OldestAgeMS  int64      `json:"oldestAgeMs"`
	PayloadBytes int64      `json:"payloadBytes"`
}

type RejectCounts struct {
	Capacity     int64      `json:"capacity"`
	Invalid      int64      `json:"invalid"`
	LastRejectAt *time.Time `json:"lastRejectAt,omitempty"`
}

type RelayState struct {
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
	LastErrorAt   *time.Time `json:"lastErrorAt,omitempty"`
}

// SchemaReady reports whether the northbound source tables exist. A deployment
// that never ran the SQL must say so instead of failing every request.
func SchemaReady(ctx context.Context, db *gorm.DB) bool {
	if db == nil {
		return false
	}
	var count int64
	err := db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).
		Raw(`SELECT count(*) FROM unnest(ARRAY[
			to_regclass('northbound_source_outbox'),
			to_regclass('northbound_source_rejects'),
			to_regclass('northbound_source_tenants'),
			to_regclass('northbound_source_relay_state')]) AS t(rel) WHERE rel IS NOT NULL`).Scan(&count).Error
	return err == nil && count == 4
}

// LoadSourceStatus gathers the source-side picture for the page.
func LoadSourceStatus(ctx context.Context, db *gorm.DB) SourceStatus {
	cfg := Current()
	status := SourceStatus{
		Enabled:          cfg.Enabled,
		SourceProfile:    cfg.SourceProfile,
		EncoreConfigured: cfg.EncoreBaseURL != "" && cfg.ServiceToken != "",
		CheckedAt:        time.Now().UTC(),
	}
	if db == nil {
		return status
	}
	status.SchemaReady = SchemaReady(ctx, db)
	if !status.SchemaReady {
		return status
	}
	quiet := db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	_ = quiet.Raw(`SELECT
			count(*) FILTER (WHERE state = 'pending'),
			count(*) FILTER (WHERE state = 'leased'),
			count(*) FILTER (WHERE state = 'handed_off'),
			count(*) FILTER (WHERE state = 'failed'),
			min(created_at) FILTER (WHERE state IN ('pending','leased')),
			coalesce(sum(payload_bytes) FILTER (WHERE state IN ('pending','leased')), 0)
		FROM northbound_source_outbox`).
		Scan(&status.Outbox).Error
	if status.Outbox.OldestAt != nil {
		status.Outbox.OldestAgeMS = time.Since(*status.Outbox.OldestAt).Milliseconds()
	}
	_ = quiet.Raw(`SELECT
			coalesce(sum(count) FILTER (WHERE reason = 'capacity_exceeded'), 0),
			coalesce(sum(count) FILTER (WHERE reason = 'invalid_event'), 0),
			max(last_at)
		FROM northbound_source_rejects`).
		Scan(&status.Rejects).Error
	_ = quiet.Raw(`SELECT last_success_at, coalesce(last_error, ''), last_error_at
		FROM northbound_source_relay_state WHERE id = 1`).
		Scan(&status.Relay).Error
	_ = quiet.Raw(`SELECT count(*) FROM northbound_source_tenants WHERE active_pipelines > 0`).
		Scan(&status.SubscribedTenants).Error
	return status
}

// PruneSourceOutbox removes finished handoff rows past retention. Pending rows
// are never removed, so a long Encore outage cannot silently destroy a backlog.
func PruneSourceOutbox(ctx context.Context, db *gorm.DB) error {
	if db == nil || !SchemaReady(ctx, db) {
		return nil
	}
	return db.WithContext(ctx).Exec(`DELETE FROM northbound_source_outbox
		WHERE state IN ('handed_off','failed') AND expires_at < now()`).Error
}

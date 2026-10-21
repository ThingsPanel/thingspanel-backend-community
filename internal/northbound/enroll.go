package northbound

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Event types accepted by the northbound handoff. These mirror the northbound
// service contract and the streams the Community runtime can actually persist.
const (
	TypeTelemetry = "telemetry"
	TypeAttribute = "attribute"
	TypeEvent     = "event"
)

// Reject reasons. Only "capacity_exceeded" means "we chose not to keep it";
// everything else is a defect that must be visible.
const (
	RejectCapacity = "capacity_exceeded"
	RejectInvalid  = "invalid_event"
)

// TelemetryInput is one logical device's telemetry values that were just
// committed inside the trusted telemetry batch transaction.
type TelemetryInput struct {
	TenantID   string
	DeviceID   string
	ReceiptID  string
	OccurredAt time.Time
	Values     map[string]interface{}
}

// BusinessInput is one reliable attribute or event write.
type BusinessInput struct {
	TenantID   string
	DeviceID   string
	MessageID  string
	Family     string // attribute | event
	OccurredAt time.Time
	Values     map[string]interface{}
}

// EventIDFor derives the stable event identity at the source boundary. The same
// receipt (or message) and device always produce the same id, so a retried
// commit, a replay, or a duplicate bridge attempt cannot create a second event.
func EventIDFor(receiptID, deviceID, kind string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("northbound-event\x00"+receiptID+"\x00"+deviceID+"\x00"+kind)).String()
}

// EnrollTelemetry records one device's telemetry event for northbound handoff.
// It must be called inside the transaction that commits the telemetry rows.
//
// Returning an error means the transaction should fail: the outbox row and the
// telemetry rows are one unit. Capacity rejection is NOT an error; it is
// recorded separately so the two situations stay distinguishable.
func EnrollTelemetry(ctx context.Context, tx *gorm.DB, in TelemetryInput) error {
	if len(in.Values) == 0 {
		return nil
	}
	return enroll(ctx, tx, enrollArgs{
		TenantID:   in.TenantID,
		DeviceID:   in.DeviceID,
		EventID:    EventIDFor(in.ReceiptID, in.DeviceID, TypeTelemetry),
		EventType:  TypeTelemetry,
		OccurredAt: in.OccurredAt,
		Data:       in.Values,
	})
}

// EnrollBusinessEvent records one reliable attribute or event write.
func EnrollBusinessEvent(ctx context.Context, tx *gorm.DB, in BusinessInput) error {
	if len(in.Values) == 0 {
		return nil
	}
	kind := in.Family
	if kind != TypeAttribute && kind != TypeEvent {
		return nil
	}
	return enroll(ctx, tx, enrollArgs{
		TenantID:   in.TenantID,
		DeviceID:   in.DeviceID,
		EventID:    EventIDFor(in.MessageID, in.DeviceID, kind),
		EventType:  kind,
		OccurredAt: in.OccurredAt,
		Data:       in.Values,
	})
}

type enrollArgs struct {
	TenantID   string
	DeviceID   string
	EventID    string
	EventType  string
	OccurredAt time.Time
	Data       map[string]interface{}
}

// enroll is the single write path into the source outbox.
func enroll(ctx context.Context, tx *gorm.DB, a enrollArgs) error {
	cfg := Current()
	if !cfg.Enabled {
		return nil
	}
	if tx == nil {
		return errors.New("northbound enrollment requires a transaction")
	}
	if a.TenantID == "" || a.DeviceID == "" || a.EventID == "" {
		return nil
	}
	quiet := tx.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})

	// Only tenants that currently have an active forwarding task pay the cost.
	// The cache is derived from the northbound service and refreshed by the
	// relay; when the refresh fails the previous value is kept, so an outage
	// slows the handoff instead of silently dropping data.
	var subscribed int64
	if err := quiet.Raw(`SELECT count(*) FROM northbound_source_tenants
		WHERE tenant_id = ? AND active_pipelines > 0`, a.TenantID).Scan(&subscribed).Error; err != nil {
		return err
	}
	if subscribed == 0 {
		return nil
	}

	// Capacity is checked with a bounded count: the subquery stops at the limit,
	// so this does not scan an unbounded backlog on every write.
	var backlog int64
	if err := quiet.Raw(`SELECT count(*) FROM (
			SELECT 1 FROM northbound_source_outbox
			WHERE tenant_id = ? AND state IN ('pending','leased')
			LIMIT ?
		) AS bounded`, a.TenantID, cfg.MaxBacklogEvents+1).Scan(&backlog).Error; err != nil {
		return err
	}
	if backlog >= int64(cfg.MaxBacklogEvents) {
		return recordReject(quiet, a.TenantID, RejectCapacity)
	}

	payload, err := json.Marshal(a.Data)
	if err != nil {
		return recordReject(quiet, a.TenantID, RejectInvalid)
	}
	if len(payload) > cfg.MaxEventBytes {
		// A single event larger than the contract limit cannot be stored.
		return recordReject(quiet, a.TenantID, RejectInvalid)
	}
	occurred := a.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	expires := time.Now().UTC().Add(cfg.Retention)
	return quiet.Exec(`INSERT INTO northbound_source_outbox
			(id, tenant_id, device_id, event_type, event_id, payload, payload_bytes, occurred_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?)
		ON CONFLICT (tenant_id, event_id) DO NOTHING`,
		uuid.NewSHA1(uuid.NameSpaceOID, []byte("northbound-outbox\x00"+a.TenantID+"\x00"+a.EventID)).String(),
		a.TenantID, a.DeviceID, a.EventType, a.EventID, string(payload), len(payload), occurred.UTC(), expires).Error
}

// recordReject counts an event that was never enrolled. It must never be
// reported to a user as "waiting to be sent".
func recordReject(tx *gorm.DB, tenantID, reason string) error {
	return tx.Exec(`INSERT INTO northbound_source_rejects (tenant_id, bucket, reason, count, last_at)
		VALUES (?, date_trunc('minute', now()), ?, 1, now())
		ON CONFLICT (tenant_id, bucket, reason)
		DO UPDATE SET count = northbound_source_rejects.count + 1, last_at = now()`, tenantID, reason).Error
}

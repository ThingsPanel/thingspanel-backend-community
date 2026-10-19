// Package brokerledger provides a durable, tenant-scoped acceptance ledger for
// MQTT ingress. It records acceptance only; it does not run workers or claim a
// physical device action completed.
package brokerledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrBindingInactive = errors.New("brokerledger: transport binding is not active")
	ErrFenced          = errors.New("brokerledger: route epoch is fenced")
	ErrRouteRetired    = errors.New("brokerledger: route was durably retired by an audited migration")
	ErrConflict        = errors.New("brokerledger: source event conflicts with accepted receipt")
	ErrTooLarge        = errors.New("brokerledger: message exceeds size limit")
	ErrTenantCapacity  = errors.New("brokerledger: tenant pending capacity exceeded")
	ErrNotFound        = errors.New("brokerledger: receipt not found")
	ErrInvalid         = errors.New("brokerledger: invalid business message")
)

const (
	statePending        = "pending"
	stateProcessed      = "processed"
	stateDead           = "dead"
	stateFenced         = "fenced"
	defaultMessageLimit = 1 << 20
	defaultPendingCount = int64(10000)
	defaultPendingBytes = int64(64 << 20)
	defaultMaxAttempts  = 5
	MaxMetadataBytes    = 256 << 10
)

// Message is the trusted, already parsed business message accepted from MQTT.
type Message struct {
	ID            string         `json:"id,omitempty"`
	TenantID      string         `json:"tenant_id"`
	DeviceID      string         `json:"device_id"`
	Stream        string         `json:"stream"`
	SourceEventID string         `json:"source_event_id,omitempty"`
	BrokerCell    string         `json:"broker_cell"`
	RouteEpoch    uint64         `json:"route_epoch"`
	Payload       []byte         `json:"payload"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// Receipt is a durable record returned by scoped receipt reads.
type Receipt struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	DeviceID      string         `json:"device_id"`
	Stream        string         `json:"stream"`
	SourceEventID string         `json:"source_event_id,omitempty"`
	BrokerCell    string         `json:"broker_cell"`
	RouteEpoch    uint64         `json:"route_epoch"`
	Payload       []byte         `json:"payload"`
	Metadata      map[string]any `json:"metadata"`
	State         string         `json:"state"`
	Attempts      int            `json:"attempts"`
	RetryAfter    time.Time      `json:"retry_after"`
	LastError     string         `json:"last_error,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	ProcessedAt   *time.Time     `json:"processed_at,omitempty"`
}

// Limits are per-store safeguards. Production defaults are conservative and
// tests may use small limits to exercise admission control.
type Limits struct {
	MaxMessageBytes int
	MaxPendingCount int64
	MaxPendingBytes int64
	MaxAttempts     int
}

type Store struct {
	db                       *gorm.DB
	limits                   Limits
	quarantineCountLimit     int64
	quarantineBodyBytesLimit int64
}

func New(db *gorm.DB) *Store { return NewWithLimits(db, Limits{}) }

func NewWithLimits(db *gorm.DB, limits Limits) *Store {
	if limits.MaxMessageBytes <= 0 {
		limits.MaxMessageBytes = defaultMessageLimit
	}
	if limits.MaxPendingCount <= 0 {
		limits.MaxPendingCount = defaultPendingCount
	}
	if limits.MaxPendingBytes <= 0 {
		limits.MaxPendingBytes = defaultPendingBytes
	}
	if limits.MaxAttempts <= 0 {
		limits.MaxAttempts = defaultMaxAttempts
	}
	if db != nil {
		db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	}
	return &Store{db: db, limits: limits, quarantineCountLimit: QuarantineTenantCountLimit, quarantineBodyBytesLimit: QuarantineTenantBodyBytesLimit}
}

type receiptRow struct {
	ID            string         `gorm:"column:id;type:uuid;primaryKey"`
	TenantID      string         `gorm:"column:tenant_id"`
	DeviceID      string         `gorm:"column:device_id"`
	Stream        string         `gorm:"column:stream"`
	SourceEventID *string        `gorm:"column:source_event_id"`
	BrokerCell    string         `gorm:"column:broker_cell"`
	RouteEpoch    int64          `gorm:"column:route_epoch"`
	Payload       []byte         `gorm:"column:payload"`
	Metadata      datatypes.JSON `gorm:"column:metadata;type:jsonb"`
	CanonicalSHA  []byte         `gorm:"column:canonical_sha256"`
	State         string         `gorm:"column:state"`
	Attempts      int            `gorm:"column:attempts"`
	RetryAfter    time.Time      `gorm:"column:retry_after"`
	LastError     string         `gorm:"column:last_error"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	ProcessedAt   *time.Time     `gorm:"column:processed_at"`
}

func (receiptRow) TableName() string { return "broker_ingress_receipts" }

type bindingRow struct {
	TenantID   string `gorm:"column:tenant_id"`
	DeviceID   string `gorm:"column:device_id"`
	BrokerCell string `gorm:"column:broker_cell"`
	RouteEpoch int64  `gorm:"column:route_epoch"`
	Phase      string `gorm:"column:phase"`
}

func (bindingRow) TableName() string { return "broker_transport_bindings" }

// Accept commits a pending receipt only when the current transport binding is
// active and matches the message's cell and route epoch.
func (s *Store) Accept(ctx context.Context, msg Message) (id string, duplicate bool, err error) {
	if s == nil || s.db == nil {
		return "", false, errors.New("brokerledger: database is nil")
	}
	if ctx == nil {
		return "", false, errors.New("brokerledger: context is nil")
	}
	if err := validateMessage(msg); err != nil {
		return "", false, err
	}
	metadata, canonical, err := canonicalMetadata(msg.Metadata)
	if err != nil {
		return "", false, err
	}
	// Wire payload and trusted transport metadata have separate bounds. Base64
	// and metadata overhead must not shrink the advertised business payload cap.
	if len(msg.Payload) > s.limits.MaxMessageBytes || len(metadata) > MaxMetadataBytes {
		return "", false, ErrTooLarge
	}
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	} else if _, parseErr := uuid.Parse(msg.ID); parseErr != nil {
		return "", false, fmt.Errorf("%w: invalid receipt id", ErrInvalid)
	}
	digest, err := canonicalDigest(msg, canonical)
	if err != nil {
		return "", false, err
	}
	row := receiptRow{
		ID: msg.ID, TenantID: msg.TenantID, DeviceID: msg.DeviceID, Stream: msg.Stream,
		BrokerCell: msg.BrokerCell, RouteEpoch: int64(msg.RouteEpoch), Payload: append([]byte(nil), msg.Payload...),
		Metadata: datatypes.JSON(metadata), CanonicalSHA: digest[:], State: statePending,
		RetryAfter: time.Now().UTC(), CreatedAt: time.Now().UTC(),
	}
	if msg.SourceEventID != "" {
		sourceID := msg.SourceEventID
		row.SourceEventID = &sourceID
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTenantCapacity(tx, msg.TenantID); err != nil {
			return err
		}
		if err := requireAdmissionBinding(tx, msg.TenantID, msg.DeviceID, msg.BrokerCell, int64(msg.RouteEpoch)); err != nil {
			return err
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing receiptRow
			lookup := tx.Where("tenant_id = ? AND device_id = ? AND stream = ?", msg.TenantID, msg.DeviceID, msg.Stream)
			if msg.SourceEventID != "" {
				lookup = lookup.Where("source_event_id = ?", msg.SourceEventID)
			} else {
				lookup = lookup.Where("id = ? AND broker_cell = ? AND route_epoch = ? AND source_event_id IS NULL", msg.ID, msg.BrokerCell, msg.RouteEpoch)
			}
			lookup = lookup.First(&existing)
			if lookup.Error != nil {
				if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
					return ErrConflict
				}
				return lookup.Error
			}
			if !equalDigest(existing.CanonicalSHA, digest[:]) {
				return ErrConflict
			}
			id, duplicate = existing.ID, true
			return nil
		}

		var usage struct {
			Count int64 `gorm:"column:count"`
			Bytes int64 `gorm:"column:bytes"`
		}
		if err := tx.Model(&receiptRow{}).
			Select("count(*) AS count, coalesce(sum(octet_length(payload) + octet_length(metadata::text)), 0) AS bytes").
			Where("tenant_id = ? AND state = ?", msg.TenantID, statePending).Scan(&usage).Error; err != nil {
			return err
		}
		if usage.Count > s.limits.MaxPendingCount || usage.Bytes > s.limits.MaxPendingBytes {
			return ErrTenantCapacity
		}
		id = row.ID
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return id, duplicate, nil
}

func validateMessage(msg Message) error {
	if msg.TenantID == "" || len(msg.TenantID) > 36 || msg.DeviceID == "" || len(msg.DeviceID) > 36 ||
		msg.Stream == "" || len(msg.Stream) > 64 || msg.BrokerCell == "" || len(msg.BrokerCell) > 128 ||
		msg.RouteEpoch == 0 || msg.RouteEpoch > math.MaxInt64 {
		return fmt.Errorf("%w: identity or route", ErrInvalid)
	}
	if len(msg.SourceEventID) > 256 {
		return fmt.Errorf("%w: source event id exceeds limit", ErrInvalid)
	}
	return nil
}

func canonicalMetadata(input map[string]any) ([]byte, []byte, error) {
	if input == nil {
		input = map[string]any{}
	}
	cleaned, err := stripTransportVolatility(input)
	if err != nil {
		return nil, nil, fmt.Errorf("brokerledger: encode metadata: %w", err)
	}
	metadata, err := json.Marshal(input)
	if err != nil {
		return nil, nil, fmt.Errorf("brokerledger: encode metadata: %w", err)
	}
	canonical, err := json.Marshal(cleaned)
	if err != nil {
		return nil, nil, fmt.Errorf("brokerledger: encode canonical metadata: %w", err)
	}
	return metadata, canonical, nil
}

var volatileMetadataKeys = map[string]struct{}{
	"broker_instance": {}, "connection_id": {}, "observed_at": {}, "received_at": {}, "ingestion_id": {},
	"timestamp":   {},
	"route_epoch": {}, "broker_cell": {},
}

func stripTransportVolatility(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if _, volatile := volatileMetadataKeys[strings.ToLower(key)]; volatile {
				continue
			}
			clean, err := stripTransportVolatility(item)
			if err != nil {
				return nil, err
			}
			out[key] = clean
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			clean, err := stripTransportVolatility(item)
			if err != nil {
				return nil, err
			}
			out[i] = clean
		}
		return out, nil
	default:
		// Round-trip through JSON to handle typed maps/slices and reject values
		// that cannot be represented in the durable receipt contract.
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var normalized any
		if err := json.Unmarshal(data, &normalized); err != nil {
			return nil, err
		}
		return normalized, nil
	}
}

func canonicalDigest(msg Message, stableMetadata []byte) ([32]byte, error) {
	body, err := json.Marshal(struct {
		TenantID      string          `json:"tenant_id"`
		DeviceID      string          `json:"device_id"`
		Stream        string          `json:"stream"`
		SourceEventID string          `json:"source_event_id,omitempty"`
		Payload       []byte          `json:"payload"`
		Metadata      json.RawMessage `json:"metadata"`
	}{msg.TenantID, msg.DeviceID, msg.Stream, msg.SourceEventID, msg.Payload, stableMetadata})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for i := range a {
		different |= a[i] ^ b[i]
	}
	return different == 0
}

func lockTenantCapacity(tx *gorm.DB, tenantID string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", tenantID).Error
}

func requireAdmissionBinding(tx *gorm.DB, tenantID, deviceID, cell string, epoch int64) error {
	var binding bindingRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND device_id = ?", tenantID, deviceID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrBindingInactive
	}
	if err != nil {
		return err
	}
	if (binding.Phase != "active" && binding.Phase != "connecting") || binding.BrokerCell != cell || binding.RouteEpoch != epoch {
		retired, e := auditedRouteRetired(tx, tenantID, deviceID, cell, epoch, binding)
		if e != nil {
			return e
		}
		if retired {
			return ErrRouteRetired
		}
		if binding.Phase != "active" && binding.Phase != "connecting" {
			return ErrBindingInactive
		}
		return ErrFenced
	}
	return nil
}

// ProcessOne claims at most one due receipt. The handler must write business
// records and any business-side outbox rows through tx; those writes commit
// atomically with the original receipt's processed state. No worker is started
// by this package and a committed receipt is not proof of physical device effect.
func (s *Store) ProcessOne(ctx context.Context, handler func(context.Context, *gorm.DB, Message) error) (processed bool, err error) {
	return s.processOne(ctx, "", handler)
}

// ProcessOneInCell keeps the business response/telemetry bus in the same cell
// as its MQTT Adapter. A backend replica must not steal another cell's work.
func (s *Store) ProcessOneInCell(ctx context.Context, cell string, handler func(context.Context, *gorm.DB, Message) error) (bool, error) {
	if strings.TrimSpace(cell) == "" {
		return false, ErrInvalid
	}
	return s.processOne(ctx, cell, handler)
}

func (s *Store) processOne(ctx context.Context, cell string, handler func(context.Context, *gorm.DB, Message) error) (processed bool, err error) {
	if s == nil || s.db == nil {
		return false, errors.New("brokerledger: database is nil")
	}
	if ctx == nil || handler == nil {
		return false, errors.New("brokerledger: context and handler are required")
	}
	var candidate receiptRow
	query := s.db.WithContext(ctx).Table("broker_ingress_receipts AS r").Select("r.*").
		Joins("LEFT JOIN broker_transport_bindings b ON b.tenant_id=r.tenant_id AND b.device_id=r.device_id").
		Where("r.state = ? AND r.retry_after <= now() AND (b.device_id IS NULL OR b.phase<>'connecting' OR b.broker_cell<>r.broker_cell OR b.route_epoch<>r.route_epoch)", statePending)
	if cell != "" {
		query = query.Where("r.broker_cell=?", cell)
	}
	err = query.Order("r.created_at, r.id").Take(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var claimed receiptRow
	var handlerErr error
	var fenced bool
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTenantCapacity(tx, candidate.TenantID); err != nil {
			return err
		}
		binding, bindingFound, bindingBusy, err := lockBindingSkipLocked(tx, candidate.TenantID, candidate.DeviceID)
		if err != nil {
			return err
		}
		if bindingBusy {
			return nil
		}
		lock := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND state = ? AND retry_after <= now()", candidate.ID, statePending).First(&claimed)
		if errors.Is(lock.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if lock.Error != nil {
			return lock.Error
		}
		if cell != "" && claimed.BrokerCell != cell {
			return nil
		}
		if bindingFound && binding.Phase == "connecting" && binding.BrokerCell == claimed.BrokerCell && binding.RouteEpoch == claimed.RouteEpoch {
			return nil
		}
		processed = true
		if !bindingFound || binding.Phase != "active" || binding.BrokerCell != claimed.BrokerCell || binding.RouteEpoch != claimed.RouteEpoch {
			now := time.Now().UTC()
			fenced = true
			return tx.Model(&receiptRow{}).Where("id = ? AND state = ?", claimed.ID, statePending).Updates(map[string]any{
				"state": stateFenced, "last_error": "transport binding no longer matches receipt route", "processed_at": now,
			}).Error
		}
		message, err := rowToMessage(claimed)
		if err != nil {
			return err
		}
		if err := handler(ctx, tx, message); err != nil {
			handlerErr = err
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&receiptRow{}).Where("id = ? AND state = ?", claimed.ID, statePending).Updates(map[string]any{
			"state": stateProcessed, "processed_at": now, "last_error": "",
		}).Error
	})
	if err != nil {
		if handlerErr == nil {
			return false, err
		}
		if recordErr := s.recordFailure(ctx, claimed.ID, handlerErr); recordErr != nil {
			return true, errors.Join(handlerErr, fmt.Errorf("record retry: %w", recordErr))
		}
		return true, handlerErr
	}
	if fenced {
		return true, nil
	}
	return processed, nil
}

func lockBindingSkipLocked(tx *gorm.DB, tenantID, deviceID string) (bindingRow, bool, bool, error) {
	var binding bindingRow
	query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("tenant_id = ? AND device_id = ?", tenantID, deviceID).First(&binding)
	if query.Error == nil {
		return binding, true, false, nil
	}
	if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return bindingRow{}, false, false, query.Error
	}
	var exists bool
	if err := tx.Model(&bindingRow{}).Select("count(*) > 0").
		Where("tenant_id = ? AND device_id = ?", tenantID, deviceID).Scan(&exists).Error; err != nil {
		return bindingRow{}, false, false, err
	}
	return bindingRow{}, false, exists, nil
}

func rowToMessage(row receiptRow) (Message, error) {
	var metadata map[string]any
	if len(row.Metadata) != 0 {
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return Message{}, err
		}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	message := Message{
		ID: row.ID, TenantID: row.TenantID, DeviceID: row.DeviceID, Stream: row.Stream,
		BrokerCell: row.BrokerCell, RouteEpoch: uint64(row.RouteEpoch), Payload: append([]byte(nil), row.Payload...), Metadata: metadata,
	}
	if row.SourceEventID != nil {
		message.SourceEventID = *row.SourceEventID
	}
	return message, nil
}

func (s *Store) recordFailure(ctx context.Context, id string, cause error) error {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	retryCtx, cancel := context.WithTimeout(base, 3*time.Second)
	defer cancel()
	message := cause.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	return s.db.WithContext(retryCtx).Transaction(func(tx *gorm.DB) error {
		var row receiptRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND state = ?", id, statePending).First(&row).Error; err != nil {
			return err
		}
		attempts := row.Attempts + 1
		state := statePending
		var processedAt *time.Time
		now := time.Now().UTC()
		if attempts >= s.limits.MaxAttempts {
			state = stateDead
			processedAt = &now
		}
		return tx.Model(&receiptRow{}).Where("id = ?", id).Updates(map[string]any{
			"attempts": attempts, "state": state, "retry_after": now.Add(retryDelay(attempts)),
			"last_error": message, "processed_at": processedAt,
		}).Error
	})
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Second * time.Duration(1<<(attempt-1))
}

// GetReceipt looks up one receipt only within the supplied tenant and device.
func (s *Store) GetReceipt(ctx context.Context, tenantID, deviceID, id string) (Receipt, error) {
	if s == nil || s.db == nil {
		return Receipt{}, errors.New("brokerledger: database is nil")
	}
	if ctx == nil {
		return Receipt{}, errors.New("brokerledger: context is nil")
	}
	var row receiptRow
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND device_id = ? AND id = ?", tenantID, deviceID, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	msg, err := rowToMessage(row)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{
		ID: row.ID, TenantID: row.TenantID, DeviceID: row.DeviceID, Stream: row.Stream,
		SourceEventID: msg.SourceEventID, BrokerCell: row.BrokerCell, RouteEpoch: uint64(row.RouteEpoch),
		Payload: msg.Payload, Metadata: msg.Metadata, State: row.State, Attempts: row.Attempts,
		RetryAfter: row.RetryAfter, LastError: row.LastError, CreatedAt: row.CreatedAt, ProcessedAt: row.ProcessedAt,
	}, nil
}

type Stats struct {
	PendingCount   int64 `json:"pending_count"`
	PendingBytes   int64 `json:"pending_bytes"`
	ProcessedCount int64 `json:"processed_count"`
	DeadCount      int64 `json:"dead_count"`
	FencedCount    int64 `json:"fenced_count"`
}

// Stats reports receipt totals for a single tenant only.
func (s *Store) Stats(ctx context.Context, tenantID string) (Stats, error) {
	if s == nil || s.db == nil {
		return Stats{}, errors.New("brokerledger: database is nil")
	}
	if ctx == nil || tenantID == "" {
		return Stats{}, errors.New("brokerledger: context and tenant id are required")
	}
	var stats Stats
	var rows []struct {
		State string `gorm:"column:state"`
		Count int64  `gorm:"column:count"`
		Bytes int64  `gorm:"column:bytes"`
	}
	err := s.db.WithContext(ctx).Model(&receiptRow{}).
		Select("state, count(*) AS count, coalesce(sum(octet_length(payload) + octet_length(metadata::text)), 0) AS bytes").
		Where("tenant_id = ?", tenantID).Group("state").Scan(&rows).Error
	if err != nil {
		return stats, err
	}
	for _, row := range rows {
		switch row.State {
		case statePending:
			stats.PendingCount, stats.PendingBytes = row.Count, row.Bytes
		case stateProcessed:
			stats.ProcessedCount = row.Count
		case stateDead:
			stats.DeadCount = row.Count
		case stateFenced:
			stats.FencedCount = row.Count
		}
	}
	return stats, nil
}

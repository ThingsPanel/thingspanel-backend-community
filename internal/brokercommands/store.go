// Package brokercommands stores durable command intents. It does not dispatch
// MQTT messages and never treats a broker acknowledgement as device execution.
package brokercommands

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	bc "project/internal/brokercontract"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrConflict = errors.New("brokercommands: idempotency key conflicts with another request")
	ErrNotFound = errors.New("brokercommands: command not found")
	ErrRejected = errors.New("brokercommands: request rejected")
	ErrCapacity = errors.New("brokercommands: tenant pending command capacity exceeded")
	ErrFenced   = errors.New("brokercommands: command route is fenced")
	ErrState    = errors.New("brokercommands: invalid command state transition")
	ErrInvalid  = errors.New("brokercommands: invalid request")
)

const (
	MaxCommandPayload          = bc.MaxCommandPayload
	StateAccepted              = "accepted"
	StateDispatching           = "dispatching"
	StateBrokerAccepted        = "broker_acknowledged"
	StateDeviceReportedSuccess = "device_reported_success"
	StateDeviceReportedFailure = "device_reported_failure"
	StateExpired               = "expired"
	StateFailed                = "failed"
	StateUnknown               = "unknown"
	StateFenced                = "fenced"
	maxField                   = 1 << 20
	maxPending                 = 10000
	maxTTL                     = 24 * time.Hour
)

// Request contains canonical business input plus its opaque serialized
// downlink. Store does not parse Payload or derive business identity from it.
type Request struct {
	TenantID         string
	CallerID         string
	Intent           string
	IdempotencyKey   string
	DeviceID         string
	TargetDeviceID   string
	CanonicalRequest []byte
	Payload          []byte
	ExpiresAt        time.Time
}

// Row is the durable command state and the route snapshot accepted at submit.
type Row struct {
	ID               string     `gorm:"column:id;type:uuid;primaryKey"`
	TenantID         string     `gorm:"column:tenant_id"`
	CallerID         string     `gorm:"column:caller_id"`
	Intent           string     `gorm:"column:intent"`
	IdempotencyKey   string     `gorm:"column:idempotency_key"`
	DeviceID         string     `gorm:"column:device_id"`
	TargetDeviceID   string     `gorm:"column:target_device_id"`
	RouteCell        string     `gorm:"column:route_cell"`
	RouteEpoch       int64      `gorm:"column:route_epoch"`
	CanonicalRequest []byte     `gorm:"column:canonical_request"`
	CanonicalSHA256  []byte     `gorm:"column:canonical_sha256"`
	Payload          []byte     `gorm:"column:payload"`
	ExpiresAt        time.Time  `gorm:"column:expires_at"`
	State            string     `gorm:"column:state"`
	LastError        string     `gorm:"column:last_error"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	DispatchedAt     *time.Time `gorm:"column:dispatched_at"`
	CompletedAt      *time.Time `gorm:"column:completed_at"`
}

func (Row) TableName() string { return "broker_command_requests" }

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	if db != nil {
		db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	}
	return &Store{db: db}
}

type deviceRow struct {
	ID        string  `gorm:"column:id"`
	TenantID  string  `gorm:"column:tenant_id"`
	IsEnabled string  `gorm:"column:is_enabled"`
	ParentID  *string `gorm:"column:parent_id"`
}

func (deviceRow) TableName() string { return "devices" }

type bindingRow struct {
	TenantID   string `gorm:"column:tenant_id"`
	DeviceID   string `gorm:"column:device_id"`
	BrokerCell string `gorm:"column:broker_cell"`
	RouteEpoch int64  `gorm:"column:route_epoch"`
	Phase      string `gorm:"column:phase"`
}

func (bindingRow) TableName() string { return "broker_transport_bindings" }

// Submit atomically admits an intent and runs persist for its command log.
// The persisted row ID is the command_id/message_id the caller must serialize
// into its eventual downlink; Store intentionally leaves Payload opaque.
func (s *Store) Submit(ctx context.Context, req Request, persist func(*gorm.DB, Row) error) (Row, bool, error) {
	if s == nil || s.db == nil || ctx == nil || persist == nil {
		return Row{}, false, ErrInvalid
	}
	if err := validateRequest(req); err != nil {
		return Row{}, false, err
	}
	if req.TargetDeviceID == "" {
		req.TargetDeviceID = req.DeviceID
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.NewString()
	}
	digest := sha256.Sum256(req.CanonicalRequest)
	now := time.Now().UTC()
	var result Row
	duplicate := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Row
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND caller_id = ? AND intent = ? AND idempotency_key = ?", req.TenantID, req.CallerID, req.Intent, req.IdempotencyKey).Take(&existing)
		if lookup.Error == nil {
			if !equalDigest(existing.CanonicalSHA256, digest[:]) {
				return ErrConflict
			}
			result, duplicate = existing, true
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if err := validateExpiry(req.ExpiresAt, time.Now()); err != nil {
			return err
		}
		if err := lockTenant(tx, req.TenantID); err != nil {
			return err
		}
		binding, err := activeBinding(tx, req.TenantID, req.TargetDeviceID)
		if err != nil {
			return err
		}
		if err := checkDevicePair(tx, req.TenantID, req.DeviceID, req.TargetDeviceID); err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&Row{}).Where("tenant_id = ? AND state IN ?", req.TenantID, []string{StateAccepted, StateDispatching, StateBrokerAccepted}).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= maxPending {
			return ErrCapacity
		}
		id := uuid.NewString()
		result = Row{
			ID: id, TenantID: req.TenantID, CallerID: req.CallerID, Intent: req.Intent, IdempotencyKey: req.IdempotencyKey,
			DeviceID: req.DeviceID, TargetDeviceID: req.TargetDeviceID, RouteCell: binding.BrokerCell, RouteEpoch: binding.RouteEpoch,
			CanonicalRequest: append([]byte(nil), req.CanonicalRequest...), CanonicalSHA256: append([]byte(nil), digest[:]...),
			Payload: append([]byte(nil), req.Payload...), ExpiresAt: req.ExpiresAt.UTC(), State: StateAccepted,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		return persist(tx, result)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return s.resolveDuplicate(ctx, req, digest)
		}
		return Row{}, false, err
	}
	return result, duplicate, nil
}

func validateRequest(req Request) error {
	for _, field := range []string{req.TenantID, req.CallerID, req.Intent, req.DeviceID} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: required identity is empty", ErrInvalid)
		}
	}
	if req.TargetDeviceID == "" {
		req.TargetDeviceID = req.DeviceID
	}
	if len(req.TenantID) > 36 || len(req.CallerID) > 128 || len(req.Intent) > 128 || len(req.IdempotencyKey) > 256 || len(req.DeviceID) > 36 || len(req.TargetDeviceID) > 36 {
		return fmt.Errorf("%w: identity field exceeds database limit", ErrInvalid)
	}
	if len(req.CanonicalRequest) == 0 || len(req.Payload) == 0 || len(req.CanonicalRequest) > maxField || len(req.Payload) > maxField {
		return fmt.Errorf("%w: canonical request and payload must be between 1 byte and 1 MiB", ErrInvalid)
	}
	return nil
}

func validateExpiry(expiresAt, now time.Time) error {
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxTTL)) {
		return fmt.Errorf("%w: expiry must be in the future and no more than 24 hours away", ErrInvalid)
	}
	return nil
}

func (s *Store) resolveDuplicate(ctx context.Context, req Request, digest [32]byte) (Row, bool, error) {
	var row Row
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND caller_id = ? AND intent = ? AND idempotency_key = ?", req.TenantID, req.CallerID, req.Intent, req.IdempotencyKey).Take(&row).Error
	if err != nil {
		return Row{}, false, err
	}
	if !equalDigest(row.CanonicalSHA256, digest[:]) {
		return Row{}, false, ErrConflict
	}
	return row, true, nil
}

// Claim leases one accepted command after revalidating its route and caller guard.
func (s *Store) Claim(ctx context.Context, guard func(context.Context, *gorm.DB, Row) error) (*Row, error) {
	return s.claim(ctx, "", guard)
}

// ClaimInCell prevents a dispatcher in another cell from consuming or failing
// this cell's commands during a gray deployment or rollback.
func (s *Store) ClaimInCell(ctx context.Context, cell string, guard func(context.Context, *gorm.DB, Row) error) (*Row, error) {
	if strings.TrimSpace(cell) == "" {
		return nil, ErrInvalid
	}
	return s.claim(ctx, cell, guard)
}
func (s *Store) claim(ctx context.Context, cell string, guard func(context.Context, *gorm.DB, Row) error) (*Row, error) {
	if s == nil || s.db == nil || ctx == nil || guard == nil {
		return nil, ErrInvalid
	}
	var claimed *Row
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate Row
		candidateQuery := tx.Select("id", "tenant_id", "target_device_id", "route_cell", "route_epoch").Where("state = ?", StateAccepted)
		if cell != "" {
			candidateQuery = candidateQuery.Where("route_cell=?", cell)
		}
		query := candidateQuery.Order("created_at ASC, id ASC").Take(&candidate)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		binding, bindingErr := activeBinding(tx, candidate.TenantID, candidate.TargetDeviceID)
		var row Row
		claimQuery := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("id = ? AND state = ?", candidate.ID, StateAccepted).Take(&row)
		if errors.Is(claimQuery.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if claimQuery.Error != nil {
			return claimQuery.Error
		}
		now := time.Now().UTC()
		if !row.ExpiresAt.After(now) {
			return tx.Model(&Row{}).Where("id = ? AND state = ?", row.ID, StateAccepted).Updates(map[string]any{"state": StateExpired, "completed_at": now, "updated_at": now}).Error
		}
		if bindingErr != nil && !errors.Is(bindingErr, ErrFenced) {
			return bindingErr
		}
		if row.TenantID != candidate.TenantID || row.TargetDeviceID != candidate.TargetDeviceID || errors.Is(bindingErr, ErrFenced) || binding.BrokerCell != row.RouteCell || binding.RouteEpoch != row.RouteEpoch {
			return tx.Model(&Row{}).Where("id = ? AND state = ?", row.ID, StateAccepted).Updates(map[string]any{"state": StateFenced, "last_error": ErrFenced.Error(), "completed_at": now, "updated_at": now}).Error
		}
		if err := checkDevicePair(tx, row.TenantID, row.DeviceID, row.TargetDeviceID); err != nil {
			if !errors.Is(err, ErrRejected) {
				return err
			}
			return tx.Model(&Row{}).Where("id = ? AND state = ?", row.ID, StateAccepted).Updates(map[string]any{"state": StateFenced, "last_error": err.Error(), "completed_at": now, "updated_at": now}).Error
		}
		if err := guard(ctx, tx, row); err != nil {
			if errors.Is(err, ErrFenced) {
				return tx.Model(&Row{}).Where("id = ? AND state = ?", row.ID, StateAccepted).Updates(map[string]any{"state": StateFenced, "last_error": "command configuration changed after acceptance", "completed_at": now, "updated_at": now}).Error
			}
			if !errors.Is(err, ErrRejected) {
				return err
			}
			return tx.Model(&Row{}).Where("id = ? AND state = ?", row.ID, StateAccepted).Updates(map[string]any{"state": StateFailed, "last_error": err.Error(), "completed_at": now, "updated_at": now}).Error
		}
		if err := tx.Model(&Row{}).Where("id = ? AND state = ?", row.ID, StateAccepted).Updates(map[string]any{"state": StateDispatching, "dispatched_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		row.State, row.DispatchedAt, row.UpdatedAt = StateDispatching, &now, now
		claimed = &row
		return nil
	})
	return claimed, err
}

// FinishPublish records a definite broker acceptance or an ambiguous outcome.
func (s *Store) FinishPublish(ctx context.Context, id string, success bool, reason string) error {
	if s == nil || s.db == nil || ctx == nil || id == "" {
		return ErrInvalid
	}
	state := StateUnknown
	if success {
		state = StateBrokerAccepted
	}
	updates := map[string]any{"state": state, "last_error": reason, "updated_at": time.Now().UTC()}
	result := s.db.WithContext(ctx).Model(&Row{}).Where("id = ? AND state = ?", id, StateDispatching).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current Row
		if err := s.db.WithContext(ctx).Select("id").Where("id = ?", id).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		return ErrState
	}
	return nil
}

// Acknowledge updates only the receipt bound to the authenticated transport device.
func (s *Store) Acknowledge(ctx context.Context, tx *gorm.DB, tenant, transportDevice, commandID string, success bool) error {
	if s == nil || tx == nil || ctx == nil || tenant == "" || transportDevice == "" || commandID == "" {
		return ErrInvalid
	}
	targetState := StateDeviceReportedFailure
	if success {
		targetState = StateDeviceReportedSuccess
	}
	result := tx.WithContext(ctx).Model(&Row{}).
		Where("id = ? AND tenant_id = ? AND target_device_id = ? AND state IN ?", commandID, tenant, transportDevice, []string{StateDispatching, StateBrokerAccepted, StateUnknown}).
		Updates(map[string]any{"state": targetState, "completed_at": time.Now().UTC(), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var row Row
		err := tx.WithContext(ctx).Select("id").Where("id = ? AND tenant_id = ? AND target_device_id = ?", commandID, tenant, transportDevice).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return ErrState
	}
	return nil
}

// MarkStaleDispatches converts expired accepted intents and old ambiguous
// dispatches to terminal states; dispatching rows are never requeued.
func (s *Store) MarkStaleDispatches(ctx context.Context, age time.Duration) (int64, error) {
	if s == nil || s.db == nil || ctx == nil || age <= 0 {
		return 0, ErrInvalid
	}
	now := time.Now().UTC()
	var total int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		expired := tx.Model(&Row{}).Where("state = ? AND expires_at <= ?", StateAccepted, now).Updates(map[string]any{"state": StateExpired, "completed_at": now, "updated_at": now})
		if expired.Error != nil {
			return expired.Error
		}
		stale := tx.Model(&Row{}).Where("state = ? AND updated_at <= ?", StateDispatching, now.Add(-age)).Updates(map[string]any{"state": StateUnknown, "last_error": "dispatch lease expired; publish outcome unknown", "updated_at": now})
		if stale.Error != nil {
			return stale.Error
		}
		unreported := tx.Model(&Row{}).Where("state = ? AND expires_at <= ?", StateBrokerAccepted, now).Updates(map[string]any{"state": StateUnknown, "last_error": "broker acknowledged publish; no device report before expiry, execution unknown", "updated_at": now})
		if unreported.Error != nil {
			return unreported.Error
		}
		total = expired.RowsAffected + stale.RowsAffected + unreported.RowsAffected
		return nil
	}); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) GetScoped(ctx context.Context, tenant, id string) (Row, error) {
	var row Row
	if s == nil || s.db == nil || ctx == nil || tenant == "" || id == "" {
		return row, ErrInvalid
	}
	// Durable IDs are UUIDs. Legacy operation IDs can be short strings; avoid
	// an invalid UUID SQL error so callers can use their tenant/device-scoped
	// legacy receipt fallback without masking real database failures.
	if _, err := uuid.Parse(id); err != nil {
		return Row{}, ErrNotFound
	}
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenant, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Row{}, ErrNotFound
	}
	return row, err
}

func lockTenant(tx *gorm.DB, tenant string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "brokercommands:"+tenant).Error
}

func checkDevicePair(tx *gorm.DB, tenant, logicalDeviceID, targetRootID string) error {
	currentID := logicalDeviceID
	visited := make(map[string]struct{}, 8)
	for depth := 0; depth < 8; depth++ {
		if _, exists := visited[currentID]; exists {
			return fmt.Errorf("%w: device parent cycle", ErrRejected)
		}
		visited[currentID] = struct{}{}
		var current deviceRow
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ?", currentID, tenant).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: logical device or ancestor unavailable in tenant", ErrRejected)
			}
			return err
		}
		if current.IsEnabled != "enabled" {
			return fmt.Errorf("%w: device or ancestor disabled", ErrRejected)
		}
		if currentID == targetRootID {
			if current.ParentID != nil && *current.ParentID != "" {
				return fmt.Errorf("%w: target device is not a root", ErrRejected)
			}
			return nil
		}
		if current.ParentID == nil || *current.ParentID == "" {
			return fmt.Errorf("%w: target is not an ancestor of logical device", ErrRejected)
		}
		currentID = *current.ParentID
	}
	return fmt.Errorf("%w: device ancestry exceeds eight levels", ErrRejected)
}

func activeBinding(tx *gorm.DB, tenant, device string) (bindingRow, error) {
	var binding bindingRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND device_id = ?", tenant, device).Take(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return bindingRow{}, ErrFenced
	}
	if err != nil {
		return bindingRow{}, err
	}
	if binding.Phase != "active" || binding.BrokerCell == "" || binding.RouteEpoch <= 0 {
		return bindingRow{}, ErrFenced
	}
	return binding, nil
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "broker_command_requests_idempotency_uq")
}

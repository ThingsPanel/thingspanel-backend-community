package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"project/internal/brokerstate"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type expectedContext struct {
	ExpectedID       string    `gorm:"column:expected_id"`
	TenantID         string    `gorm:"column:tenant_id"`
	DeviceID         string    `gorm:"column:device_id"`
	CallerID         string    `gorm:"column:caller_id"`
	SendType         string    `gorm:"column:send_type"`
	PayloadSHA256    string    `gorm:"column:payload_sha256"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	DispatchAttempts int       `gorm:"column:dispatch_attempts"`
	NextAttemptAt    time.Time `gorm:"column:next_attempt_at"`
}

var errExpectedContextUnavailable = errors.New("expected message is unavailable in ingress_v1")

func expectedPayloadHash(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// ExpectedDataIngressSchemaReady checks only when the core feature table is
// installed. A partial broker sidecar schema is a startup error, not a retry.
func ExpectedDataIngressSchemaReady(db *gorm.DB) error {
	if db == nil {
		return errors.New("expected data database is unavailable")
	}
	if !db.Migrator().HasTable("expected_datas") {
		return nil
	}
	if !db.Migrator().HasTable("broker_expected_context") {
		return errors.New("broker expected context schema is not installed")
	}
	for _, column := range []string{"expected_id", "tenant_id", "device_id", "caller_id", "send_type", "payload_sha256", "created_at", "dispatch_attempts", "next_attempt_at"} {
		if !db.Migrator().HasColumn("broker_expected_context", column) {
			return fmt.Errorf("broker expected context schema is incomplete: missing %s", column)
		}
	}
	for _, column := range []string{"id", "device_id", "tenant_id", "send_type", "payload", "expiry_time", "status", "send_time", "message"} {
		if !db.Migrator().HasColumn("expected_datas", column) {
			return fmt.Errorf("expected data schema is incomplete: missing expected_datas.%s", column)
		}
	}
	return nil
}

func validateExpectedCreatorTx(ctx context.Context, tx *gorm.DB, claims *utils.UserClaims, deviceID string) error {
	if claims == nil || strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(deviceID) == "" {
		return errors.New("expected message caller context is incomplete")
	}
	if err := validateExpectedCreator(ctx, tx, claims.TenantID, claims.ID); err != nil {
		return err
	}
	var device model.Device
	if err := tx.Where("id = ? AND tenant_id = ? AND is_enabled = ? AND activate_flag = ?", deviceID, claims.TenantID, "enabled", "active").Take(&device).Error; err != nil {
		return errors.New("expected message device is unavailable")
	}
	return nil
}

func (e *ExpectedData) createIngress(ctx context.Context, ed *model.ExpectedData, claims *utils.UserClaims) error {
	if ctx == nil || global.DB == nil {
		return errors.New("expected message database is unavailable")
	}
	if !global.DB.Migrator().HasTable("expected_datas") {
		return errors.New("expected data core schema is not installed")
	}
	if err := ExpectedDataIngressSchemaReady(global.DB.WithContext(ctx)); err != nil {
		return err
	}
	return global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateExpectedCreatorTx(ctx, tx, claims, ed.DeviceID); err != nil {
			return err
		}
		if err := tx.Create(ed).Error; err != nil {
			return err
		}
		entry := expectedContext{
			ExpectedID: ed.ID, TenantID: ed.TenantID, DeviceID: ed.DeviceID,
			CallerID: claims.ID, SendType: ed.SendType,
			PayloadSHA256: expectedPayloadHash(ed.Payload), CreatedAt: ed.CreatedAt,
		}
		return tx.Table("broker_expected_context").Create(&entry).Error
	})
}

func getExpectedDataGlobal(ctx context.Context, id string) (*model.ExpectedData, error) {
	if global.DB == nil {
		return nil, errors.New("expected message database is unavailable")
	}
	var ed model.ExpectedData
	if err := global.DB.WithContext(ctx).Where("id=?", id).Take(&ed).Error; err != nil {
		return nil, err
	}
	return &ed, nil
}

func getExpectedContext(ctx context.Context, db *gorm.DB, ed *model.ExpectedData) (expectedContext, error) {
	var owner expectedContext
	if ed == nil || db == nil {
		return owner, errExpectedContextUnavailable
	}
	if err := db.WithContext(ctx).Table("broker_expected_context").Where("expected_id = ?", ed.ID).Take(&owner).Error; err != nil {
		return owner, errExpectedContextUnavailable
	}
	if owner.TenantID == "" || owner.DeviceID == "" || owner.CallerID == "" || owner.TenantID != ed.TenantID || owner.DeviceID != ed.DeviceID || owner.SendType != ed.SendType || owner.PayloadSHA256 != expectedPayloadHash(ed.Payload) {
		return owner, errExpectedContextUnavailable
	}
	return owner, nil
}

func dispatchExpectedIngress(ctx context.Context, ed *model.ExpectedData, owner expectedContext, requiredCell string) (bool, error) {
	if ed == nil || global.DB == nil || ed.Status != "pending" {
		return false, nil
	}
	if ed.ExpiryTime != nil && !ed.ExpiryTime.After(time.Now()) {
		result := global.DB.WithContext(ctx).Model(&model.ExpectedData{}).Where("id = ? AND tenant_id = ? AND status = ?", ed.ID, ed.TenantID, "pending").Updates(map[string]interface{}{"status": "expired", "message": nil})
		return result.RowsAffected > 0, result.Error
	}
	evidence, err := brokerstate.New(global.DB).InspectCurrent(ctx, owner.TenantID, owner.DeviceID)
	if err != nil {
		return false, err
	}
	if !evidence.Fresh || evidence.Status != "online" || evidence.BrokerCell == "" || (requiredCell != "" && evidence.BrokerCell != requiredCell) {
		return false, nil
	}
	_, err = submitExpectedDownlink(ctx, ed, owner.CallerID)
	if err != nil {
		return false, err
	}
	// submitExpectedDownlink atomically records durable admission and the
	// expected-data status in its broker command transaction.
	return true, nil
}

// DispatchPendingExpected is called by the broker-cell worker. Only pending
// records with an intact creator sidecar and fresh online evidence for this
// exact cell are handed to the durable command owner.
func DispatchPendingExpected(ctx context.Context, cell string) error {
	if ctx == nil || strings.TrimSpace(cell) == "" || global.DB == nil {
		return errors.New("expected message dispatcher is not configured")
	}
	if !global.DB.Migrator().HasTable("expected_datas") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ExpectedDataIngressSchemaReady(global.DB.WithContext(ctx)); err != nil {
		return err
	}
	var rows []model.ExpectedData
	err := global.DB.WithContext(ctx).Raw(`SELECT candidate.* FROM (
		SELECT e.*, 0 AS priority, x.dispatch_attempts, x.next_attempt_at
		FROM expected_datas e JOIN broker_expected_context x ON x.expected_id=e.id AND x.tenant_id=e.tenant_id AND x.device_id=e.device_id AND x.send_type=e.send_type
		WHERE e.status='pending' AND e.expiry_time IS NOT NULL AND e.expiry_time<=now()
		UNION ALL
		SELECT e.*, 1 AS priority, x.dispatch_attempts, x.next_attempt_at
		FROM expected_datas e JOIN broker_expected_context x ON x.expected_id=e.id AND x.tenant_id=e.tenant_id AND x.device_id=e.device_id AND x.send_type=e.send_type
		LEFT JOIN broker_telemetry_activity a ON a.tenant_id=e.tenant_id AND a.device_id=e.device_id
		JOIN broker_transport_bindings b ON b.tenant_id=e.tenant_id AND b.device_id=COALESCE(a.transport_device_id,e.device_id)
		WHERE e.status='pending' AND (e.expiry_time IS NULL OR e.expiry_time>now()) AND x.next_attempt_at<=now() AND b.phase='active' AND b.broker_cell=?
	) AS candidate ORDER BY candidate.priority,candidate.dispatch_attempts,candidate.next_attempt_at,candidate.created_at,candidate.id LIMIT 32`, cell).Scan(&rows).Error
	if err != nil {
		return err
	}
	var unsupported int64
	if err := global.DB.WithContext(ctx).Raw(`SELECT 1 FROM expected_datas e WHERE e.status='pending' AND NOT EXISTS (SELECT 1 FROM broker_expected_context x WHERE x.expected_id=e.id AND x.tenant_id=e.tenant_id AND x.device_id=e.device_id AND x.send_type=e.send_type) LIMIT 1`).Scan(&unsupported).Error; err != nil {
		return err
	}
	var firstErr error
	if unsupported != 0 {
		firstErr = errExpectedContextUnavailable
	}
	for i := range rows {
		ed := &rows[i]
		owner, err := getExpectedContext(ctx, global.DB, ed)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := scheduleExpectedAttempt(ctx, ed.ID, ed.TenantID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_, err = dispatchExpectedIngress(ctx, ed, owner, cell)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func sendExpectedIngress(ctx context.Context, deviceID string) error {
	if ctx == nil || global.DB == nil {
		return errors.New("expected message database is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ExpectedDataIngressSchemaReady(global.DB.WithContext(ctx)); err != nil {
		return err
	}
	var rows []model.ExpectedData
	err := global.DB.WithContext(ctx).Raw(`SELECT e.* FROM expected_datas e JOIN broker_expected_context x ON x.expected_id=e.id AND x.tenant_id=e.tenant_id AND x.device_id=e.device_id AND x.send_type=e.send_type WHERE e.device_id=? AND e.status='pending' AND (x.next_attempt_at<=now() OR (e.expiry_time IS NOT NULL AND e.expiry_time<=now())) ORDER BY (e.expiry_time IS NOT NULL AND e.expiry_time<=now()) DESC,x.dispatch_attempts,x.next_attempt_at,e.created_at,e.id LIMIT 32`, deviceID).Scan(&rows).Error
	if err != nil {
		return err
	}
	var unsupported int64
	if err := global.DB.WithContext(ctx).Raw(`SELECT 1 FROM expected_datas e WHERE e.device_id=? AND e.status='pending' AND NOT EXISTS (SELECT 1 FROM broker_expected_context x WHERE x.expected_id=e.id AND x.tenant_id=e.tenant_id AND x.device_id=e.device_id AND x.send_type=e.send_type) LIMIT 1`, deviceID).Scan(&unsupported).Error; err != nil {
		return err
	}
	var firstErr error
	if unsupported != 0 {
		firstErr = errExpectedContextUnavailable
	}
	for i := range rows {
		ed := &rows[i]
		owner, err := getExpectedContext(ctx, global.DB, ed)
		if err == nil {
			err = scheduleExpectedAttempt(ctx, ed.ID, ed.TenantID)
		}
		if err == nil {
			_, err = dispatchExpectedIngress(ctx, ed, owner, "")
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func scheduleExpectedAttempt(ctx context.Context, id, tenant string) error {
	result := global.DB.WithContext(ctx).Table("broker_expected_context").Where("expected_id=? AND tenant_id=?", id, tenant).Updates(map[string]interface{}{
		"dispatch_attempts": gorm.Expr("dispatch_attempts + 1"),
		"next_attempt_at":   gorm.Expr("now() + interval '5 seconds'"),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errExpectedContextUnavailable
	}
	return nil
}

func deleteExpectedScoped(ctx context.Context, id string, claims *utils.UserClaims) error {
	if global.DB == nil || claims == nil || claims.ID == "" || claims.TenantID == "" || id == "" {
		return errors.New("expected message delete scope is incomplete")
	}
	return global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ExpectedData
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", id, claims.TenantID).Take(&row).Error; err != nil {
			return errors.New("expected message not found")
		}
		if row.Status != "pending" {
			return errors.New("only pending expected messages can be cancelled")
		}
		result := tx.Where("id=? AND tenant_id=? AND status='pending'", id, claims.TenantID).Delete(&model.ExpectedData{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("expected message changed before cancellation")
		}
		if tx.Migrator().HasTable("broker_expected_context") {
			return tx.Table("broker_expected_context").Where("expected_id=? AND tenant_id=?", id, claims.TenantID).Delete(&expectedContext{}).Error
		}
		return nil
	})
}

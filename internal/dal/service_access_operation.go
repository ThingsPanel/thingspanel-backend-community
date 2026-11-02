package dal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/model"
	"project/internal/query"
)

var ErrServiceAccessOperationConflict = errors.New("service access operation conflicts with the original request")
var ErrServiceAccessOperationUnknown = errors.New("service access transaction outcome is unknown")
var ErrServiceAccessResourceChanged = errors.New("service access creation is recorded but its resource has changed")

// ServiceAccessCreateReceipt belongs to the existing service-access owner.
// It survives resource deletion and must never be reused for another creation.
type ServiceAccessCreateReceipt struct {
	TenantID     string    `gorm:"column:tenant_id;primaryKey"`
	OperationKey string    `gorm:"column:operation_key;primaryKey"`
	SubjectID    string    `gorm:"column:subject_id;not null"`
	RequestHash  string    `gorm:"column:request_hash;not null"`
	AccessID     string    `gorm:"column:access_id;not null"`
	ResourceHash string    `gorm:"column:resource_hash;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

func (ServiceAccessCreateReceipt) TableName() string { return "service_access_create_receipts" }

func serviceAccessResourceHash(access model.ServiceAccess) (string, error) {
	// PostgreSQL timestamps are stored to microsecond precision. Normalize only
	// the timestamps, retaining exact nullable fields and configuration bytes.
	raw, err := json.Marshal(struct {
		Version int
		Access  model.ServiceAccess
	}{1, model.ServiceAccess{
		ID: access.ID, Name: access.Name, ServicePluginID: access.ServicePluginID,
		Voucher: access.Voucher, Description: access.Description,
		ServiceAccessConfig: access.ServiceAccessConfig, Remark: access.Remark,
		TenantID: access.TenantID,
		CreateAt: access.CreateAt.UTC().Truncate(time.Microsecond),
		UpdateAt: access.UpdateAt.UTC().Truncate(time.Microsecond),
	}})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func rollbackServiceAccessOperation(tx *gorm.DB, cause error) error {
	if err := tx.Rollback().Error; err != nil {
		return ErrServiceAccessOperationUnknown
	}
	return cause
}

func observeServiceAccessReceipt(tx *gorm.DB, receipt ServiceAccessCreateReceipt) (*model.ServiceAccessCreateObservation, error) {
	createdAt := receipt.CreatedAt
	observation := &model.ServiceAccessCreateObservation{
		ProtocolVersion: "service-access-create-v1", State: "confirmed",
		OperationKey: receipt.OperationKey, RequestHash: receipt.RequestHash,
		ID: receipt.AccessID, CreatedAt: &createdAt,
	}
	var access model.ServiceAccess
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ?", receipt.AccessID, receipt.TenantID).Take(&access).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		observation.State, observation.Reason = "requires_reconciliation", "RESOURCE_MISSING"
		return observation, nil
	}
	if err != nil {
		return nil, err
	}
	hash, err := serviceAccessResourceHash(access)
	if err != nil {
		return nil, err
	}
	if hash != receipt.ResourceHash {
		observation.State, observation.Reason = "requires_reconciliation", "RESOURCE_CHANGED"
	}
	return observation, nil
}

// CreateServiceAccessWithOperation serializes the original key before creating
// a resource. All writes are one local transaction; no remote side effects run
// inside it. Commit/rollback errors are never reported as confirmed success.
func CreateServiceAccessWithOperation(ctx context.Context, access *model.ServiceAccess, subjectID, key, requestHash string) (*model.ServiceAccessCreateObservation, error) {
	if access == nil || access.ID == "" || access.TenantID == "" || subjectID == "" || key == "" || len(requestHash) != 64 {
		return nil, fmt.Errorf("invalid service access operation")
	}
	db := query.ServiceAccess.UnderlyingDB().Session(&gorm.Session{NewDB: true}).WithContext(ctx)
	if db.Dialector.Name() != "postgres" {
		return nil, fmt.Errorf("durable service access creation is not supported by this database profile")
	}
	tx := db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	resourceHash, err := serviceAccessResourceHash(*access)
	if err != nil {
		return nil, rollbackServiceAccessOperation(tx, err)
	}
	receipt := ServiceAccessCreateReceipt{
		TenantID: access.TenantID, OperationKey: key, SubjectID: subjectID,
		RequestHash: requestHash, AccessID: access.ID, ResourceHash: resourceHash,
		CreatedAt: access.CreateAt,
	}
	insert := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "operation_key"}}, DoNothing: true}).Create(&receipt)
	if insert.Error != nil {
		return nil, rollbackServiceAccessOperation(tx, insert.Error)
	}
	replayed := insert.RowsAffected == 0
	if replayed {
		if err := tx.Where("tenant_id = ? AND operation_key = ?", access.TenantID, key).Take(&receipt).Error; err != nil {
			return nil, rollbackServiceAccessOperation(tx, err)
		}
		if receipt.SubjectID != subjectID || receipt.RequestHash != requestHash {
			return nil, rollbackServiceAccessOperation(tx, ErrServiceAccessOperationConflict)
		}
	} else {
		if insert.RowsAffected != 1 {
			return nil, rollbackServiceAccessOperation(tx, fmt.Errorf("service access receipt was not inserted exactly once"))
		}
		insertAccess := tx.Create(access)
		if insertAccess.Error != nil {
			return nil, rollbackServiceAccessOperation(tx, insertAccess.Error)
		}
		if insertAccess.RowsAffected != 1 {
			return nil, rollbackServiceAccessOperation(tx, fmt.Errorf("service access resource was not inserted exactly once"))
		}
	}
	observation, err := observeServiceAccessReceipt(tx, receipt)
	if err != nil {
		return nil, rollbackServiceAccessOperation(tx, err)
	}
	if observation.State != "confirmed" {
		return nil, rollbackServiceAccessOperation(tx, ErrServiceAccessResourceChanged)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, ErrServiceAccessOperationUnknown
	}
	observation.Replayed = replayed
	return observation, nil
}

// ReadServiceAccessCreateOperation never turns a missing/changed target into a
// new creation. not_observed means no committed receipt was visible; an older
// request may still be in flight, so only the original keyed request is safe.
func ReadServiceAccessCreateOperation(ctx context.Context, tenantID, subjectID, key string) (*model.ServiceAccessCreateObservation, error) {
	db := query.ServiceAccess.UnderlyingDB().Session(&gorm.Session{NewDB: true}).WithContext(ctx)
	if db.Dialector.Name() != "postgres" {
		return nil, fmt.Errorf("durable service access query is not supported by this database profile")
	}
	tx := db.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	var receipt ServiceAccessCreateReceipt
	err := tx.Where("tenant_id = ? AND operation_key = ?", tenantID, key).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Commit().Error; err != nil {
			return nil, ErrServiceAccessOperationUnknown
		}
		return &model.ServiceAccessCreateObservation{ProtocolVersion: "service-access-create-v1", State: "not_observed", OperationKey: key}, nil
	}
	if err != nil {
		return nil, rollbackServiceAccessOperation(tx, err)
	}
	if receipt.SubjectID != subjectID {
		return nil, rollbackServiceAccessOperation(tx, gorm.ErrRecordNotFound)
	}
	observation, err := observeServiceAccessReceipt(tx, receipt)
	if err != nil {
		return nil, rollbackServiceAccessOperation(tx, err)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, ErrServiceAccessOperationUnknown
	}
	return observation, nil
}

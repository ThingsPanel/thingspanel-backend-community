package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"github.com/go-basic/uuid"
)

// MarketBundleInstallRepo handles database operations for market bundle installations
type MarketBundleInstallRepo struct{}

func NewMarketBundleInstallRepo() *MarketBundleInstallRepo {
	return &MarketBundleInstallRepo{}
}

// CreateInstallation creates a new installation record
func (r *MarketBundleInstallRepo) CreateInstallation(ctx context.Context, inst *model.MarketBundleInstallation) (*model.MarketBundleInstallation, error) {
	inst.ID = uuid.New()
	if err := global.DB.WithContext(ctx).Create(inst).Error; err != nil {
		return nil, fmt.Errorf("failed to create installation: %w", err)
	}
	return inst, nil
}

// GetByIdempotencyKey retrieves an installation by idempotency key and tenant
func (r *MarketBundleInstallRepo) GetByIdempotencyKey(ctx context.Context, idempotencyKey, tenantID string) (*model.MarketBundleInstallation, error) {
	var inst model.MarketBundleInstallation
	err := global.DB.WithContext(ctx).
		Where("idempotency_key = ? AND tenant_id = ?", idempotencyKey, tenantID).
		First(&inst).Error
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

// GetByID retrieves an installation by ID
func (r *MarketBundleInstallRepo) GetByID(ctx context.Context, id string) (*model.MarketBundleInstallation, error) {
	var inst model.MarketBundleInstallation
	err := global.DB.WithContext(ctx).Where("id = ?", id).First(&inst).Error
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

// GetByTenantAndBundle retrieves an installation by tenant, bundle key, and version
func (r *MarketBundleInstallRepo) GetByTenantAndBundle(ctx context.Context, tenantID, bundleKey, version string) (*model.MarketBundleInstallation, error) {
	var inst model.MarketBundleInstallation
	err := global.DB.WithContext(ctx).
		Where("tenant_id = ? AND bundle_key = ? AND bundle_version = ?", tenantID, bundleKey, version).
		Order("created_at DESC").
		First(&inst).Error
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

// ListByTenant retrieves installations for a tenant with pagination
func (r *MarketBundleInstallRepo) ListByTenant(ctx context.Context, tenantID string, q *model.ListInstallationsRequest) ([]*model.MarketBundleInstallation, int64, error) {
	db := global.DB.WithContext(ctx).Model(&model.MarketBundleInstallation{}).
		Where("tenant_id = ?", tenantID)

	if q.BundleKey != "" {
		db = db.Where("bundle_key = ?", q.BundleKey)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}

	var installations []*model.MarketBundleInstallation
	offset := (q.Page - 1) * q.PageSize
	if err := db.Order("created_at DESC").Limit(q.PageSize).Offset(offset).Find(&installations).Error; err != nil {
		return nil, 0, err
	}
	return installations, total, nil
}

// UpdateStatus updates installation status and timestamps
func (r *MarketBundleInstallRepo) UpdateStatus(ctx context.Context, id, status, errorCode, errorMessage string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": now,
	}

	switch status {
	case model.InstallStateDownloaded:
		updates["downloaded_at"] = now
	case model.InstallStateVerified:
		updates["verified_at"] = now
	case model.InstallStateModelsInstalled:
		updates["models_installed_at"] = now
	case model.InstallStateDashboardsCreated:
		updates["dashboards_created_at"] = now
	case model.InstallStateWaitingForBindings, model.InstallStateCompleted:
		updates["completed_at"] = now
	case model.InstallStateFailed, model.InstallStateCompensationRequired:
		updates["error_code"] = errorCode
		updates["error_message"] = errorMessage
		updates["completed_at"] = now
	}

	return global.DB.WithContext(ctx).
		Model(&model.MarketBundleInstallation{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// UpdateWarnings updates installation warnings
func (r *MarketBundleInstallRepo) UpdateWarnings(ctx context.Context, id string, warnings []string) error {
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	return global.DB.WithContext(ctx).
		Model(&model.MarketBundleInstallation{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"warnings":   warningsJSON,
			"updated_at": time.Now(),
		}).Error
}

// --- Resource Mappings ---

// CreateResourceMapping creates a new resource mapping
func (r *MarketBundleInstallRepo) CreateResourceMapping(ctx context.Context, mapping *model.MarketResourceMapping) (*model.MarketResourceMapping, error) {
	mapping.ID = uuid.New()
	if err := global.DB.WithContext(ctx).Create(mapping).Error; err != nil {
		return nil, fmt.Errorf("failed to create resource mapping: %w", err)
	}
	return mapping, nil
}

// GetResourceMappingsByInstallation retrieves all mappings for an installation
func (r *MarketBundleInstallRepo) GetResourceMappingsByInstallation(ctx context.Context, installationID string) ([]*model.MarketResourceMapping, error) {
	var mappings []*model.MarketResourceMapping
	err := global.DB.WithContext(ctx).
		Where("installation_id = ?", installationID).
		Order("resource_type, created_at").
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

// GetResourceMappingByLocalID retrieves mapping by local ID and type
func (r *MarketBundleInstallRepo) GetResourceMappingByLocalID(ctx context.Context, tenantID, localID, resourceType string) (*model.MarketResourceMapping, error) {
	var mapping model.MarketResourceMapping
	err := global.DB.WithContext(ctx).
		Where("tenant_id = ? AND local_id = ? AND resource_type = ? AND status = 'active'", tenantID, localID, resourceType).
		First(&mapping).Error
	if err != nil {
		return nil, err
	}
	return &mapping, nil
}

// UpdateResourceMappingStatus updates a resource mapping status
func (r *MarketBundleInstallRepo) UpdateResourceMappingStatus(ctx context.Context, id, status string) error {
	return global.DB.WithContext(ctx).
		Model(&model.MarketResourceMapping{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

// --- Binding Status ---

// CreateBindingStatus creates a new binding status record
func (r *MarketBundleInstallRepo) CreateBindingStatus(ctx context.Context, binding *model.MarketBundleBindingStatus) (*model.MarketBundleBindingStatus, error) {
	binding.ID = uuid.New()
	if err := global.DB.WithContext(ctx).Create(binding).Error; err != nil {
		return nil, fmt.Errorf("failed to create binding status: %w", err)
	}
	return binding, nil
}

// GetBindingStatusesByInstallation retrieves all binding statuses for an installation
func (r *MarketBundleInstallRepo) GetBindingStatusesByInstallation(ctx context.Context, installationID string) ([]*model.MarketBundleBindingStatus, error) {
	var bindings []*model.MarketBundleBindingStatus
	err := global.DB.WithContext(ctx).
		Where("installation_id = ?", installationID).
		Order("binding_key").
		Find(&bindings).Error
	if err != nil {
		return nil, err
	}
	return bindings, nil
}

// UpdateBindingDevice updates the bound device for a binding
func (r *MarketBundleInstallRepo) UpdateBindingDevice(ctx context.Context, id, localDeviceID, status, errorMessage string) error {
	now := time.Now()
	return global.DB.WithContext(ctx).
		Model(&model.MarketBundleBindingStatus{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"local_device_id": localDeviceID,
			"bound_at":        now,
			"status":          status,
			"error_message":   errorMessage,
			"updated_at":      now,
		}).Error
}

// GetBindingByKey retrieves binding by installation ID and binding key
func (r *MarketBundleInstallRepo) GetBindingByKey(ctx context.Context, installationID, bindingKey string) (*model.MarketBundleBindingStatus, error) {
	var binding model.MarketBundleBindingStatus
	err := global.DB.WithContext(ctx).
		Where("installation_id = ? AND binding_key = ?", installationID, bindingKey).
		First(&binding).Error
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// --- Audit Trail ---

// CreateAuditEntry creates a new audit entry
func (r *MarketBundleInstallRepo) CreateAuditEntry(ctx context.Context, audit *model.MarketInstallationAudit) (*model.MarketInstallationAudit, error) {
	audit.ID = uuid.New()
	if err := global.DB.WithContext(ctx).Create(audit).Error; err != nil {
		return nil, fmt.Errorf("failed to create audit entry: %w", err)
	}
	return audit, nil
}

// GetAuditTrail retrieves audit entries for an installation
func (r *MarketBundleInstallRepo) GetAuditTrail(ctx context.Context, installationID string) ([]*model.MarketInstallationAudit, error) {
	var entries []*model.MarketInstallationAudit
	err := global.DB.WithContext(ctx).
		Where("installation_id = ?", installationID).
		Order("created_at DESC").
		Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

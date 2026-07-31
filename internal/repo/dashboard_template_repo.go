package repo

import (
	"context"
	"fmt"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"github.com/go-basic/uuid"
	"gorm.io/gorm"
)

type DashboardTemplateRepo struct{}

func NewDashboardTemplateRepo() *DashboardTemplateRepo {
	return &DashboardTemplateRepo{}
}

func (r *DashboardTemplateRepo) FindMarketTemplate(
	ctx context.Context,
	tenantID, bundleKey, bundleVersion, resourceKey string,
) (*model.LocalDashboardTemplate, error) {
	var template model.LocalDashboardTemplate
	err := global.DB.WithContext(ctx).
		Where(
			"tenant_id = ? AND bundle_key = ? AND bundle_version = ? AND dashboard_resource_key = ?",
			tenantID,
			bundleKey,
			bundleVersion,
			resourceKey,
		).
		First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *DashboardTemplateRepo) CreateTemplateWithBindings(
	ctx context.Context,
	template *model.LocalDashboardTemplate,
	bindings []*model.LocalDashboardTemplateBinding,
) error {
	now := time.Now().UTC()
	template.ID = uuid.New()
	template.CreatedAt = now
	template.UpdatedAt = now
	template.DownloadedAt = now

	return global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(template).Error; err != nil {
			return fmt.Errorf("create local dashboard template: %w", err)
		}
		for _, binding := range bindings {
			binding.ID = uuid.New()
			binding.DashboardTemplateID = template.ID
			binding.CreatedAt = now
			binding.UpdatedAt = now
			if err := tx.Create(binding).Error; err != nil {
				return fmt.Errorf("create local dashboard template binding: %w", err)
			}
		}
		return nil
	})
}

func (r *DashboardTemplateRepo) ListAll(
	ctx context.Context,
	tenantID, keyword, source string,
) ([]*model.LocalDashboardTemplate, error) {
	db := global.DB.WithContext(ctx).
		Model(&model.LocalDashboardTemplate{}).
		Where("tenant_id = ?", tenantID)
	if keyword != "" {
		db = db.Where("name ILIKE ?", "%"+keyword+"%")
	}
	if source != "" {
		db = db.Where("source = ?", source)
	}

	var templates []*model.LocalDashboardTemplate
	err := db.Order("updated_at DESC").Find(&templates).Error
	return templates, err
}

func (r *DashboardTemplateRepo) GetByID(
	ctx context.Context,
	tenantID, templateID string,
) (*model.LocalDashboardTemplate, error) {
	var template model.LocalDashboardTemplate
	err := global.DB.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", templateID, tenantID).
		First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *DashboardTemplateRepo) ListBindings(
	ctx context.Context,
	templateID string,
) ([]*model.LocalDashboardTemplateBinding, error) {
	var bindings []*model.LocalDashboardTemplateBinding
	err := global.DB.WithContext(ctx).
		Where("dashboard_template_id = ?", templateID).
		Order("created_at ASC").
		Find(&bindings).Error
	return bindings, err
}

func (r *DashboardTemplateRepo) CountCompatibleDevices(
	ctx context.Context,
	tenantID, deviceTemplateID string,
) (int64, error) {
	var count int64
	err := global.DB.WithContext(ctx).
		Table("devices AS d").
		Joins("JOIN device_configs AS dc ON dc.id = d.device_config_id").
		Where("d.tenant_id = ? AND dc.tenant_id = ? AND dc.device_template_id = ?", tenantID, tenantID, deviceTemplateID).
		Count(&count).Error
	return count, err
}

func (r *DashboardTemplateRepo) CountInstances(
	ctx context.Context,
	tenantID, templateID string,
) (int64, error) {
	var count int64
	err := global.DB.WithContext(ctx).
		Model(&model.LocalDashboardTemplateInstance{}).
		Where("tenant_id = ? AND dashboard_template_id = ?", tenantID, templateID).
		Count(&count).Error
	return count, err
}

func (r *DashboardTemplateRepo) ListCompatibleDevices(
	ctx context.Context,
	tenantID, deviceTemplateID string,
) ([]*model.CompatibleDevice, error) {
	type deviceRow struct {
		ID                 string  `gorm:"column:id"`
		Name               *string `gorm:"column:name"`
		DeviceNumber       string  `gorm:"column:device_number"`
		DeviceTemplateID   string  `gorm:"column:device_template_id"`
		DeviceTemplateName string  `gorm:"column:device_template_name"`
		IsOnline           int16   `gorm:"column:is_online"`
	}

	var rows []deviceRow
	err := global.DB.WithContext(ctx).
		Table("devices AS d").
		Select(`
			d.id,
			d.name,
			d.device_number,
			d.is_online,
			dc.device_template_id,
			dt.name AS device_template_name
		`).
		Joins("JOIN device_configs AS dc ON dc.id = d.device_config_id").
		Joins("JOIN device_templates AS dt ON dt.id = dc.device_template_id").
		Where(
			"d.tenant_id = ? AND dc.tenant_id = ? AND dt.tenant_id = ? AND dc.device_template_id = ?",
			tenantID,
			tenantID,
			tenantID,
			deviceTemplateID,
		).
		Order("d.is_online DESC, d.name ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	devices := make([]*model.CompatibleDevice, 0, len(rows))
	for _, row := range rows {
		name := ""
		if row.Name != nil {
			name = *row.Name
		}
		devices = append(devices, &model.CompatibleDevice{
			ID:                 row.ID,
			Name:               name,
			DeviceNumber:       row.DeviceNumber,
			DeviceTemplateID:   row.DeviceTemplateID,
			DeviceTemplateName: row.DeviceTemplateName,
			Online:             row.IsOnline == 1,
		})
	}
	return devices, nil
}

func (r *DashboardTemplateRepo) IsCompatibleDevice(
	ctx context.Context,
	tenantID, deviceTemplateID, deviceID string,
) (bool, error) {
	var count int64
	err := global.DB.WithContext(ctx).
		Table("devices AS d").
		Joins("JOIN device_configs AS dc ON dc.id = d.device_config_id").
		Joins("JOIN device_templates AS dt ON dt.id = dc.device_template_id").
		Where(
			"d.id = ? AND d.tenant_id = ? AND dc.tenant_id = ? AND dt.tenant_id = ? AND dc.device_template_id = ?",
			deviceID,
			tenantID,
			tenantID,
			tenantID,
			deviceTemplateID,
		).
		Count(&count).Error
	return count == 1, err
}

func (r *DashboardTemplateRepo) HasThingModelField(
	ctx context.Context,
	tenantID, deviceTemplateID, kind, identifier, dataType string,
) (bool, error) {
	table := ""
	hasDataType := false
	switch kind {
	case string(model.ThingModelFieldKindTelemetry):
		table = "device_model_telemetry"
		hasDataType = true
	case string(model.ThingModelFieldKindAttribute):
		table = "device_model_attributes"
		hasDataType = true
	case string(model.ThingModelFieldKindCommand):
		table = "device_model_commands"
	case string(model.ThingModelFieldKindEvent):
		table = "device_model_events"
	default:
		return false, fmt.Errorf("unsupported thing model field kind: %s", kind)
	}

	db := global.DB.WithContext(ctx).
		Table(table).
		Where(
			"tenant_id = ? AND device_template_id = ? AND data_identifier = ?",
			tenantID,
			deviceTemplateID,
			identifier,
		)
	if hasDataType && dataType != "" {
		db = db.Where("data_type = ?", dataType)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *DashboardTemplateRepo) CreateInstance(
	ctx context.Context,
	instance *model.LocalDashboardTemplateInstance,
) error {
	instance.ID = uuid.New()
	instance.CreatedAt = time.Now().UTC()
	return global.DB.WithContext(ctx).Create(instance).Error
}

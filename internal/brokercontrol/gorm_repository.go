package brokercontrol

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"project/internal/brokermapper"
)

var ErrBindingConflict = errors.New("brokercontrol: binding conflict")

type GormRepository struct {
	DB     *gorm.DB
	Mapper *brokermapper.Repository
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{DB: db, Mapper: brokermapper.NewRepository(db)}
}

func (r *GormRepository) quietDB(ctx context.Context) *gorm.DB {
	return r.DB.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx)
}

func (r *GormRepository) FindCredentialMatches(ctx context.Context, voucherJSON string) ([]DeviceRecord, error) {
	var devices []DeviceRecord
	// voucher is an existing text column. The RHS is a bound JSON value and
	// PostgreSQL JSONB equality ignores object key order but not object members.
	err := r.quietDB(ctx).Raw(`SELECT d.id, d.tenant_id, d.device_number,
		(SELECT count(*) FROM devices d2 WHERE d2.device_number = d.device_number) AS device_number_count,
		d.is_enabled, d.activate_flag, d.parent_id, d.device_config_id,
		COALESCE(c.device_type, '') AS device_type,
		NULLIF(to_jsonb(d)->>'service_access_id','') AS service_access_id,
		NULLIF(to_jsonb(d)->>'access_way','') AS access_way,
		NULLIF(to_jsonb(c)->>'protocol_type','') AS protocol_type
		FROM devices d LEFT JOIN device_configs c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id
		WHERE md5(broker_safe_voucher_v1(d.voucher)::text) = md5(?::jsonb::text)
		AND broker_safe_voucher_v1(d.voucher) = ?::jsonb ORDER BY d.id LIMIT 2`, voucherJSON, voucherJSON).Scan(&devices).Error
	return devices, err
}

func (r *GormRepository) ResolveServiceTargets(ctx context.Context, ids []string) ([]DeviceRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var devices []DeviceRecord
	err := r.quietDB(ctx).Raw(`SELECT d.id, d.tenant_id, d.device_number,
		(SELECT count(*) FROM devices d2 WHERE d2.device_number = d.device_number) AS device_number_count,
		d.is_enabled, d.activate_flag, d.parent_id, d.device_config_id,
		COALESCE(c.device_type, '') AS device_type,
		NULLIF(to_jsonb(d)->>'service_access_id','') AS service_access_id,
		NULLIF(to_jsonb(d)->>'access_way','') AS access_way,
		NULLIF(to_jsonb(c)->>'protocol_type','') AS protocol_type
		FROM devices d LEFT JOIN device_configs c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id
		WHERE d.id IN ? AND d.is_enabled = 'enabled' AND d.activate_flag = 'active'
		AND (d.device_config_id IS NULL OR c.id IS NOT NULL) ORDER BY d.id`, ids).Scan(&devices).Error
	return devices, err
}

func (r *GormRepository) ListEnabledChildren(ctx context.Context, tenantID, parentID string) ([]DeviceRecord, error) {
	var children []DeviceRecord
	err := r.quietDB(ctx).Table("devices").Select("id, tenant_id, device_number, is_enabled, activate_flag, parent_id, device_config_id").
		Where("tenant_id = ? AND parent_id = ? AND is_enabled = ? AND activate_flag = ?", tenantID, parentID, "enabled", "active").
		Order("device_number ASC, id ASC").Find(&children).Error
	return children, err
}

func (r *GormRepository) HasEnabledTopicMappings(ctx context.Context, tenantID, deviceConfigID string) (bool, error) {
	if deviceConfigID == "" {
		return false, nil
	}
	var exists bool
	err := r.quietDB(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM device_topic_mappings m
		JOIN device_configs c ON CAST(c.id AS text) = CAST(m.device_config_id AS text)
		WHERE c.tenant_id = ? AND c.id = ? AND m.enabled = TRUE
	)`, tenantID, deviceConfigID).Scan(&exists).Error
	return exists, err
}

func (r *GormRepository) CompileMappingSnapshot(ctx context.Context, tenantID, deviceConfigID string) (*brokermapper.Snapshot, error) {
	if deviceConfigID == "" {
		return brokermapper.Standard(), nil
	}
	mapper := r.Mapper
	if mapper == nil {
		mapper = brokermapper.NewRepository(r.DB)
	}
	return mapper.CompileForDeviceConfig(ctx, tenantID, deviceConfigID)
}

func (r *GormRepository) ClaimClientID(ctx context.Context, cell, wireID, tenantID, deviceID string) (bool, error) {
	var sameOwner bool
	err := r.quietDB(ctx).Transaction(func(tx *gorm.DB) error {
		row := ClientIDBinding{BrokerCell: cell, WireClientID: wireID, TenantID: tenantID, DeviceID: deviceID, CreatedAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "broker_cell"}, {Name: "wire_client_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		var current ClientIDBinding
		if err := tx.Where("broker_cell = ? AND wire_client_id = ?", cell, wireID).Take(&current).Error; err != nil {
			return err
		}
		sameOwner = current.TenantID == tenantID && current.DeviceID == deviceID
		return nil
	})
	return sameOwner, err
}

func (r *GormRepository) ResolveActiveBinding(ctx context.Context, tenantID, deviceID, requestedCell string) (TransportBinding, error) {
	return r.resolveBinding(ctx, tenantID, deviceID, requestedCell, false)
}

// ResolveAdmissionBinding permits a connecting device to establish evidence of
// actual access. Business commits and command delivery remain paused.
func (r *GormRepository) ResolveAdmissionBinding(ctx context.Context, tenantID, deviceID, requestedCell string) (TransportBinding, error) {
	return r.resolveBinding(ctx, tenantID, deviceID, requestedCell, true)
}

func (r *GormRepository) resolveBinding(ctx context.Context, tenantID, deviceID, requestedCell string, connecting bool) (TransportBinding, error) {
	var binding TransportBinding
	err := r.quietDB(ctx).Transaction(func(tx *gorm.DB) error {
		row := TransportBinding{TenantID: tenantID, DeviceID: deviceID, BrokerCell: requestedCell, RouteEpoch: 1, Phase: "active", UpdatedAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "device_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND device_id = ?", tenantID, deviceID).Take(&binding).Error; err != nil {
			return err
		}
		if binding.BrokerCell != requestedCell || (binding.Phase != "active" && !(connecting && binding.Phase == "connecting")) || binding.RouteEpoch == 0 {
			return ErrBindingConflict
		}
		return nil
	})
	if err != nil {
		return TransportBinding{}, err
	}
	return binding, nil
}

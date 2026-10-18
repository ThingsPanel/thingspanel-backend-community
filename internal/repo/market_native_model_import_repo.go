package repo

import (
	"context"
	"fmt"

	"project/pkg/global"

	"gorm.io/gorm"
)

type MarketNativeModelImport struct {
	ID                  string  `gorm:"column:id"`
	TenantID            string  `gorm:"column:tenant_id"`
	MarketTemplateID    string  `gorm:"column:market_template_id"`
	MarketVersion       string  `gorm:"column:market_version"`
	SourceModelID       string  `gorm:"column:source_model_id"`
	SourceSnapshotID    string  `gorm:"column:source_snapshot_id"`
	SourceRevision      int     `gorm:"column:source_revision"`
	DependencyChecksum  string  `gorm:"column:dependency_checksum"`
	CreatedBy           string  `gorm:"column:created_by"`
	Status              string  `gorm:"column:status"`
	LocalModelID        *string `gorm:"column:local_model_id"`
	LocalSnapshotID     *string `gorm:"column:local_snapshot_id"`
	LocalRevision       *int    `gorm:"column:local_revision"`
	LocalDeviceConfigID *string `gorm:"column:local_device_config_id"`
}

func ReserveMarketNativeModelImport(ctx context.Context, candidate MarketNativeModelImport) (*MarketNativeModelImport, bool, error) {
	var current MarketNativeModelImport
	created := false
	err := global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			INSERT INTO market_native_model_imports
				(tenant_id, market_template_id, market_version, source_model_id, source_snapshot_id,
				 source_revision, dependency_checksum, created_by, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'IMPORTING')
			ON CONFLICT (tenant_id, market_template_id, market_version) DO NOTHING`,
			candidate.TenantID, candidate.MarketTemplateID, candidate.MarketVersion,
			candidate.SourceModelID, candidate.SourceSnapshotID, candidate.SourceRevision,
			candidate.DependencyChecksum, candidate.CreatedBy)
		if result.Error != nil {
			return fmt.Errorf("reserve native model import: %w", result.Error)
		}
		created = result.RowsAffected == 1
		if err := tx.Raw(`SELECT * FROM market_native_model_imports
			WHERE tenant_id = ? AND market_template_id = ? AND market_version = ? FOR UPDATE`,
			candidate.TenantID, candidate.MarketTemplateID, candidate.MarketVersion).Scan(&current).Error; err != nil {
			return fmt.Errorf("read native model import identity: %w", err)
		}
		if current.ID == "" {
			return fmt.Errorf("native model import identity disappeared")
		}
		if current.SourceModelID != candidate.SourceModelID || current.SourceSnapshotID != candidate.SourceSnapshotID ||
			current.SourceRevision != candidate.SourceRevision || current.DependencyChecksum != candidate.DependencyChecksum {
			return fmt.Errorf("market template version is already mapped to a different immutable native dependency")
		}
		if current.Status == "IMPORTING" && current.CreatedBy != candidate.CreatedBy {
			return fmt.Errorf("native model import is in progress and can only be resumed by its original installer")
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &current, created, nil
}

func MarkMarketNativeModelReady(ctx context.Context, id, modelID, snapshotID string, revision int) error {
	result := global.DB.WithContext(ctx).Exec(`
		UPDATE market_native_model_imports
		SET status = 'NATIVE_READY', local_model_id = ?, local_snapshot_id = ?, local_revision = ?, updated_at = NOW()
		WHERE id = ? AND status <> 'COMPLETE'`, modelID, snapshotID, revision, id)
	if result.Error != nil {
		return fmt.Errorf("save imported native model mapping: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("native model import mapping is not updateable")
	}
	return nil
}

func MarkMarketNativeModelImportComplete(tx *gorm.DB, id, deviceConfigID string) error {
	result := tx.Exec(`
		UPDATE market_native_model_imports
		SET status = 'COMPLETE', local_device_config_id = ?, updated_at = NOW()
		WHERE id = ? AND status = 'NATIVE_READY'`, deviceConfigID, id)
	if result.Error != nil {
		return fmt.Errorf("complete native model import mapping: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("native model import mapping is not ready")
	}
	return nil
}

func LockMarketNativeModelImport(tx *gorm.DB, id string) (*MarketNativeModelImport, error) {
	var current MarketNativeModelImport
	if err := tx.Raw(`SELECT * FROM market_native_model_imports WHERE id = ? FOR UPDATE`, id).Scan(&current).Error; err != nil {
		return nil, fmt.Errorf("lock native model import mapping: %w", err)
	}
	if current.ID == "" {
		return nil, fmt.Errorf("native model import mapping not found")
	}
	return &current, nil
}

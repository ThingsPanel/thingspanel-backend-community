package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"project/internal/model"
	"project/pkg/global"
)

type MarketInstallConfigReceipt struct {
	TenantID        string
	SubjectID       string
	OperationKey    string
	PayloadHash     string
	InstanceKey     string
	ResourceKey     string
	ConfigID        string
	BindingRevision int
	Result          json.RawMessage
}

func (MarketInstallConfigReceipt) TableName() string { return "market_install_config_receipts" }

var ErrMarketInstallConfigChanged = errors.New("installed configuration was removed or changed")

func ReadMarketInstallConfigReceipt(ctx context.Context, req model.MarketInstallStepRequest) (*MarketInstallConfigReceipt, error) {
	var receipt MarketInstallConfigReceipt
	err := global.DB.WithContext(ctx).Where("tenant_id=? AND subject_id=? AND operation_key=?", req.TenantID, req.SubjectID, req.IdempotencyKey).First(&receipt).Error
	if err != nil {
		return nil, err
	}
	if receipt.PayloadHash != req.PayloadHash || receipt.InstanceKey != req.OperationID || receipt.ResourceKey != req.Step.ResourceKey {
		return nil, ErrMarketInstallUnauthorized
	}
	if err := verifyMarketInstallConfigTarget(global.DB.WithContext(ctx), receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}
func verifyMarketInstallConfigTarget(db *gorm.DB, receipt MarketInstallConfigReceipt) error {
	var expected struct {
		ModelID    string `json:"modelId"`
		SnapshotID string `json:"snapshotId"`
		Revision   int    `json:"revision"`
	}
	if err := json.Unmarshal(receipt.Result, &expected); err != nil {
		return err
	}
	var config model.DeviceConfig
	err := db.Where("id=? AND tenant_id=?", receipt.ConfigID, receipt.TenantID).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrMarketInstallConfigChanged
	}
	if err != nil {
		return err
	}
	if config.NativeThingModelID == nil || *config.NativeThingModelID != expected.ModelID || config.NativeThingModelSnapshotID == nil || *config.NativeThingModelSnapshotID != expected.SnapshotID || config.NativeThingModelRevision == nil || *config.NativeThingModelRevision != expected.Revision || config.NativeBindingRevision != receipt.BindingRevision || config.DeviceTemplateID != nil {
		return ErrMarketInstallConfigChanged
	}
	return nil
}

// ExecuteMarketInstallConfigBinding reuses the existing immutable import mapping
// and commits configuration, fixed binding, mapping and per-step receipt in one
// Community-owned transaction. Native model creation stays outside this DB.
func ExecuteMarketInstallConfigBinding(ctx context.Context, req model.MarketInstallStepRequest, identity MarketNativeModelImport, candidate *model.DeviceConfig) (*MarketInstallConfigReceipt, error) {
	reserved, _, err := ReserveMarketNativeModelImport(ctx, identity)
	if err != nil {
		return nil, err
	}
	var output MarketInstallConfigReceipt
	err = global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "a08-config:"+req.TenantID+":"+req.SubjectID+":"+req.IdempotencyKey).Error; err != nil {
			return err
		}
		var existing MarketInstallConfigReceipt
		lookup := tx.Where("tenant_id=? AND subject_id=? AND operation_key=?", req.TenantID, req.SubjectID, req.IdempotencyKey).First(&existing)
		if lookup.Error == nil {
			if existing.PayloadHash != req.PayloadHash || existing.InstanceKey != req.OperationID || existing.ResourceKey != req.Step.ResourceKey {
				return ErrMarketInstallUnauthorized
			}
			if err := verifyMarketInstallConfigTarget(tx, existing); err != nil {
				return err
			}
			output = existing
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		locked, err := LockMarketNativeModelImport(tx, reserved.ID)
		if err != nil {
			return err
		}
		config := candidate
		if locked.Status == "COMPLETE" {
			if locked.LocalDeviceConfigID == nil {
				return ErrMarketInstallConfigChanged
			}
			var prior model.DeviceConfig
			if err := tx.Where("id=? AND tenant_id=?", *locked.LocalDeviceConfigID, req.TenantID).First(&prior).Error; err != nil {
				return err
			}
			if prior.NativeThingModelID == nil || prior.NativeThingModelSnapshotID == nil || prior.NativeThingModelRevision == nil || *prior.NativeThingModelID != *candidate.NativeThingModelID || *prior.NativeThingModelSnapshotID != *candidate.NativeThingModelSnapshotID || *prior.NativeThingModelRevision != *candidate.NativeThingModelRevision || prior.DeviceTemplateID != nil {
				return ErrMarketInstallConfigChanged
			}
			config = &prior
		} else {
			if locked.Status != "IMPORTING" && locked.Status != "NATIVE_READY" {
				return fmt.Errorf("native import mapping is not bindable")
			}
			if locked.Status == "NATIVE_READY" && (locked.LocalModelID == nil || locked.LocalSnapshotID == nil || locked.LocalRevision == nil || *locked.LocalModelID != *candidate.NativeThingModelID || *locked.LocalSnapshotID != *candidate.NativeThingModelSnapshotID || *locked.LocalRevision != *candidate.NativeThingModelRevision) {
				return ErrMarketInstallConfigChanged
			}
			if err := tx.Create(candidate).Error; err != nil {
				return err
			}
			result := tx.Table("market_native_model_imports").Where("id=? AND status IN ('IMPORTING','NATIVE_READY')", locked.ID).Updates(map[string]interface{}{"status": "COMPLETE", "local_model_id": *candidate.NativeThingModelID, "local_snapshot_id": *candidate.NativeThingModelSnapshotID, "local_revision": *candidate.NativeThingModelRevision, "local_device_config_id": candidate.ID})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrMarketInstallConfigChanged
			}
		}
		data, err := json.Marshal(struct {
			ConfigID        string `json:"configId"`
			ModelID         string `json:"modelId"`
			SnapshotID      string `json:"snapshotId"`
			Revision        int    `json:"revision"`
			BindingRevision int    `json:"bindingRevision"`
			Ownership       string `json:"ownership"`
		}{config.ID, *config.NativeThingModelID, *config.NativeThingModelSnapshotID, *config.NativeThingModelRevision, config.NativeBindingRevision, "shared_dependency"})
		if err != nil {
			return err
		}
		output = MarketInstallConfigReceipt{TenantID: req.TenantID, SubjectID: req.SubjectID, OperationKey: req.IdempotencyKey, PayloadHash: req.PayloadHash, InstanceKey: req.OperationID, ResourceKey: req.Step.ResourceKey, ConfigID: config.ID, BindingRevision: config.NativeBindingRevision, Result: data}
		return tx.Create(&output).Error
	})
	if err != nil {
		return nil, err
	}
	return &output, nil
}

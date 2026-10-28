package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"project/internal/model"
	"project/internal/query"
	"project/internal/repo"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
)

func marketNativeAssetName(tenantID, templateID, version, dependencyChecksum string) string {
	hash := sha256.Sum256([]byte(tenantID + "\x00" + templateID + "\x00" + version + "\x00" + dependencyChecksum))
	return "native-" + hex.EncodeToString(hash[:])
}

func installNativeMarketDeviceConfig(ctx context.Context, market *MarketClient, req model.InstallFromMarketReq, fullData *model.MarketTemplateFullData, dependency *model.MarketNativeThingModelDependency, localCoverURL *string, claims *utils.UserClaims, communityAuthorization string) (*model.InstallFromMarketRsp, bool, error) {
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.ID) == "" {
		return nil, false, errcode.WithData(errcode.CodeParamError, "authenticated tenant and user are required")
	}
	if fullData == nil || strings.TrimSpace(req.Version) == "" || strings.TrimSpace(fullData.Version) == "" || req.Version != fullData.Version {
		return nil, false, errcode.WithData(errcode.CodeParamError, "market download did not return the requested immutable version")
	}
	if err := verifyMarketNativeDependency(dependency); err != nil {
		return nil, false, errcode.WithData(errcode.CodeParamError, err.Error())
	}
	targetResources, err := resolveMarketNativeDependencyResources(dependency, localCoverURL)
	if err != nil {
		return nil, false, errcode.WithData(errcode.CodeParamError, err.Error())
	}
	if err := validateMarketPortableResources(targetResources); err != nil {
		return nil, false, errcode.WithData(errcode.CodeParamError, err.Error())
	}
	client, err := newNativeMarketImportClient()
	if err != nil {
		return nil, false, errcode.WithData(errcode.CodeSystemError, err.Error())
	}
	identity := repo.MarketNativeModelImport{
		TenantID: claims.TenantID, MarketTemplateID: req.MarketTemplateID, MarketVersion: fullData.Version,
		SourceModelID: dependency.SourceModelID, SourceSnapshotID: dependency.SourceSnapshotID,
		SourceRevision: dependency.SourceRevision, DependencyChecksum: dependency.DependencyChecksum, CreatedBy: claims.ID,
	}
	importRecord, _, err := repo.ReserveMarketNativeModelImport(ctx, identity)
	if err != nil {
		return nil, false, errcode.WithData(errcode.CodeDBError, err.Error())
	}
	if importRecord.Status == "COMPLETE" {
		config, err := readCompletedNativeImport(ctx, importRecord, claims.TenantID)
		if err != nil {
			return nil, false, errcode.WithData(errcode.CodeDBError, err.Error())
		}
		return &model.InstallFromMarketRsp{DeviceConfigID: config.ID, DeviceConfig: config, MissingPlugins: checkMissingPlugins(fullData.PluginDependencies)}, false, nil
	}
	if importRecord.Status == "IMPORTING" {
		result, err := client.importSnapshot(ctx, communityAuthorization, importRecord.ID, dependency, targetResources)
		if err != nil {
			return nil, false, errcode.WithData(errcode.CodeSystemError, "native thing-model import failed: "+err.Error())
		}
		if result.PublishedRevision == nil || result.SnapshotID == "" {
			return nil, false, errcode.WithData(errcode.CodeSystemError, "native publish did not return a verified immutable snapshot")
		}
		if err := repo.MarkMarketNativeModelReady(ctx, importRecord.ID, result.ModelID, result.SnapshotID, int(*result.PublishedRevision)); err != nil {
			return nil, false, errcode.WithData(errcode.CodeDBError, err.Error())
		}
		importRecord.Status = "NATIVE_READY"
		importRecord.LocalModelID = &result.ModelID
		importRecord.LocalSnapshotID = &result.SnapshotID
		revision := int(*result.PublishedRevision)
		importRecord.LocalRevision = &revision
	}
	if importRecord.Status != "NATIVE_READY" || importRecord.LocalModelID == nil || importRecord.LocalSnapshotID == nil || importRecord.LocalRevision == nil {
		return nil, false, errcode.WithData(errcode.CodeSystemError, "native model import mapping is incomplete")
	}
	importedSnapshot, err := client.readSnapshot(ctx, communityAuthorization, *importRecord.LocalModelID, *importRecord.LocalRevision)
	if err != nil || importedSnapshot.SnapshotID != *importRecord.LocalSnapshotID {
		return nil, false, errcode.WithData(errcode.CodeSystemError, "imported native model snapshot is no longer available")
	}
	var snapshotVerifyErr error
	if importedSnapshot.SnapshotID == dependency.SourceSnapshotID {
		snapshotVerifyErr = verifyMarketSourceSnapshotContent(dependency, importedSnapshot)
	} else {
		snapshotVerifyErr = verifyImportedNativeSnapshotContent(dependency.Model, targetResources, importedSnapshot)
	}
	if snapshotVerifyErr != nil {
		return nil, false, errcode.WithData(errcode.CodeSystemError, "imported native model failed full payload verification")
	}
	deviceConfig := buildImportedMarketDeviceConfig(fullData, claims, localCoverURL, importRecord)
	tx := global.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, false, errcode.WithData(errcode.CodeDBError, "begin native market install transaction")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback().Error
			panic(recovered)
		}
	}()
	locked, err := repo.LockMarketNativeModelImport(tx, importRecord.ID)
	if err != nil {
		_ = tx.Rollback().Error
		return nil, false, errcode.WithData(errcode.CodeDBError, err.Error())
	}
	if locked.Status == "COMPLETE" {
		if err := tx.Commit().Error; err != nil {
			return nil, false, errcode.WithData(errcode.CodeDBError, "finish concurrent native install lookup")
		}
		config, err := readCompletedNativeImport(ctx, locked, claims.TenantID)
		if err != nil {
			return nil, false, errcode.WithData(errcode.CodeDBError, err.Error())
		}
		return &model.InstallFromMarketRsp{DeviceConfigID: config.ID, DeviceConfig: config, MissingPlugins: checkMissingPlugins(fullData.PluginDependencies)}, false, nil
	}
	if locked.Status != "NATIVE_READY" || locked.LocalModelID == nil || *locked.LocalModelID != *importRecord.LocalModelID || locked.LocalSnapshotID == nil || *locked.LocalSnapshotID != *importRecord.LocalSnapshotID || locked.LocalRevision == nil || *locked.LocalRevision != *importRecord.LocalRevision {
		_ = tx.Rollback().Error
		return nil, false, errcode.WithData(errcode.CodeDBError, "native model mapping changed during install")
	}
	if err := tx.Create(deviceConfig).Error; err != nil {
		_ = tx.Rollback().Error
		return nil, false, errcode.WithData(errcode.CodeDBError, "create native-bound device config failed")
	}
	if err := repo.MarkMarketNativeModelImportComplete(tx, importRecord.ID, deviceConfig.ID); err != nil {
		_ = tx.Rollback().Error
		return nil, false, errcode.WithData(errcode.CodeDBError, err.Error())
	}
	if err := tx.Commit().Error; err != nil {
		return nil, false, errcode.WithData(errcode.CodeDBError, "commit native market install failed")
	}
	created, err := query.DeviceConfig.WithContext(ctx).Where(query.DeviceConfig.ID.Eq(deviceConfig.ID), query.DeviceConfig.TenantID.Eq(claims.TenantID)).First()
	if err != nil || created == nil || created.NativeThingModelID == nil || *created.NativeThingModelID != *importRecord.LocalModelID || created.NativeThingModelSnapshotID == nil || *created.NativeThingModelSnapshotID != *importRecord.LocalSnapshotID || created.NativeThingModelRevision == nil || *created.NativeThingModelRevision != *importRecord.LocalRevision || created.DeviceTemplateID != nil {
		return nil, false, errcode.WithData(errcode.CodeDBError, "installed native device config failed post-commit ownership/binding verification")
	}
	go func() {
		_ = market.ReportTemplateUsage(context.Background(), req.MarketToken, req.MarketTemplateID, fullData.VersionID, deviceConfig.ID)
	}()
	return &model.InstallFromMarketRsp{DeviceConfigID: created.ID, DeviceConfig: created, MissingPlugins: checkMissingPlugins(fullData.PluginDependencies)}, true, nil
}

func readCompletedNativeImport(ctx context.Context, record *repo.MarketNativeModelImport, tenantID string) (*model.DeviceConfig, error) {
	if record.LocalDeviceConfigID == nil || record.LocalModelID == nil || record.LocalSnapshotID == nil || record.LocalRevision == nil {
		return nil, fmt.Errorf("complete native import mapping has no local references")
	}
	config, err := query.DeviceConfig.WithContext(ctx).Where(query.DeviceConfig.ID.Eq(*record.LocalDeviceConfigID), query.DeviceConfig.TenantID.Eq(tenantID)).First()
	if err != nil || config == nil {
		return nil, fmt.Errorf("complete native import config is not owned by the current tenant")
	}
	if config.DeviceTemplateID != nil || config.NativeThingModelID == nil || *config.NativeThingModelID != *record.LocalModelID || config.NativeThingModelSnapshotID == nil || *config.NativeThingModelSnapshotID != *record.LocalSnapshotID || config.NativeThingModelRevision == nil || *config.NativeThingModelRevision != *record.LocalRevision {
		return nil, fmt.Errorf("complete native import config binding no longer matches its immutable mapping")
	}
	return config, nil
}

func buildImportedMarketDeviceConfig(fullData *model.MarketTemplateFullData, claims *utils.UserClaims, localCoverURL *string, record *repo.MarketNativeModelImport) *model.DeviceConfig {
	now := time.Now().UTC()
	config := &model.DeviceConfig{
		ID: uuid.New(), Name: fullData.Name, DeviceType: "1", TenantID: claims.TenantID,
		NativeThingModelID: record.LocalModelID, NativeThingModelSnapshotID: record.LocalSnapshotID,
		NativeThingModelRevision: record.LocalRevision, NativeBindingRevision: 1,
		ImageURL: localCoverURL, CreatedAt: now, UpdatedAt: now,
	}
	if fullData.DeviceConfig == nil {
		return config
	}
	config.Name = fullData.DeviceConfig.Name
	if config.Name == "" {
		config.Name = fullData.Name
	}
	if fullData.DeviceConfig.DeviceType != "" {
		config.DeviceType = fullData.DeviceConfig.DeviceType
	}
	config.ProtocolType = ptrStrP(fullData.DeviceConfig.ProtocolType)
	config.VoucherType = ptrStrP(fullData.DeviceConfig.VoucherType)
	config.DeviceConnType = ptrStrP(fullData.DeviceConfig.DeviceConnType)
	if raw, err := json.Marshal(scrubMarketSecrets(fullData.DeviceConfig.ProtocolConfig)); err == nil {
		config.ProtocolConfig = ptrStrP(string(raw))
	}
	if raw, err := json.Marshal(scrubMarketSecrets(fullData.DeviceConfig.OtherConfig)); err == nil {
		config.OtherConfig = ptrStrP(string(raw))
	}
	if raw, err := json.Marshal(scrubMarketSecrets(fullData.DeviceConfig.AdditionalInfo)); err == nil {
		config.AdditionalInfo = ptrStrP(string(raw))
	}
	config.AutoRegister = fullData.DeviceConfig.AutoRegister
	return config
}

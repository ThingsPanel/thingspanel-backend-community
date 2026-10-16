package service

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// InstallFromMarket downloads a template from the market and creates it locally:
// 1. DeviceTemplate (物模型 + 面板配置)
// 2. DeviceConfig (凭证协议配置，引用新创建的 DeviceTemplate)
func (*DeviceTemplate) InstallFromMarket(req model.InstallFromMarketReq, claims *utils.UserClaims) (*model.InstallFromMarketRsp, error) {
	// 1. Download the full template definition from market
	client := NewMarketClient()
	fullData, err := client.DownloadTemplate(context.Background(), req.MarketToken, req.MarketTemplateID, req.Version)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to download template from market: " + err.Error(),
		})
	}

	// Copy the resource-center cover locally before creating database records.
	// The local file is removed if any later installation step fails.
	localImageURL, localImagePath, err := localizeMarketTemplateImage(context.Background(), client, fullData)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to localize market template cover: " + err.Error(),
		})
	}
	installCommitted := false
	defer func() {
		if !installCommitted && localImagePath != "" {
			if removeErr := os.Remove(localImagePath); removeErr != nil && !os.IsNotExist(removeErr) {
				logrus.Errorf("Failed to clean up localized market template cover %s: %v", localImagePath, removeErr)
			}
		}
	}()

	// 2. Check plugin dependencies (before any DB writes)
	missingPlugins := checkMissingPlugins(fullData.PluginDependencies)

	// 3. Begin transaction
	tx := global.DB.Begin()
	if tx.Error != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to begin transaction: " + tx.Error.Error(),
		})
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Serialize installs for the same tenant/name pair. The lookup below is
	// otherwise vulnerable to two concurrent requests both observing no row and
	// creating duplicate device templates.
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", marketTemplateInstallLockKey(claims.TenantID, fullData.Name)).Error; err != nil {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to lock device template installation: " + err.Error(),
		})
	}

	// ── 3.1 Create/update DeviceTemplate ──────────────────────────────────────
	now := time.Now().UTC()
	flag := int16(1) // private

	// Check if template with same name already exists
	existingTpl, err := query.DeviceTemplate.WithContext(context.Background()).
		Where(query.DeviceTemplate.Name.Eq(fullData.Name), query.DeviceTemplate.TenantID.Eq(claims.TenantID)).
		First()
	if err != nil && err.Error() != "record not found" {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to check existing template: " + err.Error(),
		})
	}

	var templateID string
	isUpdate := false
	if existingTpl != nil {
		templateID = existingTpl.ID
		isUpdate = true
	} else {
		templateID = uuid.New()
		isUpdate = false
	}

	// Parse template definition (panel config + web_chart_config / app_chart_config)
	var webChartConfig, appChartConfig string
	tplDef := fullData.TemplateDefinition // *map[string]interface{}
	if tplDef != nil {
		if v, ok := (*tplDef)["web_chart_config"]; ok && v != nil {
			if bytes, err := json.Marshal(v); err == nil {
				webChartConfig = string(bytes)
			}
		}
		if v, ok := (*tplDef)["app_chart_config"]; ok && v != nil {
			if bytes, err := json.Marshal(v); err == nil {
				appChartConfig = string(bytes)
			}
		}
	}

	newTemplate := &model.DeviceTemplate{
		ID:             templateID,
		Name:           fullData.Name,
		TenantID:       claims.TenantID,
		Brand:          ptrStrP(fullData.Brand),
		ModelNumber:    ptrStrP(fullData.ModelNumber),
		Author:         ptrStrP(fullData.Author),
		Version:        ptrStrP(fullData.Version),
		Description:    ptrStrP(fullData.Description),
		Flag:           &flag,
		WebChartConfig: ptrStrP(webChartConfig),
		AppChartConfig: ptrStrP(appChartConfig),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if isUpdate {
		// Clean up existing device models before update
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelTelemetry{})
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelAttribute{})
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelEvent{})
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelCommand{})

		if err := tx.Save(newTemplate).Error; err != nil {
			tx.Rollback()
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "Failed to update template: " + err.Error(),
			})
		}
	} else {
		if err := tx.Create(newTemplate).Error; err != nil {
			tx.Rollback()
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "Failed to create template: " + err.Error(),
			})
		}
	}

	// ── 3.2 Create device models from template_definition ─────────────────────
	if err := createMarketTemplateModels(tx, tplDef, templateID, claims.TenantID, now); err != nil {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{"error": err.Error()})
	}

	// ── 3.3 Create DeviceConfig ──────────────────────────────────────────────
	dcID := uuid.New()
	dcName := fullData.Name
	if fullData.DeviceConfig != nil {
		if fullData.DeviceConfig.Name != "" {
			dcName = fullData.DeviceConfig.Name
		}
	}

	newDC := &model.DeviceConfig{
		ID:               dcID,
		Name:             dcName,
		DeviceTemplateID: &templateID, // 引用新创建的 DeviceTemplate
		DeviceType:       "1",         // 默认直连设备
		TenantID:         claims.TenantID,
		ImageURL:         localImageURL,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if fullData.DeviceConfig != nil {
		newDC.DeviceType = fullData.DeviceConfig.DeviceType
		if fullData.DeviceConfig.DeviceType == "" {
			newDC.DeviceType = "1"
		}
		if fullData.DeviceConfig.ProtocolType != "" {
			newDC.ProtocolType = &fullData.DeviceConfig.ProtocolType
		}
		if fullData.DeviceConfig.VoucherType != "" {
			newDC.VoucherType = &fullData.DeviceConfig.VoucherType
		}
		if fullData.DeviceConfig.ProtocolConfig != nil {
			if bytes, err := json.Marshal(fullData.DeviceConfig.ProtocolConfig); err == nil {
				s := string(bytes)
				newDC.ProtocolConfig = &s
			}
		}
		if fullData.DeviceConfig.DeviceConnType != "" {
			newDC.DeviceConnType = &fullData.DeviceConfig.DeviceConnType
		}
		if fullData.DeviceConfig.OtherConfig != nil {
			if bytes, err := json.Marshal(fullData.DeviceConfig.OtherConfig); err == nil {
				s := string(bytes)
				newDC.OtherConfig = &s
			}
		}
		if fullData.DeviceConfig.AdditionalInfo != nil {
			if bytes, err := json.Marshal(fullData.DeviceConfig.AdditionalInfo); err == nil {
				s := string(bytes)
				newDC.AdditionalInfo = &s
			}
		}
		newDC.AutoRegister = fullData.DeviceConfig.AutoRegister
	}

	if err := tx.Create(newDC).Error; err != nil {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to create device config: " + err.Error(),
		})
	}

	// ── 3.4 Commit ───────────────────────────────────────────────────────────
	if err := tx.Commit().Error; err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to commit transaction: " + err.Error(),
		})
	}
	installCommitted = true

	// 4. Report successful local use to the resource center (async, non-blocking).
	// dcID is stable and acts as the idempotency key for this real local import.
	versionID := ""
	if fullData.VersionID != "" {
		versionID = fullData.VersionID
	}
	logrus.Infof("Reporting successful market template use: TemplateID=%s, VersionID=%s, EventID=%s",
		req.MarketTemplateID, versionID, dcID)
	go func() {
		err := client.ReportTemplateUsage(context.Background(), req.MarketToken, req.MarketTemplateID, versionID, dcID)
		if err != nil {
			logrus.Errorf("Failed to report market template usage: %v", err)
		} else {
			logrus.Infof("Successfully reported market template usage for template %s", req.MarketTemplateID)
		}
	}()

	// 5. Fetch created records and return
	createdTpl, _ := dal.GetDeviceTemplateById(templateID)
	createdDC, _ := dal.GetDeviceConfigByID(dcID)

	return &model.InstallFromMarketRsp{
		DeviceConfigID: dcID,
		DeviceTemplate: createdTpl,
		DeviceConfig:   createdDC,
		MissingPlugins: missingPlugins,
	}, nil
}

func marketTemplateInstallLockKey(tenantID, templateName string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(tenantID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(templateName))
	return int64(h.Sum64())
}

func createMarketTemplateModels(tx *gorm.DB, tplDef *model.TemplateDefinitionPayload, templateID, tenantID string, now time.Time) error {
	if tplDef == nil {
		return nil
	}
	modelRows := []struct {
		key   string
		label string
		new   func(map[string]interface{}) interface{}
	}{
		{"telemetry", "telemetry", func(fields map[string]interface{}) interface{} {
			return &model.DeviceModelTelemetry{ID: uuid.New(), DeviceTemplateID: templateID, TenantID: tenantID, DataName: getStrP(fields, "data_name"), DataIdentifier: getStr(fields, "data_identifier"), ReadWriteFlag: getStrP(fields, "read_write_flag"), DataType: getStrP(fields, "data_type"), Unit: getStrP(fields, "unit"), Description: getStrP(fields, "description"), AdditionalInfo: getStrP(fields, "additional_info"), CreatedAt: now, UpdatedAt: now}
		}},
		{"attributes", "attribute", func(fields map[string]interface{}) interface{} {
			return &model.DeviceModelAttribute{ID: uuid.New(), DeviceTemplateID: templateID, TenantID: tenantID, DataName: getStrP(fields, "data_name"), DataIdentifier: getStr(fields, "data_identifier"), ReadWriteFlag: getStrP(fields, "read_write_flag"), DataType: getStrP(fields, "data_type"), Unit: getStrP(fields, "unit"), Description: getStrP(fields, "description"), AdditionalInfo: getStrP(fields, "additional_info"), CreatedAt: now, UpdatedAt: now}
		}},
		{"events", "event", func(fields map[string]interface{}) interface{} {
			return &model.DeviceModelEvent{ID: uuid.New(), DeviceTemplateID: templateID, TenantID: tenantID, DataName: getStrP(fields, "data_name"), DataIdentifier: getStr(fields, "data_identifier"), Param: getStrP(fields, "params"), Description: getStrP(fields, "description"), AdditionalInfo: getStrP(fields, "additional_info"), CreatedAt: now, UpdatedAt: now}
		}},
		{"commands", "command", func(fields map[string]interface{}) interface{} {
			return &model.DeviceModelCommand{ID: uuid.New(), DeviceTemplateID: templateID, TenantID: tenantID, DataName: getStrP(fields, "data_name"), DataIdentifier: getStr(fields, "data_identifier"), Param: getStrP(fields, "params"), Description: getStrP(fields, "description"), AdditionalInfo: getStrP(fields, "additional_info"), CreatedAt: now, UpdatedAt: now}
		}},
	}
	for _, rowType := range modelRows {
		entries, ok := (*tplDef)[rowType.key].([]interface{})
		if !ok {
			continue
		}
		for _, entry := range entries {
			fields, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			if err := tx.Create(rowType.new(fields)).Error; err != nil {
				return fmt.Errorf("Failed to create %s: %w", rowType.label, err)
			}
		}
	}
	return nil
}

// checkMissingPlugins checks which plugin dependencies are not installed locally
func checkMissingPlugins(deps []model.PluginDependency) []model.PluginDependency {
	if len(deps) == 0 {
		return nil
	}

	var missing []model.PluginDependency
	for _, dep := range deps {
		p, err := dal.GetServicePluginByServiceIdentifier(dep.PluginName)
		if err != nil || p == nil {
			missing = append(missing, dep)
		}
	}
	return missing
}

// ptrStrP returns a pointer to a string (nil-safe)
func ptrStrP(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// getStr safely extracts a String field from a map
func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStrP safely extracts a string and returns a pointer (nil if empty)
func getStrP(m map[string]interface{}, key string) *string {
	s := getStr(m, key)
	if s == "" {
		return nil
	}
	return &s
}

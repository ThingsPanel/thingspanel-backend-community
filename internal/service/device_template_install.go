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
	defer cleanupLocalizedMarketTemplateCover(&installCommitted, localImagePath)

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

	now := time.Now().UTC()
	templateID, err := upsertMarketDeviceTemplate(tx, fullData, claims.TenantID, now)
	if err != nil {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{"error": err.Error()})
	}
	tplDef := fullData.TemplateDefinition

	// ── 3.2 Create device models from template_definition ─────────────────────
	if err := createMarketTemplateModels(tx, tplDef, templateID, claims.TenantID, now); err != nil {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{"error": err.Error()})
	}

	dcID := uuid.New()
	newDC := newMarketDeviceConfig(fullData, templateID, claims.TenantID, localImageURL, dcID, now)

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

func cleanupLocalizedMarketTemplateCover(installCommitted *bool, localImagePath string) {
	if *installCommitted || localImagePath == "" {
		return
	}
	if removeErr := os.Remove(localImagePath); removeErr != nil && !os.IsNotExist(removeErr) {
		logrus.Errorf("Failed to clean up localized market template cover %s: %v", localImagePath, removeErr)
	}
}

func upsertMarketDeviceTemplate(tx *gorm.DB, fullData *model.MarketTemplateFullData, tenantID string, now time.Time) (string, error) {
	existingTpl, err := query.DeviceTemplate.WithContext(context.Background()).
		Where(query.DeviceTemplate.Name.Eq(fullData.Name), query.DeviceTemplate.TenantID.Eq(tenantID)).
		First()
	if err != nil && err.Error() != "record not found" {
		return "", fmt.Errorf("Failed to check existing template: %w", err)
	}
	templateID := uuid.New()
	if existingTpl != nil {
		templateID = existingTpl.ID
	}
	flag := int16(1)
	webChartConfig, appChartConfig := marketTemplateChartConfigs(fullData.TemplateDefinition)
	newTemplate := &model.DeviceTemplate{
		ID:             templateID,
		Name:           fullData.Name,
		TenantID:       tenantID,
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
	if existingTpl != nil {
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelTelemetry{})
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelAttribute{})
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelEvent{})
		tx.Where("device_template_id = ?", templateID).Delete(&model.DeviceModelCommand{})
		if err := tx.Save(newTemplate).Error; err != nil {
			return "", fmt.Errorf("Failed to update template: %w", err)
		}
		return templateID, nil
	}
	if err := tx.Create(newTemplate).Error; err != nil {
		return "", fmt.Errorf("Failed to create template: %w", err)
	}
	return templateID, nil
}

func marketTemplateChartConfigs(tplDef *model.TemplateDefinitionPayload) (string, string) {
	var webChartConfig, appChartConfig string
	if tplDef == nil {
		return webChartConfig, appChartConfig
	}
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
	return webChartConfig, appChartConfig
}

func newMarketDeviceConfig(fullData *model.MarketTemplateFullData, templateID, tenantID string, imageURL *string, deviceConfigID string, now time.Time) *model.DeviceConfig {
	dcName := fullData.Name
	if fullData.DeviceConfig != nil && fullData.DeviceConfig.Name != "" {
		dcName = fullData.DeviceConfig.Name
	}
	newDC := &model.DeviceConfig{
		ID:               deviceConfigID,
		Name:             dcName,
		DeviceTemplateID: &templateID,
		DeviceType:       "1",
		TenantID:         tenantID,
		ImageURL:         imageURL,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if fullData.DeviceConfig == nil {
		return newDC
	}
	config := fullData.DeviceConfig
	newDC.DeviceType = config.DeviceType
	if config.DeviceType == "" {
		newDC.DeviceType = "1"
	}
	if config.ProtocolType != "" {
		newDC.ProtocolType = &config.ProtocolType
	}
	if config.VoucherType != "" {
		newDC.VoucherType = &config.VoucherType
	}
	if config.ProtocolConfig != nil {
		if bytes, err := json.Marshal(config.ProtocolConfig); err == nil {
			s := string(bytes)
			newDC.ProtocolConfig = &s
		}
	}
	if config.DeviceConnType != "" {
		newDC.DeviceConnType = &config.DeviceConnType
	}
	if config.OtherConfig != nil {
		if bytes, err := json.Marshal(config.OtherConfig); err == nil {
			s := string(bytes)
			newDC.OtherConfig = &s
		}
	}
	if config.AdditionalInfo != nil {
		if bytes, err := json.Marshal(config.AdditionalInfo); err == nil {
			s := string(bytes)
			newDC.AdditionalInfo = &s
		}
	}
	newDC.AutoRegister = config.AutoRegister
	return newDC
}

func createMarketTemplateModels(tx *gorm.DB, tplDef *model.TemplateDefinitionPayload, templateID, tenantID string, now time.Time) error {
	if tplDef == nil || *tplDef == nil {
		return fmt.Errorf("invalid template_definition: expected an object")
	}
	parsed, err := parseMarketTemplateModelFields(tplDef)
	if err != nil {
		return err
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
		for _, fields := range parsed[rowType.key] {
			if err := tx.Create(rowType.new(fields)).Error; err != nil {
				return fmt.Errorf("Failed to create %s: %w", rowType.label, err)
			}
		}
	}
	return nil
}

// parseMarketTemplateModelFields validates the resource-center contract and normalizes
// only aliases already accepted by its public catalog implementation.
func parseMarketTemplateModelFields(tplDef *model.TemplateDefinitionPayload) (map[string][]map[string]interface{}, error) {
	parsed := make(map[string][]map[string]interface{}, 4)
	for _, kind := range []string{"telemetry", "attributes", "events", "commands"} {
		raw, exists := (*tplDef)[kind]
		if !exists || raw == nil {
			parsed[kind] = nil
			continue
		}
		entries, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid template_definition.%s: expected an array", kind)
		}
		fieldsList := make([]map[string]interface{}, 0, len(entries))
		for index, item := range entries {
			fields, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid template_definition.%s[%d]: expected an object", kind, index)
			}
			name, err := marketModelString(fields, kind, index, "data_name", "buttom_name", "name", "label")
			if err != nil {
				return nil, err
			}
			identifier, err := marketModelString(fields, kind, index, "data_identifier", "identifier", "key")
			if err != nil {
				return nil, err
			}
			if identifier == "" {
				return nil, fmt.Errorf("invalid template_definition.%s[%d]: missing data_identifier (or identifier/key)", kind, index)
			}
			if name == "" {
				name = identifier
			}
			canonical := map[string]interface{}{"data_name": name, "data_identifier": identifier}
			aliases := []struct {
				target string
				keys   []string
			}{
				{"data_type", []string{"data_type", "dataType", "type"}},
				{"read_write_flag", []string{"read_write_flag", "accessMode"}},
				{"unit", []string{"unit"}},
				{"description", []string{"description", "remark"}},
			}
			for _, alias := range aliases {
				value, err := marketModelString(fields, kind, index, alias.keys...)
				if err != nil {
					return nil, err
				}
				if value != "" {
					canonical[alias.target] = value
				}
			}
			for _, key := range []string{"additional_info", "params"} {
				value, exists := fields[key]
				if !exists || value == nil {
					continue
				}
				text, err := marketModelJSONText(value)
				if err != nil {
					return nil, fmt.Errorf("invalid template_definition.%s[%d].%s: %w", kind, index, key, err)
				}
				canonical[key] = text
			}
			fieldsList = append(fieldsList, canonical)
		}
		parsed[kind] = fieldsList
	}
	return parsed, nil
}

func marketModelJSONText(value interface{}) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("expected a string or JSON value: %w", err)
	}
	return string(encoded), nil
}

func marketModelString(fields map[string]interface{}, kind string, index int, keys ...string) (string, error) {
	for _, key := range keys {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("invalid template_definition.%s[%d].%s: expected a string", kind, index, key)
		}
		if text != "" {
			return text, nil
		}
	}
	return "", nil
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

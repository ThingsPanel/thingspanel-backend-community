package service

import (
	"context"
	"encoding/json"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
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
	if tplDef != nil {
		// 从 template_definition 中提取物模型
		if v, ok := (*tplDef)["telemetry"]; ok {
			if ts, ok := v.([]interface{}); ok {
				for _, t := range ts {
					if m, ok := t.(map[string]interface{}); ok {
						created := model.DeviceModelTelemetry{
							ID:               uuid.New(),
							DeviceTemplateID: templateID,
							TenantID:         claims.TenantID,
							DataName:         getStrP(m, "data_name"),
							DataIdentifier:   getStr(m, "data_identifier"),
							ReadWriteFlag:    getStrP(m, "read_write_flag"),
							DataType:         getStrP(m, "data_type"),
							Unit:             getStrP(m, "unit"),
							Description:      getStrP(m, "description"),
							AdditionalInfo:   getStrP(m, "additional_info"),
							CreatedAt:       now,
							UpdatedAt:       now,
						}
						if err := tx.Create(&created).Error; err != nil {
							tx.Rollback()
							return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
								"error": "Failed to create telemetry: " + err.Error(),
							})
						}
					}
				}
			}
		}

		// Attributes
		if v, ok := (*tplDef)["attributes"]; ok {
			if attrs, ok := v.([]interface{}); ok {
				for _, a := range attrs {
					if m, ok := a.(map[string]interface{}); ok {
						created := model.DeviceModelAttribute{
							ID:               uuid.New(),
							DeviceTemplateID: templateID,
							TenantID:         claims.TenantID,
							DataName:         getStrP(m, "data_name"),
							DataIdentifier:   getStr(m, "data_identifier"),
							ReadWriteFlag:    getStrP(m, "read_write_flag"),
							DataType:         getStrP(m, "data_type"),
							Unit:             getStrP(m, "unit"),
							Description:      getStrP(m, "description"),
							AdditionalInfo:   getStrP(m, "additional_info"),
							CreatedAt:       now,
							UpdatedAt:       now,
						}
						if err := tx.Create(&created).Error; err != nil {
							tx.Rollback()
							return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
								"error": "Failed to create attribute: " + err.Error(),
							})
						}
					}
				}
			}
		}

		// Events
		if v, ok := (*tplDef)["events"]; ok {
			if evts, ok := v.([]interface{}); ok {
				for _, e := range evts {
					if m, ok := e.(map[string]interface{}); ok {
						created := model.DeviceModelEvent{
							ID:               uuid.New(),
							DeviceTemplateID: templateID,
							TenantID:         claims.TenantID,
							DataName:         getStrP(m, "data_name"),
							DataIdentifier:   getStr(m, "data_identifier"),
							Param:           getStrP(m, "params"),
							Description:      getStrP(m, "description"),
							AdditionalInfo:   getStrP(m, "additional_info"),
							CreatedAt:       now,
							UpdatedAt:       now,
						}
						if err := tx.Create(&created).Error; err != nil {
							tx.Rollback()
							return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
								"error": "Failed to create event: " + err.Error(),
							})
						}
					}
				}
			}
		}

		// Commands
		if v, ok := (*tplDef)["commands"]; ok {
			if cmds, ok := v.([]interface{}); ok {
				for _, c := range cmds {
					if m, ok := c.(map[string]interface{}); ok {
						created := model.DeviceModelCommand{
							ID:               uuid.New(),
							DeviceTemplateID: templateID,
							TenantID:         claims.TenantID,
							DataName:         getStrP(m, "data_name"),
							DataIdentifier:   getStr(m, "data_identifier"),
							Param:           getStrP(m, "params"),
							Description:      getStrP(m, "description"),
							AdditionalInfo:   getStrP(m, "additional_info"),
							CreatedAt:       now,
							UpdatedAt:       now,
						}
						if err := tx.Create(&created).Error; err != nil {
							tx.Rollback()
							return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
								"error": "Failed to create command: " + err.Error(),
							})
						}
					}
				}
			}
		}
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
		DeviceType:       "1",        // 默认直连设备
		TenantID:         claims.TenantID,
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

	// 4. Notify market service of installation (async, non-blocking)
	marketUserID, err := client.ExtractUserIDFromMarketToken(req.MarketToken)
	if err != nil {
		logrus.Warnf("Could not extract market user_id from token, install notification may fail: %v", err)
		marketUserID = ""
	}
	if marketUserID == "" {
		marketUserID = claims.ID
	}
	versionID := ""
	if fullData.VersionID != "" {
		versionID = fullData.VersionID
	}
	logrus.Infof("Notifying market of installation: TemplateID=%s, VersionID=%s, MarketUserID=%s, OrgID=%s",
		req.MarketTemplateID, versionID, marketUserID, claims.TenantID)
	go func() {
		err := client.InstallTemplate(context.Background(), req.MarketToken, req.MarketTemplateID, versionID, marketUserID, claims.TenantID)
		if err != nil {
			logrus.Errorf("Failed to notify market service of installation: %v", err)
		} else {
			logrus.Infof("Successfully notified market service of installation for template %s", req.MarketTemplateID)
		}
	}()

	// 5. Fetch created records and return
	createdTpl, _ := dal.GetDeviceTemplateById(templateID)
	createdDC, _ := dal.GetDeviceConfigByID(dcID)

	return &model.InstallFromMarketRsp{
		DeviceTemplate: createdTpl,
		DeviceConfig:   createdDC,
		MissingPlugins: missingPlugins,
	}, nil
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

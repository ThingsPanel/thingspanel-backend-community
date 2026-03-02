package service

import (
	"context"
	"encoding/json"
	"fmt"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/sirupsen/logrus"
)

// ptrStr safely dereferences a *string, returning "" if nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// getPluginDependencies 从 device_configs 中提取模板关联的协议插件依赖
func getPluginDependencies(templateID string) []model.PluginDependency {
	configs, err := query.DeviceConfig.
		Where(query.DeviceConfig.DeviceTemplateID.Eq(templateID)).
		Find()
	if err != nil {
		logrus.Warnf("getPluginDependencies query error: %v", err)
		return []model.PluginDependency{}
	}

	// 用 map 去重 (按 protocol_type)
	seen := make(map[string]bool)
	var deps []model.PluginDependency
	for _, cfg := range configs {
		pt := ptrStr(cfg.ProtocolType)
		if pt == "" || seen[pt] {
			continue
		}
		seen[pt] = true
		deps = append(deps, model.PluginDependency{
			PluginName: pt,
			PluginType: "protocol",
			Required:   true,
		})
	}

	if deps == nil {
		return []model.PluginDependency{}
	}
	return deps
}

// PublishToMarket packages a device template and publishes it to the market
func (*DeviceTemplate) PublishToMarket(req model.PublishToMarketReq, claims *utils.UserClaims) (*model.MarketPublishApiResponse, error) {
	// 1. Get the template
	tpl, err := dal.GetDeviceTemplateById(req.DeviceTemplateID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": fmt.Sprintf("failed to find template: %s", err.Error()),
		})
	}

	// 2. Build template definition from WebChartConfig
	tplDef := make(map[string]interface{})
	if tpl.WebChartConfig != nil && *tpl.WebChartConfig != "" {
		_ = json.Unmarshal([]byte(*tpl.WebChartConfig), &tplDef)
	}

	// 3. Build device model (telemetry, attributes, commands, events)
	deviceModel := map[string]interface{}{
		"telemetry":  []interface{}{},
		"attributes": []interface{}{},
		"commands":   []interface{}{},
		"events":     []interface{}{},
	}

	if ts, err := dal.GetDeviceModelTelemetryDataList(tpl.ID); err == nil && ts != nil {
		deviceModel["telemetry"] = ts
	}
	if attrs, err := dal.GetDeviceModelAttributeDataList(tpl.ID); err == nil && attrs != nil {
		deviceModel["attributes"] = attrs
	}
	if evts, err := dal.GetDeviceModelEventDataList(tpl.ID); err == nil && evts != nil {
		deviceModel["events"] = evts
	}
	if cmds, err := dal.GetDeviceModelCommandDataList(tpl.ID); err == nil && cmds != nil {
		deviceModel["commands"] = cmds
	}

	// 4. Extract brand/model_number
	brand := ptrStr(tpl.Brand)
	devModel := ptrStr(tpl.ModelNumber)
	if brand == "" {
		brand = "DefaultBrand"
	}
	if devModel == "" {
		devModel = "DefaultModel"
	}

	// 5. Extract plugin dependencies from device_configs
	pluginDeps := getPluginDependencies(tpl.ID)

	// 6. Build publish request
	marketReq := &model.PublishTemplateReq{
		Name:               tpl.Name,
		Brand:              brand,
		Model:              devModel,
		Category:           "default",
		Author:             ptrStr(tpl.Author),
		Version:            ptrStr(tpl.Version),
		Description:        ptrStr(tpl.Description),
		TemplateDefinition: tplDef,
		DeviceModel:        deviceModel,
		PluginDependencies: pluginDeps,
	}

	// 7. Send to Market
	client := NewMarketClient()
	apiResp, err := client.PublishTemplate(context.Background(), req.MarketToken, marketReq)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Market service unreachable or request failed: " + err.Error(),
		})
	}

	// 8. Handle version conflict (code 4009)
	if apiResp.Code == 4009 {
		return apiResp, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error":   "Version conflict: this template version already exists in the market",
			"message": apiResp.Message,
		})
	}

	if apiResp.Code != 0 {
		return apiResp, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": apiResp.Message,
		})
	}

	return apiResp, nil
}

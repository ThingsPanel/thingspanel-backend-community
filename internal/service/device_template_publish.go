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

	"github.com/golang-jwt/jwt"
)

// ptrStr safely dereferences a *string, returning "" if nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ptrInt16 safely dereferences a *int16, returning 0 if nil.
func ptrInt16(i *int16) int16 {
	if i == nil {
		return 0
	}
	return *i
}

// parseJSON parses a JSON string into a map, returning nil on error.
func parseJSON(data string) map[string]interface{} {
	if data == "" {
		return nil
	}
	var result map[string]interface{}
	_ = json.Unmarshal([]byte(data), &result)
	return result
}

// PublishToMarket 以 device_config_id 为入口，发布 DeviceConfig（凭证协议）和 DeviceTemplate（物模型+面板）到市场
func (*DeviceTemplate) PublishToMarket(req model.PublishToMarketReq, claims *utils.UserClaims) (*model.MarketPublishApiResponse, error) {
	// 1. Get the DeviceConfig
	dc, err := dal.GetDeviceConfigByID(req.DeviceConfigID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": fmt.Sprintf("failed to find device config: %s", err.Error()),
		})
	}

	// 2. Get the DeviceTemplate (via device_template_id)
	if dc.DeviceTemplateID == nil || *dc.DeviceTemplateID == "" {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "device config has no associated device template",
		})
	}
	tplID := *dc.DeviceTemplateID
	tpl, err := dal.GetDeviceTemplateById(tplID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": fmt.Sprintf("failed to find device template: %s", err.Error()),
		})
	}

	// 3. Build DeviceConfig payload (凭证协议配置)
	deviceConfig := &model.DeviceConfigPayload{
		Name:           dc.Name,
		DeviceType:     dc.DeviceType,
		ProtocolType:   ptrStr(dc.ProtocolType),
		VoucherType:    ptrStr(dc.VoucherType),
		ProtocolConfig: parseJSON(ptrStr(dc.ProtocolConfig)),
		DeviceConnType: ptrStr(dc.DeviceConnType),
		OtherConfig:    parseJSON(ptrStr(dc.OtherConfig)),
		AdditionalInfo: parseJSON(ptrStr(dc.AdditionalInfo)),
		AutoRegister:   dc.AutoRegister,
	}

	// 4. Build TemplateDefinition payload (面板配置)
	tplDef := map[string]interface{}{
		"web_chart_config": parseJSON(ptrStr(tpl.WebChartConfig)),
		"app_chart_config": parseJSON(ptrStr(tpl.AppChartConfig)),
	}

	// 5. Build device model (telemetry, attributes, commands, events)
	deviceModel := map[string]interface{}{
		"telemetry":  []interface{}{},
		"attributes": []interface{}{},
		"commands":   []interface{}{},
		"events":     []interface{}{},
	}

	if ts, err := dal.GetDeviceModelTelemetryDataList(tplID); err == nil && ts != nil {
		deviceModel["telemetry"] = ts
	}
	if attrs, err := dal.GetDeviceModelAttributeDataList(tplID); err == nil && attrs != nil {
		deviceModel["attributes"] = attrs
	}
	if evts, err := dal.GetDeviceModelEventDataList(tplID); err == nil && evts != nil {
		deviceModel["events"] = evts
	}
	if cmds, err := dal.GetDeviceModelCommandDataList(tplID); err == nil && cmds != nil {
		deviceModel["commands"] = cmds
	}

	// 6. Extract metadata (priority: Request > Template/Config > Default)
	name := req.MarketName
	if name == "" {
		name = tpl.Name
	}
	brand := req.Brand
	if brand == "" {
		brand = ptrStr(tpl.Brand)
	}
	if brand == "" {
		brand = "DefaultBrand"
	}
	devModel := req.Model
	if devModel == "" {
		devModel = ptrStr(tpl.ModelNumber)
	}
	if devModel == "" {
		devModel = "DefaultModel"
	}
	category := req.Category
	if category == "" {
		category = "default"
	}
	version := req.Version
	if version == "" {
		version = ptrStr(tpl.Version)
	}
	author := req.Author
	if author == "" {
		author = ptrStr(tpl.Author)
	}
	description := req.Description
	if description == "" {
		description = ptrStr(tpl.Description)
	}

	// 7. Extract plugin dependencies from DeviceConfig.protocol_type
	pluginDeps := getPluginDependenciesFromProtocol(dc)

	// 8. Build publish request to market
	marketReq := &model.PublishTemplateReq{
		Name:               name,
		Brand:              brand,
		Model:              devModel,
		Category:           category,
		Author:             author,
		Version:            version,
		Description:        description,
		DeviceConfig:       deviceConfig,
		TemplateDefinition: map[string]interface{}{
			"web_chart_config": tplDef["web_chart_config"],
			"app_chart_config": tplDef["app_chart_config"],
			"telemetry":        deviceModel["telemetry"],
			"attributes":       deviceModel["attributes"],
			"commands":         deviceModel["commands"],
			"events":           deviceModel["events"],
		},
		PluginDependencies: pluginDeps,
	}

	// 9. Parse MarketToken to get UserID (sub claim)
	marketUserID := ""
	token, _, _ := new(jwt.Parser).ParseUnverified(req.MarketToken, jwt.MapClaims{})
	if token != nil {
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if sub, ok := claims["sub"].(string); ok {
				marketUserID = sub
			}
		}
	}

	// 10. Send to Market
	client := NewMarketClient()
	apiResp, err := client.PublishTemplate(context.Background(), req.MarketToken, marketUserID, marketReq)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Market service unreachable or request failed: " + err.Error(),
		})
	}

	// 11. Handle version conflict (code 4009)
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

// getPluginDependenciesFromProtocol 从 DeviceConfig 的 protocol_type 提取插件依赖
func getPluginDependenciesFromProtocol(dc *model.DeviceConfig) []model.PluginDependency {
	pt := ptrStr(dc.ProtocolType)
	if pt == "" {
		return []model.PluginDependency{}
	}

	// 查询插件真实版本
	version := ""
	pluginMsg, _ := query.ServicePlugin.WithContext(context.Background()).
		Where(query.ServicePlugin.ServiceIdentifier.Eq(pt)).
		First()
	if pluginMsg != nil && pluginMsg.Version != nil {
		version = *pluginMsg.Version
	}

	return []model.PluginDependency{
		{
			PluginName: pt,
			PluginType: "protocol",
			MinVersion: version,
			Required:   true,
		},
	}
}

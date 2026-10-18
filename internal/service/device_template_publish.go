package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/utils"
)

// ptrStr safely dereferences a *string, returning "" if nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// parseJSON parses a JSON string into a map, returning nil on error.
func parseJSON(data string) map[string]interface{} {
	if data == "" {
		return nil
	}
	var result map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	_ = decoder.Decode(&result)
	return result
}

// PublishToMarket 以 device_config_id 为入口，发布 DeviceConfig（凭证协议）和 DeviceTemplate（物模型+面板）到市场
func (*DeviceTemplate) PublishToMarket(ctx context.Context, req model.PublishToMarketReq, claims *utils.UserClaims, communityAuthorization, publicOrigin string) (*model.MarketPublishApiResponse, error) {
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" {
		return nil, errcode.WithData(errcode.CodeParamError, "authenticated tenant is required")
	}
	dc, err := query.DeviceConfig.WithContext(ctx).Where(query.DeviceConfig.ID.Eq(req.DeviceConfigID), query.DeviceConfig.TenantID.Eq(claims.TenantID)).First()
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": fmt.Sprintf("failed to find device config: %s", err.Error()),
		})
	}
	if dc.NativeThingModelID != nil && strings.TrimSpace(*dc.NativeThingModelID) != "" {
		return publishNativeDeviceConfig(ctx, req, dc, communityAuthorization, publicOrigin)
	}
	return nil, errcode.WithData(errcode.CodeParamError, "unsupported device config: native_thing_model_binding is required for market publication")
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

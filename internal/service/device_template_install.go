package service

import (
	"context"
	"encoding/json"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
)

// InstallFromMarket downloads a template from the market and creates it locally:
// 1. DeviceTemplate (物模型 + 面板配置)
// 2. DeviceConfig (凭证协议配置，引用新创建的 DeviceTemplate)
func (*DeviceTemplate) InstallFromMarket(ctx context.Context, req model.InstallFromMarketReq, claims *utils.UserClaims, communityAuthorization string) (*model.InstallFromMarketRsp, error) {
	// 1. Download the full template definition from market
	client := NewMarketClient()
	fullData, err := client.DownloadTemplate(ctx, req.MarketToken, req.MarketTemplateID, req.Version)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to download template from market: " + err.Error(),
		})
	}

	definition, err := decodeMarketTemplateDefinition(fullData.TemplateDefinition)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, "market template definition is invalid")
	}
	if nativeRaw, ok := definition["native_thing_model"]; ok {
		_ = nativeRaw
		dependency, err := decodeNativeMarketDependency(definition)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeParamError, err.Error())
		}
		if err := verifyMarketNativeDependency(dependency); err != nil {
			return nil, errcode.WithData(errcode.CodeParamError, err.Error())
		}
		if claims == nil || claims.TenantID == "" || req.Version == "" || fullData.Version == "" || req.Version != fullData.Version {
			return nil, errcode.WithData(errcode.CodeParamError, "market download did not return the requested immutable version")
		}
		stableAssetName := marketNativeAssetName(claims.TenantID, req.MarketTemplateID, fullData.Version, dependency.DependencyChecksum)
		localImageURL, err := localizeMarketTemplateImageStable(ctx, client, fullData, stableAssetName)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeSystemError, "Failed to localize market template cover: "+err.Error())
		}
		result, _, err := installNativeMarketDeviceConfig(ctx, client, req, fullData, dependency, localImageURL, claims, communityAuthorization)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	return nil, errcode.WithData(errcode.CodeParamError, "unsupported market package: native_thing_model contract_version=1 is required; legacy package conversion is not lossless")
}

func decodeMarketTemplateDefinition(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
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

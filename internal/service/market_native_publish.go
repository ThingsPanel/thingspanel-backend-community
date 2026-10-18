package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"project/internal/model"
	"project/pkg/errcode"

	"github.com/golang-jwt/jwt"
)

func publishNativeDeviceConfig(ctx context.Context, req model.PublishToMarketReq, dc *model.DeviceConfig, communityAuthorization, publicOrigin string) (*model.MarketPublishApiResponse, error) {
	if dc.NativeThingModelSnapshotID == nil || dc.NativeThingModelRevision == nil || dc.DeviceTemplateID != nil {
		return nil, errcode.WithData(errcode.CodeParamError, "device config native binding is incomplete or mixed with a legacy template")
	}
	nativeClient, err := newNativeMarketImportClient()
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, err.Error())
	}
	snapshot, err := nativeClient.readSnapshot(ctx, communityAuthorization, *dc.NativeThingModelID, *dc.NativeThingModelRevision)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "failed to read bound native model snapshot: "+err.Error())
	}
	if snapshot.SnapshotID != *dc.NativeThingModelSnapshotID {
		return nil, errcode.WithData(errcode.CodeParamError, "native snapshot does not match the device config binding")
	}
	dependency, err := buildMarketNativeThingModelDependency(snapshot)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, err.Error())
	}
	resources, err := dependencyResourceObject(dependency.Resources)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, "native model resources are invalid")
	}
	cover := req.CoverURL
	if cover == "" && dc.ImageURL != nil {
		cover = *dc.ImageURL
	}
	profileCover := rawString(resources["profile"], "cover")
	if profileCover != "" {
		profileCover = resolveMarketSourceAsset(profileCover, publicOrigin)
		if cover != "" && resolveMarketSourceAsset(cover, publicOrigin) != profileCover {
			return nil, errcode.WithData(errcode.CodeParamError, "device config cover and native profile cover differ; market package supports one cover asset")
		}
		cover = profileCover
		profile, err := rawObject(resources["profile"])
		if err != nil {
			return nil, errcode.WithData(errcode.CodeParamError, "native profile is invalid")
		}
		profile["cover"] = json.RawMessage(`"asset://native-profile-cover"`)
		resources["profile"], err = json.Marshal(profile)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeSystemError, "encode native profile")
		}
		dependency.Resources, err = json.Marshal(resources)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeSystemError, "encode native resources")
		}
		dependency.Assets = []model.MarketNativeThingModelAsset{{Key: "native-profile-cover", Source: "market-cover"}}
		dependency.DependencyChecksum, err = marketNativeDependencyHash(dependency.Model, dependency.Resources, dependency.Assets)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeSystemError, "checksum native dependency")
		}
	} else if cover != "" {
		return nil, errcode.WithData(errcode.CodeParamError, "device config cover cannot substitute for an empty native profile cover")
	}
	cover = resolveMarketSourceAsset(cover, publicOrigin)
	if err := validateMarketPortableResources(dependency.Resources); err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, err.Error())
	}
	if err := verifyMarketNativeDependency(dependency); err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, err.Error())
	}
	brand := req.Brand
	if brand == "" {
		brand = rawString(resources["profile"], "brand")
	}
	modelName := req.Model
	if modelName == "" {
		modelName = rawString(resources["profile"], "modelNumber")
	}
	version := req.Version
	if version == "" {
		version = rawString(resources["profile"], "version")
	}
	if version == "" {
		version = fmt.Sprintf("%d", snapshot.PublishedRevision)
	}
	name := req.MarketName
	if name == "" {
		name = dc.Name
	}
	if name == "" {
		name = rawString(dependency.Model, "name")
	}
	description := req.Description
	if description == "" {
		description = rawString(dependency.Model, "description")
	}
	if description == "" {
		description = rawNestedString(dependency.Model, "descriptionI18n", "default")
	}
	category := req.Category
	if category == "" {
		category = rawString(dependency.Model, "category")
	}
	if category == "" {
		category = rawString(resources["profile"], "category")
	}
	if category == "" {
		category = "default"
	}
	author := req.Author
	if author == "" {
		author = rawString(resources["profile"], "author")
	}
	if brand == "" {
		brand = "DefaultBrand"
	}
	if modelName == "" {
		modelName = "DefaultModel"
	}
	depRaw, err := json.Marshal(dependency)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "encode native dependency")
	}
	deviceConfig := &model.DeviceConfigPayload{
		Name: dc.Name, DeviceType: dc.DeviceType, ProtocolType: ptrStr(dc.ProtocolType), VoucherType: ptrStr(dc.VoucherType),
		ProtocolConfig: scrubMarketSecrets(parseJSON(ptrStr(dc.ProtocolConfig))), DeviceConnType: ptrStr(dc.DeviceConnType),
		OtherConfig: scrubMarketSecrets(parseJSON(ptrStr(dc.OtherConfig))), AdditionalInfo: scrubMarketSecrets(parseJSON(ptrStr(dc.AdditionalInfo))),
		AutoRegister: dc.AutoRegister, ImageURL: cover,
	}
	marketReq := &model.PublishTemplateReq{
		SourceTemplateID: dc.ID, Name: name, Brand: brand, Model: modelName, Category: category,
		Author: author, Version: version, Description: description, CoverURL: cover, DeviceConfig: deviceConfig,
		TemplateDefinition: map[string]interface{}{"native_thing_model": string(depRaw), "native_thing_model_encoding": "raw-json-string-v1"}, PluginDependencies: getPluginDependenciesFromProtocol(dc),
	}
	marketUserID := marketSubject(req.MarketToken)
	apiResp, err := NewMarketClient().PublishTemplate(ctx, req.MarketToken, marketUserID, marketReq)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "Market service unreachable or request failed: "+err.Error())
	}
	if apiResp.Code != 0 {
		return apiResp, errcode.WithData(errcode.CodeSystemError, apiResp.Message)
	}
	return apiResp, nil
}

func rawNestedString(raw json.RawMessage, keys ...string) string {
	current := raw
	for i, key := range keys {
		obj, err := rawObject(current)
		if err != nil {
			return ""
		}
		current = obj[key]
		if i == len(keys)-1 {
			var value string
			_ = json.Unmarshal(current, &value)
			return value
		}
	}
	return ""
}

func marketSubject(token string) string {
	parsed, _, _ := new(jwt.Parser).ParseUnverified(token, jwt.MapClaims{})
	if parsed == nil {
		return ""
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

func resolveMarketSourceAsset(value, publicOrigin string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return value
	}
	if publicOrigin == "" {
		return value
	}
	return strings.TrimRight(publicOrigin, "/") + "/" + strings.TrimLeft(strings.TrimPrefix(value, "./"), "/")
}

func dependencyResourceObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var resources map[string]json.RawMessage
	if err := json.Unmarshal(raw, &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func rawString(raw json.RawMessage, key string) string {
	obj, err := rawObject(raw)
	if err != nil {
		return ""
	}
	var value string
	_ = json.Unmarshal(obj[key], &value)
	return value
}

func validateMarketPortableResources(raw json.RawMessage) error {
	resources, err := dependencyResourceObject(raw)
	if err != nil {
		return err
	}
	for _, key := range []string{"webChartConfig", "appChartConfig"} {
		value := bytes.TrimSpace(resources[key])
		if len(value) == 0 || bytes.Equal(value, []byte("null")) {
			continue
		}
		if err := rejectMarketExternalAssetJSON(value); err != nil {
			return fmt.Errorf("native %s: %w", key, err)
		}
	}
	return nil
}

func rejectMarketExternalAssetJSON(raw json.RawMessage) error {
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var walk func(interface{}) error
	walk = func(node interface{}) error {
		switch item := node.(type) {
		case map[string]interface{}:
			for key, child := range item {
				lower := strings.ToLower(key)
				if strings.Contains(lower, "url") || strings.Contains(lower, "image") || lower == "src" {
					if value, ok := child.(string); ok && (strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")) {
						return fmt.Errorf("contains an external asset reference that is not in the market asset manifest")
					}
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range item {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}

func scrubMarketSecrets(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
		if strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || strings.Contains(normalized, "credential") || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "accesskey") || strings.Contains(normalized, "privatekey") || normalized == "tenantid" || normalized == "userid" || normalized == "deviceid" {
			continue
		}
		result[key] = scrubMarketValue(value)
	}
	return result
}

func scrubMarketValue(value interface{}) interface{} {
	switch item := value.(type) {
	case map[string]interface{}:
		return scrubMarketSecrets(item)
	case []interface{}:
		result := make([]interface{}, len(item))
		for i, child := range item {
			result[i] = scrubMarketValue(child)
		}
		return result
	default:
		return value
	}
}

func dependencyHasSensitiveKeys(raw json.RawMessage) bool {
	return rejectNonPortableIdentityKeys(raw) != nil
}

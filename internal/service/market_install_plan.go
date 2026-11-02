package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/utils"
)

type marketInstallNativePayload struct {
	MarketTemplateID string                                 `json:"marketTemplateId"`
	Version          string                                 `json:"version"`
	Dependency       model.MarketNativeThingModelDependency `json:"dependency"`
	Model            json.RawMessage                        `json:"model"`
	Resources        json.RawMessage                        `json:"resources"`
	Config           *model.DeviceConfigPayload             `json:"config,omitempty"`
	Name             string                                 `json:"name"`
	CoverURL         *string                                `json:"coverUrl,omitempty"`
	Reuse            *nativeMarketSnapshotResponse          `json:"reuse,omitempty"`
}
type marketInstallDashboardPayload struct {
	Dashboard      model.DashboardTemplate             `json:"dashboard"`
	DeviceBindings []model.DeviceBindingInput          `json:"deviceBindings"`
	DeviceProofs   map[string]marketInstallDeviceProof `json:"deviceProofs"`
}
type marketInstallDeviceProof struct {
	DeviceID        string `json:"deviceId"`
	ConfigID        string `json:"configId"`
	BindingRevision int    `json:"bindingRevision"`
	ModelID         string `json:"modelId"`
	SnapshotID      string `json:"snapshotId"`
	Revision        int    `json:"revision"`
}

// prepareMarketInstallPlan consumes v2 native references without translating
// legacy thingModel arrays. It downloads only during the user's live request.
func prepareMarketInstallPlan(ctx context.Context, req *model.InstallBundleRequest, claims *utils.UserClaims, authorization string) (model.MarketInstallPlan, error) {
	if err := supportedInstallPolicy(req.OverwritePolicy); err != nil {
		return model.MarketInstallPlan{}, err
	}
	service := NewMarketBundleInstallService()
	bundle, err := service.downloadBundleFromHorizon(ctx, req.MarketToken, req.BundleKey, req.Version)
	if err != nil {
		return model.MarketInstallPlan{}, err
	}
	var security model.BundleSecurity
	if err := json.Unmarshal(bundle.Security, &security); err != nil {
		return model.MarketInstallPlan{}, err
	}
	if security.ContainsSecrets || security.ContainsRuntimeData {
		return model.MarketInstallPlan{}, fmt.Errorf("bundle contains private runtime data")
	}
	if security.ContentHash != "" && security.ContentHash != bundle.ContentHash {
		return model.MarketInstallPlan{}, fmt.Errorf("bundle content hash mismatch")
	}
	var resources struct {
		DeviceTemplates json.RawMessage `json:"deviceTemplates"`
		NativeTemplates []struct {
			ResourceKey        string `json:"resourceKey"`
			MarketTemplateID   string `json:"marketTemplateId"`
			Version            string `json:"version"`
			DependencyChecksum string `json:"dependencyChecksum"`
		} `json:"nativeTemplates"`
		Dashboards []model.DashboardTemplate `json:"dashboards"`
	}
	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return model.MarketInstallPlan{}, err
	}
	switch bundle.ContractVersion {
	case "1.0":
		var legacy []json.RawMessage
		if len(resources.DeviceTemplates) > 0 {
			if err := json.Unmarshal(resources.DeviceTemplates, &legacy); err != nil {
				return model.MarketInstallPlan{}, err
			}
		}
		if len(legacy) > 0 || len(resources.NativeTemplates) > 0 {
			return model.MarketInstallPlan{}, fmt.Errorf("NATIVE_DEPENDENCY_REQUIRED: legacy templates require explicit native v2 bundle references")
		}
	case "2.0":
		if len(resources.DeviceTemplates) > 0 {
			return model.MarketInstallPlan{}, fmt.Errorf("v2 bundle cannot contain legacy deviceTemplates")
		}
	default:
		return model.MarketInstallPlan{}, fmt.Errorf("unsupported bundle contract version")
	}
	plan := model.MarketInstallPlan{ArtifactDigest: bundle.ContentHash, Steps: []model.MarketInstallStep{}}
	nativeByKey := map[string]marketInstallNativePayload{}
	client := NewMarketClient()
	nativeClient, err := newNativeMarketImportClient()
	if err != nil && len(resources.NativeTemplates) > 0 {
		return plan, err
	}
	for _, reference := range resources.NativeTemplates {
		if reference.ResourceKey == "" || len(reference.ResourceKey) > 128 || strings.TrimSpace(reference.ResourceKey) != reference.ResourceKey || reference.MarketTemplateID == "" || reference.Version == "" || len(reference.DependencyChecksum) != 64 {
			return plan, fmt.Errorf("invalid native template reference")
		}
		if _, err := hex.DecodeString(reference.DependencyChecksum); err != nil {
			return plan, fmt.Errorf("invalid native dependency checksum")
		}
		if _, exists := nativeByKey[reference.ResourceKey]; exists {
			return plan, fmt.Errorf("duplicate native resource key")
		}
		fullData, err := client.DownloadTemplate(ctx, req.MarketToken, reference.MarketTemplateID, reference.Version)
		if err != nil {
			return plan, err
		}
		if fullData.Version != reference.Version {
			return plan, fmt.Errorf("native market version mismatch")
		}
		definition, err := decodeMarketTemplateDefinition(fullData.TemplateDefinition)
		if err != nil {
			return plan, err
		}
		dependency, err := decodeNativeMarketDependency(definition)
		if err != nil {
			return plan, err
		}
		if err := verifyMarketNativeDependency(dependency); err != nil {
			return plan, err
		}
		if dependency.DependencyChecksum != reference.DependencyChecksum {
			return plan, fmt.Errorf("native reference dependency checksum mismatch")
		}
		stableAssetName := marketNativeAssetName(claims.TenantID, reference.MarketTemplateID, reference.Version, dependency.DependencyChecksum)
		coverURL, err := localizeMarketTemplateImageStable(ctx, client, fullData, stableAssetName)
		if err != nil {
			return plan, err
		}
		targetResources, err := resolveMarketNativeDependencyResources(dependency, coverURL)
		if err != nil {
			return plan, err
		}
		if err := validateMarketPortableResources(targetResources); err != nil {
			return plan, err
		}
		// Keep only the already-public protocol defaults in the durable plan.
		var config *model.DeviceConfigPayload
		if fullData.DeviceConfig != nil {
			sanitized := *fullData.DeviceConfig
			sanitized.ProtocolConfig = scrubMarketSecrets(sanitized.ProtocolConfig)
			sanitized.OtherConfig = scrubMarketSecrets(sanitized.OtherConfig)
			sanitized.AdditionalInfo = scrubMarketSecrets(sanitized.AdditionalInfo)
			config = &sanitized
		}
		payload := marketInstallNativePayload{MarketTemplateID: reference.MarketTemplateID, Version: reference.Version, Dependency: *dependency, Model: dependency.Model, Resources: targetResources, Config: config, Name: fullData.Name, CoverURL: coverURL}
		// Same-tenant source reuse is legal only after a complete immutable readback.
		source, readErr := nativeClient.readSnapshot(ctx, authorization, dependency.SourceModelID, dependency.SourceRevision)
		if readErr == nil {
			if err := verifyMarketSourceSnapshotContent(dependency, source); err != nil {
				return plan, err
			}
			payload.Reuse = source
		} else {
			var requestError *nativeMarketHTTPError
			if !errors.As(readErr, &requestError) || requestError.StatusCode != 404 {
				return plan, readErr
			}
		}
		nativeByKey[reference.ResourceKey] = payload
		raw, err := json.Marshal(payload)
		if err != nil {
			return plan, err
		}
		actions := []string{"create", "publish", "readback", "bind"}
		if payload.Reuse != nil {
			actions = []string{"readback", "bind"}
		}
		for _, action := range actions {
			owner := "thingmodel"
			if action == "bind" {
				owner = "community-config"
			}
			plan.Steps = append(plan.Steps, model.MarketInstallStep{ID: reference.ResourceKey + ":" + action, Owner: owner, Action: action, ResourceKey: reference.ResourceKey, Payload: raw})
		}
	}
	provided := map[string]string{}
	for _, binding := range req.DeviceBindings {
		if binding.BindingKey == "" || binding.LocalDeviceID == "" || provided[binding.BindingKey] != "" {
			return plan, fmt.Errorf("invalid or duplicate device binding")
		}
		provided[binding.BindingKey] = binding.LocalDeviceID
	}
	expected := map[string]bool{}
	dashKeys := map[string]bool{}
	for _, dashboard := range resources.Dashboards {
		if dashboard.ResourceKey == "" || len(dashboard.ResourceKey) > 128 || dashKeys[dashboard.ResourceKey] {
			return plan, fmt.Errorf("invalid or duplicate dashboard resource key")
		}
		dashKeys[dashboard.ResourceKey] = true
		bindings := []model.DeviceBindingInput{}
		proofs := map[string]marketInstallDeviceProof{}
		for _, binding := range dashboard.DeviceBindings {
			native, exists := nativeByKey[binding.DeviceTemplateKey]
			if !exists {
				return plan, fmt.Errorf("dashboard binding references an unavailable native template")
			}
			localID := provided[binding.BindingKey]
			if localID == "" {
				return plan, fmt.Errorf("dashboard binding must select a real tenant device")
			}
			expected[binding.BindingKey] = true
			device, err := dal.GetDeviceByID(localID)
			if err != nil || device == nil || device.TenantID != claims.TenantID || device.DeviceConfigID == nil {
				return plan, fmt.Errorf("dashboard binding device is unavailable")
			}
			config, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
			if err != nil || config == nil || config.TenantID != claims.TenantID || config.NativeThingModelID == nil || config.NativeThingModelRevision == nil || config.NativeThingModelSnapshotID == nil {
				return plan, fmt.Errorf("dashboard binding lacks a native configuration")
			}
			snapshot, err := nativeClient.readSnapshot(ctx, authorization, *config.NativeThingModelID, *config.NativeThingModelRevision)
			if err != nil {
				return plan, err
			}
			if err := verifyImportedNativeSnapshotContent(native.Model, native.Resources, snapshot); err != nil {
				return plan, fmt.Errorf("dashboard binding native snapshot does not match its dependency")
			}
			if snapshot.SnapshotID != *config.NativeThingModelSnapshotID {
				return plan, fmt.Errorf("device fixed snapshot does not match owner readback")
			}
			proofs[binding.BindingKey] = marketInstallDeviceProof{DeviceID: localID, ConfigID: config.ID, BindingRevision: config.NativeBindingRevision, ModelID: *config.NativeThingModelID, SnapshotID: *config.NativeThingModelSnapshotID, Revision: *config.NativeThingModelRevision}
			bindings = append(bindings, model.DeviceBindingInput{BindingKey: binding.BindingKey, LocalDeviceID: localID})
		}
		if err := validateMarketInstallFieldBindings(dashboard, nativeByKey); err != nil {
			return plan, err
		}
		payload, err := json.Marshal(marketInstallDashboardPayload{Dashboard: dashboard, DeviceBindings: bindings, DeviceProofs: proofs})
		if err != nil {
			return plan, err
		}
		plan.Steps = append(plan.Steps, model.MarketInstallStep{ID: dashboard.ResourceKey + ":import", Owner: "thingsvis", Action: "import", ResourceKey: dashboard.ResourceKey, Payload: payload})
	}
	for key := range provided {
		if !expected[key] {
			return plan, fmt.Errorf("unknown device binding")
		}
	}
	if len(plan.Steps) == 0 || len(plan.Steps) > 256 {
		return plan, fmt.Errorf("bundle has no supported executable resources")
	}
	return plan, nil
}
func validateMarketInstallFieldBindings(dashboard model.DashboardTemplate, nativeByKey map[string]marketInstallNativePayload) error {
	bindings := map[string]string{}
	for _, binding := range dashboard.DeviceBindings {
		bindings[binding.BindingKey] = binding.DeviceTemplateKey
	}
	for _, field := range dashboard.FieldBindings {
		payload, exists := nativeByKey[bindings[field.BindingKey]]
		if !exists {
			return fmt.Errorf("field binding references an unknown native role")
		}
		var native struct {
			Fields []struct {
				Kind       string `json:"kind"`
				Identifier string `json:"identifier"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(payload.Model, &native); err != nil {
			return err
		}
		found := false
		for _, candidate := range native.Fields {
			if strings.EqualFold(candidate.Kind, string(field.Kind)) && candidate.Identifier == field.Identifier {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("field binding does not exist in the complete native model")
		}
	}
	return nil
}

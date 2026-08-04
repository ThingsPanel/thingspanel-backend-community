package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/utils"
)

// MarketDashboardBundleService coordinates the one-dashboard Bundle workflow.
type MarketDashboardBundleService struct {
	thingsvis *ThingsVisClient
	market    *MarketClient
}

// NewMarketDashboardBundleService creates the dashboard Bundle coordinator.
func NewMarketDashboardBundleService(thingsVisBaseURL string) *MarketDashboardBundleService {
	return &MarketDashboardBundleService{
		thingsvis: NewThingsVisClientWithBaseURL(thingsVisBaseURL),
		market:    NewMarketClient(),
	}
}

// Analyze discovers dashboard device references and enriches them from local device models.
func (s *MarketDashboardBundleService) Analyze(ctx context.Context, dashboardID, thingsVisAuthorization string, claims *utils.UserClaims) (*model.AnalyzeDashboardBundleResponse, error) {
	analyzed, err := s.thingsvis.AnalyzeMarketDashboard(ctx, dashboardID, claims.TenantID, claims.ID, thingsVisAuthorization)
	if err != nil {
		code := errcode.CodeSystemError
		if errors.Is(err, ErrThingsVisRequestRejected) {
			code = errcode.CodeParamError
		}
		return nil, errcode.WithData(code, map[string]interface{}{
			"error":  "failed to analyze ThingsVis dashboard",
			"detail": err.Error(),
		})
	}
	result := &model.AnalyzeDashboardBundleResponse{
		DashboardID:      analyzed.Dashboard.ID,
		DashboardName:    analyzed.Dashboard.Name,
		DeviceReferences: make([]model.DashboardBundleDeviceReference, 0, len(analyzed.DeviceReferences)),
	}
	for _, reference := range analyzed.DeviceReferences {
		device, err := query.Device.WithContext(ctx).Where(
			query.Device.ID.Eq(reference.SourceDeviceID),
			query.Device.TenantID.Eq(claims.TenantID),
		).First()
		if err != nil || device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
			return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error":    "dashboard references an unavailable or unconfigured device",
				"deviceId": reference.SourceDeviceID,
			})
		}
		config, err := query.DeviceConfig.WithContext(ctx).Where(
			query.DeviceConfig.ID.Eq(*device.DeviceConfigID),
			query.DeviceConfig.TenantID.Eq(claims.TenantID),
		).First()
		if err != nil || config.DeviceTemplateID == nil || *config.DeviceTemplateID == "" {
			return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error":    "dashboard device has no device template",
				"deviceId": reference.SourceDeviceID,
			})
		}
		fields, err := readRequiredThingModelFields(*config.DeviceTemplateID, reference.FieldIdentifiers)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error":    err.Error(),
				"deviceId": reference.SourceDeviceID,
			})
		}
		deviceName := reference.SourceDeviceName
		if device.Name != nil && strings.TrimSpace(*device.Name) != "" {
			deviceName = *device.Name
		}
		deviceName = portableDeviceDisplayName(deviceName, reference.SourceDeviceID)
		result.DeviceReferences = append(result.DeviceReferences, model.DashboardBundleDeviceReference{
			SourceDeviceID:      reference.SourceDeviceID,
			SourceDeviceName:    deviceName,
			DeviceTemplateID:    *config.DeviceTemplateID,
			SuggestedBindingKey: suggestBindingKey(deviceName, reference.SourceDeviceID),
			RequiredFields:      fields,
		})
	}
	return result, nil
}

// Publish exports one dashboard, attaches its exact dependencies, and submits it for review.
func (s *MarketDashboardBundleService) Publish(ctx context.Context, req *model.PublishDashboardBundleRequest, thingsVisAuthorization string, claims *utils.UserClaims) (*model.PublishDraftResponse, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`).MatchString(req.BundleKey) {
		return nil, errcode.WithData(errcode.CodeParamError, "invalid bundleKey")
	}
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`).MatchString(req.Version) {
		return nil, errcode.WithData(errcode.CodeParamError, "invalid semantic version")
	}
	req.DeviceRoles = normalizeDashboardBundleRoles(req.DeviceRoles)
	roles, err := validateDashboardBundleRoles(req.DeviceRoles)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, err.Error())
	}

	analyzed, err := s.Analyze(ctx, req.DashboardID, thingsVisAuthorization, claims)
	if err != nil {
		return nil, err
	}
	referenceByDevice := make(map[string]model.DashboardBundleDeviceReference)
	templateIDs := make([]string, 0)
	templateSeen := make(map[string]bool)
	for _, reference := range analyzed.DeviceReferences {
		referenceByDevice[reference.SourceDeviceID] = reference
		if !templateSeen[reference.DeviceTemplateID] {
			templateSeen[reference.DeviceTemplateID] = true
			templateIDs = append(templateIDs, reference.DeviceTemplateID)
		}
	}
	if len(roles) != len(referenceByDevice) {
		return nil, errcode.WithData(errcode.CodeParamError, "deviceRoles must cover every dashboard device exactly once")
	}
	for sourceDeviceID := range roles {
		if _, exists := referenceByDevice[sourceDeviceID]; !exists {
			return nil, errcode.WithData(errcode.CodeParamError, "deviceRoles contains a device not referenced by the dashboard")
		}
	}

	publisher := NewMarketBundlePublish()
	templates, err := publisher.validateDeviceTemplatesOwnership(ctx, templateIDs, claims.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, err.Error())
	}
	thingModels, failures := publisher.readThingModels(ctx, templates)
	if len(failures) > 0 {
		return nil, errcode.WithData(errcode.CodeParamError, failures)
	}
	templateKeyByID := buildDeviceTemplateResourceKeys(templates)

	thingsvisRoles := make([]ThingsVisDeviceRole, 0, len(req.DeviceRoles))
	for _, role := range req.DeviceRoles {
		thingsvisRoles = append(thingsvisRoles, ThingsVisDeviceRole{
			SourceDeviceID: role.SourceDeviceID,
			BindingKey:     role.BindingKey,
			DisplayName:    role.DisplayName,
		})
	}
	exported, err := s.thingsvis.ExportMarketDashboard(
		ctx,
		req.DashboardID,
		claims.TenantID,
		claims.ID,
		thingsVisAuthorization,
		thingsvisRoles,
	)
	if err != nil {
		code := errcode.CodeSystemError
		if errors.Is(err, ErrThingsVisRequestRejected) {
			code = errcode.CodeParamError
		}
		return nil, errcode.WithData(code, map[string]interface{}{
			"error":  "failed to export ThingsVis dashboard",
			"detail": err.Error(),
		})
	}

	deviceBindings := make([]model.DeviceBinding, 0, len(req.DeviceRoles))
	fieldBindings := make([]model.FieldBinding, 0)
	for _, role := range req.DeviceRoles {
		reference := referenceByDevice[role.SourceDeviceID]
		deviceBindings = append(deviceBindings, model.DeviceBinding{
			BindingKey:        role.BindingKey,
			DeviceTemplateKey: templateKeyByID[reference.DeviceTemplateID],
			Required:          true,
			DisplayName:       role.DisplayName,
		})
		for _, field := range reference.RequiredFields {
			fieldBindings = append(fieldBindings, model.FieldBinding{
				BindingKey: role.BindingKey,
				Kind:       field.Kind,
				Identifier: field.Identifier,
				Required:   true,
			})
		}
	}

	dashboard := model.DashboardTemplate{
		ResourceKey:    sanitizeResourceKey(req.Name),
		Version:        req.Version,
		Name:           req.Name,
		SchemaVersion:  exported.Snapshot.SchemaVersion,
		CanvasConfig:   exported.Snapshot.CanvasConfig,
		Nodes:          exported.Snapshot.Nodes,
		DataSources:    exported.Snapshot.DataSources,
		Variables:      exported.Snapshot.Variables,
		DeviceBindings: deviceBindings,
		FieldBindings:  fieldBindings,
	}
	resources := publisher.buildBundleResources(templates, templateKeyByID, thingModels, []model.DashboardTemplate{dashboard})
	sourceDeviceIDs := make([]string, 0, len(req.DeviceRoles))
	for _, role := range req.DeviceRoles {
		sourceDeviceIDs = append(sourceDeviceIDs, role.SourceDeviceID)
	}
	if failures := publisher.checkForRealIDs(resources, sourceDeviceIDs...); len(failures) > 0 {
		return nil, errcode.WithData(errcode.CodeParamError, failures)
	}
	metadata := &model.BundleMetadata{
		Name:          req.Name,
		Category:      req.Category,
		Description:   req.Description,
		CoverAssetKey: req.CoverAssetKey,
	}
	compatibility := &model.CompatibilityInfo{}
	security := &model.BundleSecurity{
		ContainsSecrets:     false,
		ContainsRuntimeData: false,
	}
	horizonRequest := &model.HorizonPublishRequest{
		ContractVersion: ContractVersion,
		BundleKind:      BundleKindDashboardTemplate,
		BundleKey:       req.BundleKey,
		Version:         req.Version,
		Metadata:        mustMarshalJSON(metadata),
		Compatibility:   mustMarshalJSON(compatibility),
		Resources:       mustMarshalJSON(resources),
		Security:        mustMarshalJSON(security),
	}
	idempotency := dashboardPublishIdempotencyKey(claims.TenantID, req.BundleKey, req.Version)
	horizonResponse, err := s.market.PublishBundle(ctx, req.MarketToken, idempotency, horizonRequest)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, err.Error())
	}
	if horizonResponse.Code != 0 {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error":  "market rejected dashboard Bundle",
			"detail": horizonResponse.Message,
		})
	}
	publishData, err := requireSuccessfulHorizonPublish(horizonResponse)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, err.Error())
	}
	return &model.PublishDraftResponse{
		BundleKey:   publishData.BundleKey,
		Version:     publishData.Version,
		ContentHash: publishData.ContentHash,
		Status:      publishData.Status,
	}, nil
}

func validateDashboardBundleRoles(input []model.DashboardBundleRole) (map[string]model.DashboardBundleRole, error) {
	bindingPattern := regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)
	byDevice := make(map[string]model.DashboardBundleRole)
	bindingKeys := make(map[string]bool)
	for _, role := range input {
		if role.SourceDeviceID == "" || strings.TrimSpace(role.DisplayName) == "" {
			return nil, fmt.Errorf("sourceDeviceId and displayName are required")
		}
		if !bindingPattern.MatchString(role.BindingKey) {
			return nil, fmt.Errorf("invalid bindingKey %s", role.BindingKey)
		}
		if _, exists := byDevice[role.SourceDeviceID]; exists {
			return nil, fmt.Errorf("duplicate sourceDeviceId %s", role.SourceDeviceID)
		}
		if bindingKeys[role.BindingKey] {
			return nil, fmt.Errorf("duplicate bindingKey %s", role.BindingKey)
		}
		byDevice[role.SourceDeviceID] = role
		bindingKeys[role.BindingKey] = true
	}
	return byDevice, nil
}

func dashboardPublishIdempotencyKey(tenantID, bundleKey, version string) string {
	payload, _ := json.Marshal([]string{tenantID, bundleKey, version})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func readRequiredThingModelFields(templateID string, identifiers []string) ([]model.ThingModelField, error) {
	publisher := &MarketBundlePublish{}
	all, failures := publisher.readThingModels(context.Background(), []model.LocalDeviceTemplate{{ID: templateID}})
	if len(failures) > 0 {
		return nil, fmt.Errorf("failed to read thing model for template %s", templateID)
	}
	byIdentifier := make(map[string]model.ThingModelField)
	for _, field := range all[templateID] {
		if _, exists := byIdentifier[field.Identifier]; !exists {
			byIdentifier[field.Identifier] = field
		}
	}
	required := make([]model.ThingModelField, 0, len(identifiers))
	for _, identifier := range identifiers {
		field, exists := byIdentifier[identifier]
		if !exists {
			return nil, fmt.Errorf("dashboard field %s does not exist in device template", identifier)
		}
		required = append(required, field)
	}
	return required, nil
}

var invalidBindingKeyChars = regexp.MustCompile(`[^a-z0-9-]+`)

func normalizeDashboardBundleRoles(input []model.DashboardBundleRole) []model.DashboardBundleRole {
	normalized := make([]model.DashboardBundleRole, len(input))
	for index, role := range input {
		role.DisplayName = portableDeviceDisplayName(role.DisplayName, role.SourceDeviceID)
		encodedDeviceID := strings.ReplaceAll(strings.ToLower(role.SourceDeviceID), "-", "_")
		if encodedDeviceID != "" && strings.Contains(strings.ToLower(role.BindingKey), encodedDeviceID) {
			role.BindingKey = deviceRoleBindingKey(role.SourceDeviceID)
		} else {
			role.BindingKey = strings.ReplaceAll(role.BindingKey, "_", "-")
		}
		normalized[index] = role
	}
	return normalized
}

func portableDeviceDisplayName(name, deviceID string) string {
	value := strings.TrimSpace(strings.ReplaceAll(name, deviceID, ""))
	value = strings.Trim(value, " -_")
	if value == "" {
		return "Device"
	}
	return value
}

func deviceRoleBindingKey(deviceID string) string {
	sum := sha256.Sum256([]byte(deviceID))
	return "device-" + hex.EncodeToString(sum[:])[:12]
}

func suggestBindingKey(name, deviceID string) string {
	value := strings.ToLower(strings.TrimSpace(name))
	value = invalidBindingKeyChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		value = deviceRoleBindingKey(deviceID)
	}
	if len(value) < 3 {
		value += "-device"
	}
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/utils"

	"github.com/sirupsen/logrus"
)

// MarketBundlePublish handles the bundle publish flow from local resources.
type MarketBundlePublish struct {
	marketClient   *MarketClient
	thingsvisClient *ThingsVisClient
}

// NewMarketBundlePublish creates a new MarketBundlePublish service.
func NewMarketBundlePublish() *MarketBundlePublish {
	return &MarketBundlePublish{
		marketClient:    NewMarketClient(),
		thingsvisClient: NewThingsVisClient(),
	}
}

// BundleKind constants for the publish flow.
const (
	BundleKindSolutionBundle = "solution-bundle"
	BundleKindDeviceTemplate = "device-template"
	BundleKindDashboardTemplate = "dashboard-template"
)

// ContractVersion is the current contract version for bundles.
const ContractVersion = "1.0"

// PublishDraft publishes a draft bundle from local resources.
func (s *MarketBundlePublish) PublishDraft(ctx context.Context, req model.PublishDraftRequest, claims *utils.UserClaims) (*model.PublishDraftResponse, *model.PublishDraftPrecheckReport, error) {
	// Generate idempotency key based on tenant and request content
	idempotencyKey := generateIdempotencyKey(claims.TenantID, req)

	// Step 1: Validate request parameters
	if err := s.validatePublishDraftRequest(req); err != nil {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: []model.PrecheckError{
				{Code: "INVALID_REQUEST", Message: err.Error()},
			},
		}
		return nil, precheckReport, err
	}

	// Step 2: Validate tenant owns all device templates
	deviceTemplates, err := s.validateDeviceTemplatesOwnership(ctx, req.DeviceTemplateIDs, claims.TenantID)
	if err != nil {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: []model.PrecheckError{
				{Code: model.ErrCodeResourceForbidden, Message: err.Error()},
			},
		}
		return nil, precheckReport, err
	}

	// Step 3: Validate tenant owns all dashboards
	dashboards, err := s.validateDashboardsOwnership(ctx, req.DashboardIDs, claims.TenantID)
	if err != nil {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: []model.PrecheckError{
				{Code: model.ErrCodeResourceForbidden, Message: err.Error()},
			},
		}
		return nil, precheckReport, err
	}

	// Step 4: Read thing model for each device template
	thingModelMap, precheckErrors := s.readThingModels(ctx, deviceTemplates)
	if len(precheckErrors) > 0 {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: precheckErrors,
		}
		return nil, precheckReport, fmt.Errorf("failed to read thing models")
	}

	// Step 5: Export dashboards from ThingsVis
	dashboardTemplates, exportErrors := s.exportDashboards(ctx, dashboards)
	if len(exportErrors) > 0 {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: exportErrors,
		}
		return nil, precheckReport, fmt.Errorf("failed to export dashboards")
	}

	// Step 6: Validate field bindings exist in thing models
	bindErrors := s.validateFieldBindings(deviceTemplates, thingModelMap, dashboardTemplates)
	if len(bindErrors) > 0 {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: bindErrors,
		}
		return nil, precheckReport, fmt.Errorf("invalid field bindings")
	}

	// Step 7: Build bundle resources
	bundleResources := s.buildBundleResources(deviceTemplates, thingModelMap, dashboardTemplates)

	// Step 8: Validate protocol configs (sanitize secrets)
	sanitizedResources, secretWarnings := s.sanitizeProtocolConfigs(bundleResources)
	warnings := secretWarnings

	// Step 9: Check for real IDs (deviceId, tenantId, userId) in resources
	idErrors := s.checkForRealIDs(sanitizedResources)
	if len(idErrors) > 0 {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: idErrors,
		}
		return nil, precheckReport, fmt.Errorf("real IDs detected in resources")
	}

	// Step 10: Build metadata
	metadata := s.buildMetadata(req, deviceTemplates, dashboards)

	// Step 11: Build compatibility
	compatibility := s.buildCompatibility(req, deviceTemplates)

	// Step 12: Build security section
	security := &model.BundleSecurity{
		ContainsSecrets:     false, // Already sanitized
		ContainsRuntimeData: false,
	}

	// Step 13: Calculate content hash
	contentHash, hashErr := s.calculateContentHash(ContractVersion, req.BundleKey, req.Version, metadata, compatibility, bundleResources, security)
	if hashErr != nil {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: []model.PrecheckError{
				{Code: "HASH_FAILED", Message: hashErr.Error()},
			},
		}
		return nil, precheckReport, hashErr
	}
	security.ContentHash = "sha256:" + contentHash

	// Step 14: Call Horizon API to publish
	horizonReq := &model.HorizonPublishRequest{
		ContractVersion: ContractVersion,
		BundleKind:      BundleKindSolutionBundle,
		BundleKey:       req.BundleKey,
		Version:         req.Version,
		Metadata:        mustMarshalJSON(metadata),
		Compatibility:   mustMarshalJSON(compatibility),
		Resources:       mustMarshalJSON(bundleResources),
		Security:        mustMarshalJSON(security),
	}

	_, publishErr := s.marketClient.PublishBundle(ctx, req.MarketToken, idempotencyKey, horizonReq)
	if publishErr != nil {
		precheckReport := &model.PublishDraftPrecheckReport{
			Passed: false,
			Errors: []model.PrecheckError{
				{Code: model.ErrCodeHorizonPublishFailed, Message: publishErr.Error()},
			},
		}
		return nil, precheckReport, publishErr
	}

	// Build final response
	response := &model.PublishDraftResponse{
		BundleKey:   req.BundleKey,
		Version:     req.Version,
		ContentHash: security.ContentHash,
		Status:      "published",
	}

	// Include precheck report with warnings if any
	if len(warnings) > 0 {
		response.PrecheckReport = &model.PublishDraftPrecheckReport{
			Passed:     true,
			Warnings:   warnings,
		}
	}

	// Log success
	logrus.Infof("[MarketBundlePublish] Published bundle: key=%s version=%s tenant=%s", 
		req.BundleKey, req.Version, claims.TenantID)

	return response, response.PrecheckReport, nil
}

// validatePublishDraftRequest validates the publish draft request parameters.
func (s *MarketBundlePublish) validatePublishDraftRequest(req model.PublishDraftRequest) error {
	if len(req.DeviceTemplateIDs) == 0 {
		return fmt.Errorf("at least one device template ID is required")
	}
	if len(req.DashboardIDs) == 0 {
		return fmt.Errorf("at least one dashboard ID is required")
	}
	if req.BundleKey == "" {
		return fmt.Errorf("bundle key is required")
	}
	// Validate bundle key pattern: ^[a-z][a-z0-9-]{2,63}$
	bundleKeyRegex := regexp.MustCompile(`^[a-z][a-z0-9-]{2,63}$`)
	if !bundleKeyRegex.MatchString(req.BundleKey) {
		return fmt.Errorf("bundle key must start with lowercase letter, contain only lowercase letters, numbers, and hyphens, and be 3-64 characters")
	}
	if req.Version == "" {
		return fmt.Errorf("version is required")
	}
	// Validate version pattern: ^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$
	versionRegex := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
	if !versionRegex.MatchString(req.Version) {
		return fmt.Errorf("version must follow semantic versioning (e.g., 1.0.0 or 1.0.0-beta)")
	}
	if req.MarketToken == "" {
		return fmt.Errorf("market token is required")
	}
	return nil
}

// validateDeviceTemplatesOwnership validates that all device templates belong to the tenant.
func (s *MarketBundlePublish) validateDeviceTemplatesOwnership(ctx context.Context, templateIDs []string, tenantID string) ([]model.LocalDeviceTemplate, error) {
	var templates []model.LocalDeviceTemplate
	q := query.DeviceTemplate

	for _, id := range templateIDs {
		tpl, err := q.WithContext(ctx).Where(q.ID.Eq(id)).First()
		if err != nil {
			return nil, fmt.Errorf("device template not found: %s", id)
		}
		if tpl.TenantID != tenantID {
			return nil, fmt.Errorf("access denied: device template %s belongs to another tenant", id)
		}
		templates = append(templates, model.LocalDeviceTemplate{
			ID:          tpl.ID,
			TenantID:    tpl.TenantID,
			Name:        tpl.Name,
			Version:     safePtrStr(tpl.Version),
			Brand:       safePtrStr(tpl.Brand),
			Description: safePtrStr(tpl.Description),
			Label:       safePtrStr(tpl.Label),
		})
	}
	return templates, nil
}

// validateDashboardsOwnership validates that all dashboards belong to the tenant.
func (s *MarketBundlePublish) validateDashboardsOwnership(ctx context.Context, dashboardIDs []string, tenantID string) ([]model.LocalDashboard, error) {
	var dashboards []model.LocalDashboard
	q := query.VisDashboard

	for _, id := range dashboardIDs {
		board, err := q.WithContext(ctx).Where(q.ID.Eq(id)).First()
		if err != nil {
			return nil, fmt.Errorf("dashboard not found: %s", id)
		}
		if board.TenantID != nil && *board.TenantID != tenantID {
			return nil, fmt.Errorf("access denied: dashboard %s belongs to another tenant", id)
		}
		dashboards = append(dashboards, model.LocalDashboard{
			ID:          board.ID,
			TenantID:    safePtrStr(board.TenantID),
			Name:        safePtrStr(board.DashboardName),
			JsonData:    safePtrStr(board.JSONDatum),
		})
	}
	return dashboards, nil
}

// ThingModelMap maps device template ID to its thing model fields.
type ThingModelMap map[string][]model.ThingModelField

// readThingModels reads thing models for all device templates.
func (s *MarketBundlePublish) readThingModels(ctx context.Context, templates []model.LocalDeviceTemplate) (ThingModelMap, []model.PrecheckError) {
	result := make(ThingModelMap)
	var errors []model.PrecheckError

	for _, tpl := range templates {
		var fields []model.ThingModelField

		// Read telemetry
		telemetry, err := dal.GetDeviceModelTelemetryDataList(tpl.ID)
		if err != nil {
			errors = append(errors, model.PrecheckError{
				Code:    model.ErrCodeThingModelReadFailed,
				Message: fmt.Sprintf("failed to read telemetry for template %s: %s", tpl.Name, err.Error()),
				Field:   "deviceTemplates." + tpl.ID + ".telemetry",
			})
			continue
		}
		for _, t := range telemetry {
			fields = append(fields, model.ThingModelField{
				Kind:        model.ThingModelFieldKindTelemetry,
				Identifier:  t.DataIdentifier,
				Name:        safePtrStr(t.DataName),
				DataType:    safePtrStr(t.DataType),
				Unit:        safePtrStr(t.Unit),
				Description: safePtrStr(t.Description),
				AccessMode:  safePtrStr(t.ReadWriteFlag),
			})
		}

		// Read attributes
		attributes, err := dal.GetDeviceModelAttributeDataList(tpl.ID)
		if err != nil {
			errors = append(errors, model.PrecheckError{
				Code:    model.ErrCodeThingModelReadFailed,
				Message: fmt.Sprintf("failed to read attributes for template %s: %s", tpl.Name, err.Error()),
				Field:   "deviceTemplates." + tpl.ID + ".attributes",
			})
			continue
		}
		for _, a := range attributes {
			fields = append(fields, model.ThingModelField{
				Kind:        model.ThingModelFieldKindAttribute,
				Identifier:  a.DataIdentifier,
				Name:        safePtrStr(a.DataName),
				DataType:    safePtrStr(a.DataType),
				Unit:        safePtrStr(a.Unit),
				Description: safePtrStr(a.Description),
				AccessMode:  safePtrStr(a.ReadWriteFlag),
			})
		}

		// Read commands
		commands, err := dal.GetDeviceModelCommandDataList(tpl.ID)
		if err != nil {
			errors = append(errors, model.PrecheckError{
				Code:    model.ErrCodeThingModelReadFailed,
				Message: fmt.Sprintf("failed to read commands for template %s: %s", tpl.Name, err.Error()),
				Field:   "deviceTemplates." + tpl.ID + ".commands",
			})
			continue
		}
		for _, c := range commands {
			fields = append(fields, model.ThingModelField{
				Kind:        model.ThingModelFieldKindCommand,
				Identifier:  c.DataIdentifier,
				Name:        safePtrStr(c.DataName),
				Description: safePtrStr(c.Description),
				AccessMode:  "write", // Commands are typically write-only
			})
		}

		// Read events
		events, err := dal.GetDeviceModelEventDataList(tpl.ID)
		if err != nil {
			errors = append(errors, model.PrecheckError{
				Code:    model.ErrCodeThingModelReadFailed,
				Message: fmt.Sprintf("failed to read events for template %s: %s", tpl.Name, err.Error()),
				Field:   "deviceTemplates." + tpl.ID + ".events",
			})
			continue
		}
		for _, e := range events {
			fields = append(fields, model.ThingModelField{
				Kind:        model.ThingModelFieldKindEvent,
				Identifier:  e.DataIdentifier,
				Name:        safePtrStr(e.DataName),
				Description: safePtrStr(e.Description),
				AccessMode:  "read", // Events are typically read-only
			})
		}

		result[tpl.ID] = fields
	}

	return result, errors
}

// exportDashboards exports dashboards from ThingsVis.
func (s *MarketBundlePublish) exportDashboards(ctx context.Context, dashboards []model.LocalDashboard) ([]model.DashboardTemplate, []model.PrecheckError) {
	var results []model.DashboardTemplate
	var errors []model.PrecheckError

	for _, db := range dashboards {
		exportData, err := s.thingsvisClient.ExportDashboardForMarket(ctx, db.ID)
		if err != nil {
			errors = append(errors, model.PrecheckError{
				Code:    model.ErrCodeDashboardExportFailed,
				Message: fmt.Sprintf("failed to export dashboard %s: %s", db.Name, err.Error()),
				Field:   "dashboards." + db.ID,
			})
			continue
		}

		// Parse device bindings from export data
		var deviceBindings []model.DeviceBinding
		if len(exportData.DeviceBindings) > 0 {
			if err := json.Unmarshal(exportData.DeviceBindings, &deviceBindings); err != nil {
				logrus.Warnf("Failed to parse device bindings for dashboard %s: %v", db.ID, err)
			}
		}

		// Parse field bindings from export data
		var fieldBindings []model.FieldBinding
		if len(exportData.FieldBindings) > 0 {
			if err := json.Unmarshal(exportData.FieldBindings, &fieldBindings); err != nil {
				logrus.Warnf("Failed to parse field bindings for dashboard %s: %v", db.ID, err)
			}
		}

		results = append(results, model.DashboardTemplate{
			ResourceKey:    sanitizeResourceKey(db.Name),
			Version:        "1.0.0",
			Name:           db.Name,
			SchemaVersion:  exportData.SchemaVersion,
			CanvasConfig:   exportData.CanvasConfig,
			Nodes:         exportData.Nodes,
			DataSources:   exportData.DataSources,
			Variables:     exportData.Variables,
			DeviceBindings: deviceBindings,
			FieldBindings:  fieldBindings,
		})
	}

	return results, errors
}

// validateFieldBindings validates that all field bindings reference valid fields in thing models.
func (s *MarketBundlePublish) validateFieldBindings(templates []model.LocalDeviceTemplate, thingModelMap ThingModelMap, dashboards []model.DashboardTemplate) []model.PrecheckError {
	var errors []model.PrecheckError

	// Build a map of template resource keys to template IDs
	templateKeyToID := make(map[string]string)
	templateIDToResourceKey := make(map[string]string)
	for _, tpl := range templates {
		resourceKey := sanitizeResourceKey(tpl.Name)
		templateKeyToID[resourceKey] = tpl.ID
		templateIDToResourceKey[tpl.ID] = resourceKey
	}

	// Build a set of valid field identifiers per template
	validFields := make(map[string]map[string]model.ThingModelField)
	for tplID, fields := range thingModelMap {
		validFields[tplID] = make(map[string]model.ThingModelField)
		for _, field := range fields {
			validFields[tplID][string(field.Kind)+"."+field.Identifier] = field
		}
	}

	for _, dash := range dashboards {
		for _, binding := range dash.FieldBindings {
			// Find the template for this binding
			templateResourceKey := ""
			for _, db := range dash.DeviceBindings {
				if db.BindingKey == binding.BindingKey {
					templateResourceKey = db.DeviceTemplateKey
					break
				}
			}

			if templateResourceKey == "" {
				errors = append(errors, model.PrecheckError{
					Code:    model.ErrCodeInvalidFieldBinding,
					Message: fmt.Sprintf("field binding references unknown binding key: %s", binding.BindingKey),
					Field:   "dashboards." + dash.ResourceKey + ".fieldBindings",
				})
				continue
			}

			templateID, ok := templateKeyToID[templateResourceKey]
			if !ok {
				errors = append(errors, model.PrecheckError{
					Code:    model.ErrCodeInvalidFieldBinding,
					Message: fmt.Sprintf("field binding references unknown device template: %s", templateResourceKey),
					Field:   "dashboards." + dash.ResourceKey + ".fieldBindings",
				})
				continue
			}

			fieldKey := string(binding.Kind) + "." + binding.Identifier
			if _, exists := validFields[templateID][fieldKey]; !exists {
				errors = append(errors, model.PrecheckError{
					Code:    model.ErrCodeInvalidFieldBinding,
					Message: fmt.Sprintf("field binding references non-existent field: %s.%s", binding.Kind, binding.Identifier),
					Field:   "dashboards." + dash.ResourceKey + ".fieldBindings." + binding.BindingKey,
					Details: fmt.Sprintf("template: %s, valid fields: %v", templateResourceKey, getValidFields(validFields[templateID])),
				})
			}
		}
	}

	return errors
}

// getValidFields returns a list of valid field identifiers for logging.
func getValidFields(fields map[string]model.ThingModelField) []string {
	var result []string
	for key := range fields {
		result = append(result, key)
	}
	return result
}

// buildBundleResources builds the bundle resources from templates and dashboards.
func (s *MarketBundlePublish) buildBundleResources(templates []model.LocalDeviceTemplate, thingModelMap ThingModelMap, dashboards []model.DashboardTemplate) *model.BundleResources {
	deviceTemplates := make([]model.BundleDeviceTemplate, 0, len(templates))
	
	for _, tpl := range templates {
		dt := model.BundleDeviceTemplate{
			ResourceKey: sanitizeResourceKey(tpl.Name),
			Version:     tpl.Version,
			Name:        tpl.Name,
			ThingModel:  thingModelMap[tpl.ID],
			Protocol: &model.ProtocolInfo{
				ProtocolType:   "",
				PublicDefaults: make(map[string]interface{}),
			},
		}

		// Read device config for protocol info
		if dc, err := dal.GetDeviceConfigByTemplateID(tpl.ID); err == nil && dc != nil {
			if dc.ProtocolType != nil {
				dt.Protocol.ProtocolType = *dc.ProtocolType
			}
		}

		deviceTemplates = append(deviceTemplates, dt)
	}

	return &model.BundleResources{
		DeviceTemplates: deviceTemplates,
		Dashboards:      dashboards,
	}
}

// sanitizeProtocolConfigs sanitizes protocol configs to remove secrets.
func (s *MarketBundlePublish) sanitizeProtocolConfigs(resources *model.BundleResources) (*model.BundleResources, []model.PrecheckWarning) {
	var warnings []model.PrecheckWarning

	// For each device template, we would sanitize the protocol config
	// Since we only store sanitized info (ProtocolType), no actual secrets are present
	// This is a safeguard in case ProtocolConfig is ever added

	return resources, warnings
}

// checkForRealIDs checks for real device IDs, tenant IDs, or user IDs in resources.
func (s *MarketBundlePublish) checkForRealIDs(resources *model.BundleResources) []model.PrecheckError {
	var errors []model.PrecheckError

	// Serialize resources to check for patterns
	resourcesJSON, err := json.Marshal(resources)
	if err != nil {
		return errors
	}

	resourcesStr := string(resourcesJSON)

	// Check for real device ID patterns (UUID format)
	deviceIDRegex := regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`)
	deviceIDs := deviceIDRegex.FindAllString(resourcesStr, -1)
	if len(deviceIDs) > 0 {
		errors = append(errors, model.PrecheckError{
			Code:    model.ErrCodeRealDeviceIDDetected,
			Message: "real device IDs detected in resources",
			Details: fmt.Sprintf("found %d device ID(s)", len(deviceIDs)),
		})
	}

	// Check for tenant ID patterns
	tenantIDRegex := regexp.MustCompile(`"tenant_?[Ii][Dd"\s:]+[a-z0-9]{8,}`)
	if tenantIDRegex.MatchString(resourcesStr) {
		errors = append(errors, model.PrecheckError{
			Code:    model.ErrCodeRealTenantIDDetected,
			Message: "real tenant IDs detected in resources",
		})
	}

	// Check for user ID patterns
	userIDRegex := regexp.MustCompile(`"user_?[Ii][Dd"\s:]+[a-z0-9]{8,}`)
	if userIDRegex.MatchString(resourcesStr) {
		errors = append(errors, model.PrecheckError{
			Code:    model.ErrCodeRealUserIDDetected,
			Message: "real user IDs detected in resources",
		})
	}

	return errors
}

// buildMetadata builds the bundle metadata.
func (s *MarketBundlePublish) buildMetadata(req model.PublishDraftRequest, templates []model.LocalDeviceTemplate, dashboards []model.LocalDashboard) *model.BundleMetadata {
	metadata := &model.BundleMetadata{
		Name:         templates[0].Name,
		Category:     req.Category,
		Description:  req.Description,
		Brand:        req.Brand,
	}

	// Use first template name as bundle name if not explicitly set
	if metadata.Name == "" && len(templates) > 0 {
		metadata.Name = templates[0].Name
	}

	// Use first dashboard description if not set
	if metadata.Description == "" && len(dashboards) > 0 {
		metadata.Description = dashboards[0].Name
	}

	// Default category
	if metadata.Category == "" {
		metadata.Category = "uncategorized"
	}

	return metadata
}

// buildCompatibility builds the compatibility section.
func (s *MarketBundlePublish) buildCompatibility(req model.PublishDraftRequest, templates []model.LocalDeviceTemplate) *model.CompatibilityInfo {
	compatibility := &model.CompatibilityInfo{
		MinThingsPanel: req.MinThingsPanel,
		MinThingsVis:   req.MinThingsVis,
		Plugins:        make([]model.BundlePluginDependency, 0),
	}

	// Get plugin dependencies from device configs
	for _, tpl := range templates {
		if dc, err := dal.GetDeviceConfigByTemplateID(tpl.ID); err == nil && dc != nil {
			if dc.ProtocolType != nil && *dc.ProtocolType != "" {
				// Look up plugin version
				pluginDeps := getPluginDependenciesFromDeviceConfig(dc)
				compatibility.Plugins = append(compatibility.Plugins, pluginDeps...)
			}
		}
	}

	return compatibility
}

// calculateContentHash calculates SHA-256 hash of bundle content.
func (s *MarketBundlePublish) calculateContentHash(contractVersion, bundleKey, version string, metadata *model.BundleMetadata, compatibility *model.CompatibilityInfo, resources *model.BundleResources, security *model.BundleSecurity) (string, error) {
	// Build canonical JSON for hashing
	canonical := map[string]interface{}{
		"contractVersion": contractVersion,
		"bundleKind":      BundleKindSolutionBundle,
		"bundleKey":       bundleKey,
		"version":         version,
		"metadata":        metadata,
		"compatibility":   compatibility,
		"resources":       resources,
		"security": &model.BundleSecurity{
			ContainsSecrets:     security.ContainsSecrets,
			ContainsRuntimeData: security.ContainsRuntimeData,
		},
	}

	// Marshal with canonical ordering
	jsonBytes, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("failed to marshal canonical JSON: %w", err)
	}

	// Normalize JSON (remove whitespace)
	normalized := normalizeJSON(string(jsonBytes))

	// Calculate SHA-256
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:]), nil
}

// normalizeJSON normalizes JSON string by removing whitespace.
func normalizeJSON(jsonStr string) string {
	var buf bytes.Buffer
	for _, r := range jsonStr {
		if !unicode.IsSpace(r) {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// generateIdempotencyKey generates an idempotency key for the request.
func generateIdempotencyKey(tenantID string, req model.PublishDraftRequest) string {
	data := fmt.Sprintf("%s:%s:%v:%v", tenantID, req.BundleKey, req.Version, req.DeviceTemplateIDs)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:32]
}

// sanitizeResourceKey converts a name to a valid resource key.
func sanitizeResourceKey(name string) string {
	var result strings.Builder
	for i, r := range strings.ToLower(name) {
		if i == 0 && !isLetter(r) {
			result.WriteRune('a')
		}
		if isLetter(r) || isDigit(r) || r == '-' {
			result.WriteRune(r)
		} else if r == ' ' || r == '_' {
			result.WriteRune('-')
		}
	}
	key := result.String()
	// Ensure minimum length
	if len(key) < 3 {
		key = "res-" + key
	}
	// Trim leading/trailing hyphens
	key = strings.Trim(key, "-")
	if len(key) < 3 {
		key = "default-resource"
	}
	return key
}

func isLetter(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c rune) bool {
	return c >= '0' && c <= '9'
}

// getPluginDependenciesFromDeviceConfig extracts plugin dependencies from device config.
func getPluginDependenciesFromDeviceConfig(dc *model.DeviceConfig) []model.BundlePluginDependency {
	if dc == nil || dc.ProtocolType == nil || *dc.ProtocolType == "" {
		return nil
	}

	// Query plugin version
	version := ""
	if dc.ProtocolType != nil {
		pluginMsg, _ := query.ServicePlugin.WithContext(context.Background()).
			Where(query.ServicePlugin.ServiceIdentifier.Eq(*dc.ProtocolType)).
			First()
		if pluginMsg != nil && pluginMsg.Version != nil {
			version = *pluginMsg.Version
		}
	}

	return []model.BundlePluginDependency{
		{
			Identifier: *dc.ProtocolType,
			MinVersion: version,
		},
	}
}

// mustMarshalJSON marshals an object to JSON, panics on error.
func mustMarshalJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// safePtrStr safely dereferences a string pointer.
func safePtrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

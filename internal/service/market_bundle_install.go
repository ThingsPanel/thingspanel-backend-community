package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// MarketBundleInstallService handles the orchestration of market bundle installations
type MarketBundleInstallService struct {
	installRepo  *repo.MarketBundleInstallRepo
	marketClient *MarketClient
	thingsVis    *ThingsVisClient
	httpClient   *http.Client
}

// NewMarketBundleInstallService creates a new install service
func NewMarketBundleInstallService() *MarketBundleInstallService {
	return &MarketBundleInstallService{
		installRepo:  repo.NewMarketBundleInstallRepo(),
		marketClient: NewMarketClient(),
		thingsVis:    NewThingsVisClient(),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// InstallBundle orchestrates the complete installation of a market bundle
func (s *MarketBundleInstallService) InstallBundle(ctx context.Context, req *model.InstallBundleRequest, claims *utils.UserClaims) (*model.InstallBundleResponse, error) {
	if owner := os.Getenv("TP_MARKETINSTALL_OWNER"); owner != "" && owner != "legacy" {
		return nil, errcode.WithData(errcode.CodeOpDenied, "legacy installation writer is retired or owner selection is invalid; use the selected owner")
	}
	tenantID := claims.TenantID
	if err := supportedInstallPolicy(req.OverwritePolicy); err != nil {
		return nil, err
	}

	// Reject duplicate binding keys before creating an installation or importing
	// any resources. resolveDeviceBindings performs the same check defensively.
	seenBindings := make(map[string]struct{}, len(req.DeviceBindings))
	for _, binding := range req.DeviceBindings {
		if _, exists := seenBindings[binding.BindingKey]; exists {
			return nil, errcode.WithData(errcode.CodeParamError, "duplicate device binding: "+binding.BindingKey)
		}
		seenBindings[binding.BindingKey] = struct{}{}
	}

	// 1. Handle idempotency
	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		// An omitted key denotes a new installation instance. Callers that
		// need safe retries must retain and resend an explicit key.
		idempotencyKey = uuid.New()
	}

	requestDigest := installationRequestDigest(req, tenantID, claims.ID)

	// Check for existing installation with same idempotency key
	existing, err := s.installRepo.GetByIdempotencyKey(ctx, idempotencyKey, tenantID)
	if err == nil && existing != nil {
		if err := checkInstallationReplay(existing, requestDigest); err != nil {
			return nil, err
		}
		return s.buildIdempotentResponse(ctx, existing, tenantID)
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		// An unavailable lookup is not evidence that no installation exists.
		// Do not create a new record or start remote work on an unknown result.
		return nil, errcode.WithData(errcode.CodeDBError, "Failed to look up installation")
	}

	// 2. Create installation record
	inst := &model.MarketBundleInstallation{
		IdempotencyKey: idempotencyKey,
		RequestDigest:  requestDigest,
		BundleKey:      req.BundleKey,
		BundleVersion:  req.Version,
		TenantID:       tenantID,
		Status:         model.InstallStateDownloading,
	}
	inst, err = s.installRepo.CreateInstallation(ctx, inst)
	if err != nil {
		// A concurrent request (or a committed insert whose reply was lost)
		// may own this key. Re-read only; never start a second set of effects.
		existing, lookupErr := s.installRepo.GetByIdempotencyKey(ctx, idempotencyKey, tenantID)
		if lookupErr == nil {
			if replayErr := checkInstallationReplay(existing, requestDigest); replayErr != nil {
				return nil, replayErr
			}
			return s.buildIdempotentResponse(ctx, existing, tenantID)
		}
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"errorCode": "INSTALLATION_CREATE_RESULT_UNKNOWN", "idempotencyKey": idempotencyKey,
			"error": "Installation record was not reliably acknowledged; retry only with the same key",
		})
	}

	// Record audit
	s.recordAudit(ctx, inst.ID, tenantID, "install_started", "", model.InstallStateDownloading, nil)

	// 3. Download bundle from Horizon
	bundle, err := s.downloadBundleFromHorizon(ctx, req.MarketToken, req.BundleKey, req.Version)
	if err != nil {
		return nil, s.failInstallationFrom(ctx, inst.ID, tenantID, inst.Status, "DOWNLOAD_FAILED", err)
	}

	// Update to DOWNLOADED state
	if err := s.installRepo.TransitionStatus(ctx, inst.ID, tenantID, model.InstallStateDownloading, model.InstallStateDownloaded, "", ""); err != nil {
		return nil, installationPersistenceError(inst.ID, "state_downloaded", err)
	}
	inst.Status = model.InstallStateDownloaded
	s.recordAudit(ctx, inst.ID, tenantID, "state_change", model.InstallStateDownloading, model.InstallStateDownloaded, nil)

	// 4. Verify bundle (hash, signature, schema, compatibility)
	if err := s.verifyBundle(ctx, inst.ID, tenantID, bundle, req.MarketToken); err != nil {
		return nil, s.failInstallationFrom(ctx, inst.ID, tenantID, inst.Status, "VERIFICATION_FAILED", err)
	}

	// Update to VERIFIED state
	if err := s.installRepo.TransitionStatus(ctx, inst.ID, tenantID, model.InstallStateDownloaded, model.InstallStateVerified, "", ""); err != nil {
		return nil, installationPersistenceError(inst.ID, "state_verified", err)
	}
	inst.Status = model.InstallStateVerified
	s.recordAudit(ctx, inst.ID, tenantID, "state_change", model.InstallStateDownloaded, model.InstallStateVerified, nil)

	// 5. Install device templates
	warnings := []string{}
	deviceTemplateMappings, installWarnings, err := s.installDeviceTemplates(ctx, inst.ID, tenantID, bundle, req.OverwritePolicy)
	if err != nil {
		return nil, s.failInstallationFrom(ctx, inst.ID, tenantID, inst.Status, "MODELS_INSTALL_FAILED", err)
	}
	warnings = append(warnings, installWarnings...)

	// Update to MODELS_INSTALLED state
	if err := s.installRepo.TransitionStatus(ctx, inst.ID, tenantID, model.InstallStateVerified, model.InstallStateModelsInstalled, "", ""); err != nil {
		return nil, installationPersistenceError(inst.ID, "state_modelsinstalled", err)
	}
	inst.Status = model.InstallStateModelsInstalled
	s.recordAudit(ctx, inst.ID, tenantID, "state_change", model.InstallStateVerified, model.InstallStateModelsInstalled, nil)

	// 6. Validate all runtime device bindings before creating any dashboard.
	resolvedBindings, err := s.resolveDeviceBindings(ctx, tenantID, bundle, req.DeviceBindings, deviceTemplateMappings)
	if err != nil {
		return nil, s.failInstallationFrom(ctx, inst.ID, tenantID, inst.Status, "BINDING_FAILED", err)
	}

	// 7. Create dashboards via ThingsVis with real tenant device IDs.
	dashboardMappings, err := s.createDashboards(ctx, inst.ID, tenantID, claims.ID, bundle, resolvedBindings)
	if err != nil {
		return nil, s.failInstallationFrom(ctx, inst.ID, tenantID, inst.Status, "DASHBOARD_CREATE_FAILED", err)
	}

	// Update to DASHBOARDS_CREATED state
	if err := s.installRepo.TransitionStatus(ctx, inst.ID, tenantID, model.InstallStateModelsInstalled, model.InstallStateDashboardsCreated, "", ""); err != nil {
		return nil, installationPersistenceError(inst.ID, "state_dashboardscreated", err)
	}
	inst.Status = model.InstallStateDashboardsCreated
	s.recordAudit(ctx, inst.ID, tenantID, "state_change", model.InstallStateModelsInstalled, model.InstallStateDashboardsCreated, nil)

	// 8. Record device bindings
	bindingStatuses, finalWarnings, err := s.processDeviceBindings(ctx, inst.ID, tenantID, bundle, req.DeviceBindings, deviceTemplateMappings)
	if err != nil {
		return nil, s.failInstallationFrom(ctx, inst.ID, tenantID, inst.Status, "BINDING_FAILED", err)
	}
	warnings = append(warnings, finalWarnings...)

	// Update warnings
	if len(warnings) > 0 {
		if err := s.installRepo.UpdateWarnings(ctx, inst.ID, warnings); err != nil {
			return nil, installationPersistenceError(inst.ID, "warnings", err)
		}
	}

	// 9. Determine final state
	finalState := model.InstallStateCompleted
	hasUnboundRequired := false
	for _, bs := range bindingStatuses {
		if bs.Required && bs.LocalDeviceID == "" {
			hasUnboundRequired = true
			break
		}
	}

	if hasUnboundRequired {
		finalState = model.InstallStateWaitingForBindings
	}

	// Update to final state
	if err := s.installRepo.TransitionStatus(ctx, inst.ID, tenantID, model.InstallStateDashboardsCreated, finalState, "", ""); err != nil {
		return nil, installationPersistenceError(inst.ID, "final_state", err)
	}
	s.recordAudit(ctx, inst.ID, tenantID, "state_change", model.InstallStateDashboardsCreated, finalState, nil)
	s.recordAudit(ctx, inst.ID, tenantID, "install_completed", "", finalState, nil)

	// 10. Notify Horizon of installation (async)
	if finalState == model.InstallStateCompleted {
		go s.notifyHorizonInstallComplete(context.Background(), req.MarketToken, req.BundleKey, req.Version, tenantID, inst.ID)
	}

	// Build response
	return s.buildInstallResponse(inst.ID, req.BundleKey, req.Version, finalState, deviceTemplateMappings, dashboardMappings, bindingStatuses, warnings, false, "")
}

// downloadBundleFromHorizon downloads the bundle from Horizon market
func (s *MarketBundleInstallService) downloadBundleFromHorizon(ctx context.Context, token, bundleKey, version string) (*model.HorizonBundleDownload, error) {
	baseURL := s.marketClient.baseURL
	url := fmt.Sprintf("%s/api/market/bundles/%s/versions/%s/download", baseURL, bundleKey, version)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to download bundle from Horizon: " + err.Error(),
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": fmt.Sprintf("Horizon download failed with status %d: %s", resp.StatusCode, string(body)),
		})
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to read download response: " + err.Error(),
		})
	}

	// Parse bundle JSON
	var bundle struct {
		ContractVersion string          `json:"contractVersion"`
		BundleKind      string          `json:"bundleKind"`
		Metadata        json.RawMessage `json:"metadata"`
		Compatibility   json.RawMessage `json:"compatibility"`
		Resources       json.RawMessage `json:"resources"`
		Security        json.RawMessage `json:"security"`
	}
	if err := json.Unmarshal(body, &bundle); err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to parse bundle JSON: " + err.Error(),
		})
	}

	// Compute content hash
	hash := sha256.Sum256(body)
	contentHash := "sha256:" + hex.EncodeToString(hash[:])

	return &model.HorizonBundleDownload{
		BundleFileBytes: body,
		ContentHash:     contentHash,
		ContractVersion: bundle.ContractVersion,
		BundleKind:      bundle.BundleKind,
		Metadata:        bundle.Metadata,
		Compatibility:   bundle.Compatibility,
		Resources:       bundle.Resources,
		Security:        bundle.Security,
	}, nil
}

// verifyBundle verifies the downloaded bundle
func (s *MarketBundleInstallService) verifyBundle(ctx context.Context, installID, tenantID string, bundle *model.HorizonBundleDownload, token string) error {
	// 1. Parse security section
	var security struct {
		ContainsSecrets     bool   `json:"containsSecrets"`
		ContainsRuntimeData bool   `json:"containsRuntimeData"`
		ContentHash         string `json:"contentHash"`
		Signature           string `json:"signature"`
	}
	if err := json.Unmarshal(bundle.Security, &security); err != nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Failed to parse bundle security section: " + err.Error(),
		})
	}

	// 2. Reject bundles with secrets
	if security.ContainsSecrets {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "BUNDLE_CONTAINS_SECRETS: Bundles containing secrets are not allowed",
		})
	}

	// 3. Verify content hash
	if security.ContentHash != "" && security.ContentHash != bundle.ContentHash {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": fmt.Sprintf("Content hash mismatch: expected %s, got %s", security.ContentHash, bundle.ContentHash),
		})
	}

	// 4. Verify contract version
	if bundle.ContractVersion != "1.0" {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": fmt.Sprintf("Unsupported contract version: %s", bundle.ContractVersion),
		})
	}

	// 5. Verify compatibility (ThingsPanel version, ThingsVis version, plugins)
	var compatibility struct {
		MinThingsPanel string `json:"minThingsPanel"`
		MinThingsVis   string `json:"minThingsVis"`
		Plugins        []struct {
			Identifier string `json:"identifier"`
			MinVersion string `json:"minVersion"`
		} `json:"plugins"`
	}
	if bundle.Compatibility != nil {
		if err := json.Unmarshal(bundle.Compatibility, &compatibility); err != nil {
			logrus.Warnf("Failed to parse compatibility section: %v", err)
		}
	}

	// Check plugin dependencies
	var warnings []string
	for _, plugin := range compatibility.Plugins {
		p, err := dal.GetServicePluginByServiceIdentifier(plugin.Identifier)
		if err != nil || p == nil {
			warnings = append(warnings, fmt.Sprintf("Plugin '%s' not found locally", plugin.Identifier))
			s.recordAudit(ctx, installID, tenantID, "plugin_warning", "", "", &model.MarketResourceMapping{
				ResourceType: "plugin",
				LocalName:    plugin.Identifier,
			})
		}
	}

	if len(warnings) > 0 {
		if err := s.installRepo.UpdateWarnings(ctx, installID, warnings); err != nil {
			return installationPersistenceError(installID, "verification_warnings", err)
		}
	}

	s.recordAudit(ctx, installID, tenantID, "bundle_verified", "", "", nil)
	return nil
}

// installDeviceTemplates installs device templates from the bundle
func (s *MarketBundleInstallService) installDeviceTemplates(ctx context.Context, installID, tenantID string, bundle *model.HorizonBundleDownload, overwritePolicy string) ([]*model.ResourceMappingResponse, []string, error) {
	if err := supportedInstallPolicy(overwritePolicy); err != nil {
		return nil, nil, err
	}
	var resources struct {
		DeviceTemplates []struct {
			ResourceKey string `json:"resourceKey"`
			Version     string `json:"version"`
			Name        string `json:"name"`
			Protocol    struct {
				ProtocolType   string                 `json:"protocolType"`
				PublicDefaults map[string]interface{} `json:"publicDefaults"`
			} `json:"protocol"`
			ThingModel []struct {
				Kind        string `json:"kind"`
				Identifier  string `json:"identifier"`
				Name        string `json:"name"`
				DataType    string `json:"dataType"`
				Unit        string `json:"unit"`
				Description string `json:"description"`
				AccessMode  string `json:"accessMode"`
			} `json:"thingModel"`
		} `json:"deviceTemplates"`
	}

	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return nil, nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to parse device templates: " + err.Error(),
		})
	}

	var mappings []*model.ResourceMappingResponse
	var warnings []string

	// Check for existing templates with same name (default: do not overwrite)
	for _, dt := range resources.DeviceTemplates {
		existingTpl, err := dal.GetDeviceTemplateByNameAndTenant(dt.Name, tenantID)
		if err == nil && existingTpl != nil {
			// This is a reference to a pre-existing tenant resource, never an
			// ownership claim authorizing compensation to delete it.
			mapping := &model.MarketResourceMapping{
				InstallationID: installID, TenantID: tenantID,
				ResourceType:      model.ResourceTypeDeviceTemplate,
				MarketResourceKey: dt.ResourceKey, MarketVersion: dt.Version,
				LocalID: existingTpl.ID, LocalName: dt.Name, Status: "skipped_existing",
			}
			if _, err := s.installRepo.CreateResourceMapping(ctx, mapping); err != nil {
				return nil, warnings, installationPersistenceError(installID, "reused_template_mapping", err)
			}
			warnings = append(warnings, fmt.Sprintf("Template '%s' already exists locally, skipped", dt.Name))
			mappings = append(mappings, &model.ResourceMappingResponse{
				ResourceType: model.ResourceTypeDeviceTemplate, MarketResourceKey: dt.ResourceKey,
				LocalID: existingTpl.ID, LocalName: dt.Name, Status: "skipped_existing",
			})
			continue
		}

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, warnings, installationPersistenceError(installID, "lookup_template", err)
		}
		// Create new template
		templateID, err := s.createDeviceTemplate(ctx, tenantID, &dt)
		if err != nil {
			return nil, warnings, err
		}

		// Record mapping
		mapping := &model.MarketResourceMapping{
			InstallationID:    installID,
			TenantID:          tenantID,
			ResourceType:      model.ResourceTypeDeviceTemplate,
			MarketResourceKey: dt.ResourceKey,
			MarketVersion:     dt.Version,
			LocalID:           templateID,
			LocalName:         dt.Name,
			Status:            "active",
		}
		if _, err := s.installRepo.CreateResourceMapping(ctx, mapping); err != nil {
			return nil, warnings, installationPersistenceError(installID, "template_mapping", err)
		}

		mappings = append(mappings, &model.ResourceMappingResponse{
			ResourceType:      model.ResourceTypeDeviceTemplate,
			MarketResourceKey: dt.ResourceKey,
			LocalID:           templateID,
			LocalName:         dt.Name,
			Status:            "created",
		})

		s.recordAudit(ctx, installID, tenantID, "resource_created", "", "", &model.MarketResourceMapping{
			ResourceType:      model.ResourceTypeDeviceTemplate,
			MarketResourceKey: dt.ResourceKey,
			LocalID:           templateID,
		})
	}

	return mappings, warnings, nil
}

// createDeviceTemplate creates a device template from bundle definition
func (s *MarketBundleInstallService) createDeviceTemplate(ctx context.Context, tenantID string, tmpl *struct {
	ResourceKey string `json:"resourceKey"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Protocol    struct {
		ProtocolType   string                 `json:"protocolType"`
		PublicDefaults map[string]interface{} `json:"publicDefaults"`
	} `json:"protocol"`
	ThingModel []struct {
		Kind        string `json:"kind"`
		Identifier  string `json:"identifier"`
		Name        string `json:"name"`
		DataType    string `json:"dataType"`
		Unit        string `json:"unit"`
		Description string `json:"description"`
		AccessMode  string `json:"accessMode"`
	} `json:"thingModel"`
}) (string, error) {
	now := time.Now().UTC()
	flag := int16(1) // private

	templateID := uuid.New()

	// Create device template
	newTemplate := &model.DeviceTemplate{
		ID:          templateID,
		Name:        tmpl.Name,
		TenantID:    tenantID,
		Version:     &tmpl.Version,
		Description: &tmpl.Name,
		Flag:        &flag,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	tx := global.DB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return "", tx.Error
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			// Let the request recovery boundary handle the panic. Returning
			// normally here would fabricate an empty template ID and nil error.
			panic(r)
		}
	}()

	if err := tx.Create(newTemplate).Error; err != nil {
		tx.Rollback()
		return "", err
	}

	// Create thing model entries
	for _, field := range tmpl.ThingModel {
		switch field.Kind {
		case "telemetry":
			tm := model.DeviceModelTelemetry{
				ID:               uuid.New(),
				DeviceTemplateID: templateID,
				TenantID:         tenantID,
				DataName:         &field.Name,
				DataIdentifier:   field.Identifier,
				ReadWriteFlag:    getStringPtr(platformAccessMode(field.AccessMode)),
				DataType:         &field.DataType,
				Unit:             &field.Unit,
				Description:      &field.Description,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&tm).Error; err != nil {
				tx.Rollback()
				return "", err
			}
		case "attribute":
			attr := model.DeviceModelAttribute{
				ID:               uuid.New(),
				DeviceTemplateID: templateID,
				TenantID:         tenantID,
				DataName:         &field.Name,
				DataIdentifier:   field.Identifier,
				ReadWriteFlag:    getStringPtr(platformAccessMode(field.AccessMode)),
				DataType:         &field.DataType,
				Unit:             &field.Unit,
				Description:      &field.Description,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&attr).Error; err != nil {
				tx.Rollback()
				return "", err
			}
		case "event":
			evt := model.DeviceModelEvent{
				ID:               uuid.New(),
				DeviceTemplateID: templateID,
				TenantID:         tenantID,
				DataName:         &field.Name,
				DataIdentifier:   field.Identifier,
				Description:      &field.Description,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&evt).Error; err != nil {
				tx.Rollback()
				return "", err
			}
		case "command":
			cmd := model.DeviceModelCommand{
				ID:               uuid.New(),
				DeviceTemplateID: templateID,
				TenantID:         tenantID,
				DataName:         &field.Name,
				DataIdentifier:   field.Identifier,
				Description:      &field.Description,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&cmd).Error; err != nil {
				tx.Rollback()
				return "", err
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return "", err
	}

	return templateID, nil
}

// resolveDeviceBindings validates tenant ownership and exact installed template compatibility.
func (s *MarketBundleInstallService) resolveDeviceBindings(
	ctx context.Context,
	tenantID string,
	bundle *model.HorizonBundleDownload,
	input []model.DeviceBindingInput,
	templateMappings []*model.ResourceMappingResponse,
) (map[string]string, error) {
	var resources struct {
		Dashboards []struct {
			DeviceBindings []struct {
				BindingKey        string `json:"bindingKey"`
				DeviceTemplateKey string `json:"deviceTemplateKey"`
			} `json:"deviceBindings"`
		} `json:"dashboards"`
	}
	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "failed to parse dashboard bindings")
	}

	expectedTemplateIDs := make(map[string]string, len(templateMappings))
	for _, mapping := range templateMappings {
		if mapping.ResourceType == model.ResourceTypeDeviceTemplate {
			expectedTemplateIDs[mapping.MarketResourceKey] = mapping.LocalID
		}
	}
	provided := make(map[string]string, len(input))
	for _, binding := range input {
		if _, exists := provided[binding.BindingKey]; exists {
			return nil, errcode.WithData(errcode.CodeParamError, "duplicate device binding: "+binding.BindingKey)
		}
		provided[binding.BindingKey] = binding.LocalDeviceID
	}

	resolved := make(map[string]string)
	expectedKeys := make(map[string]bool)
	for _, dashboard := range resources.Dashboards {
		for _, binding := range dashboard.DeviceBindings {
			expectedKeys[binding.BindingKey] = true
			localDeviceID := provided[binding.BindingKey]
			if localDeviceID == "" {
				return nil, errcode.WithData(errcode.CodeParamError, "device binding is required: "+binding.BindingKey)
			}

			device, err := dal.GetDeviceByID(localDeviceID)
			if err != nil || device == nil || device.TenantID != tenantID {
				return nil, errcode.WithData(errcode.CodeParamError, "bound device is unavailable")
			}
			if device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
				return nil, errcode.WithData(errcode.CodeParamError, "bound device has no device configuration")
			}
			config, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
			if err != nil || config == nil || config.DeviceTemplateID == nil {
				return nil, errcode.WithData(errcode.CodeParamError, "bound device has no device template")
			}
			expectedTemplateID := expectedTemplateIDs[binding.DeviceTemplateKey]
			if expectedTemplateID == "" || *config.DeviceTemplateID != expectedTemplateID {
				return nil, errcode.WithData(
					errcode.CodeParamError,
					fmt.Sprintf("device binding %s is not compatible with template %s", binding.BindingKey, binding.DeviceTemplateKey),
				)
			}
			resolved[binding.BindingKey] = localDeviceID
		}
	}
	for bindingKey := range provided {
		if !expectedKeys[bindingKey] {
			return nil, errcode.WithData(errcode.CodeParamError, "unknown device binding: "+bindingKey)
		}
	}
	return resolved, nil
}

// createDashboards creates dashboards via ThingsVis import API
func (s *MarketBundleInstallService) createDashboards(ctx context.Context, installID, tenantID, userID string, bundle *model.HorizonBundleDownload, resolvedBindings map[string]string) ([]*model.ResourceMappingResponse, error) {
	var resources struct {
		Dashboards []struct {
			ResourceKey    string          `json:"resourceKey"`
			Version        string          `json:"version"`
			Name           string          `json:"name"`
			SchemaVersion  string          `json:"schemaVersion"`
			CanvasConfig   json.RawMessage `json:"canvasConfig"`
			Nodes          json.RawMessage `json:"nodes"`
			DataSources    json.RawMessage `json:"dataSources"`
			Variables      json.RawMessage `json:"variables"`
			DeviceBindings []struct {
				BindingKey        string `json:"bindingKey"`
				DeviceTemplateKey string `json:"deviceTemplateKey"`
				Required          bool   `json:"required"`
				AllowMany         bool   `json:"allowMany"`
				DisplayName       string `json:"displayName"`
			} `json:"deviceBindings"`
			FieldBindings []struct {
				BindingKey string `json:"bindingKey"`
				Kind       string `json:"kind"`
				Identifier string `json:"identifier"`
				Required   bool   `json:"required"`
			} `json:"fieldBindings"`
		} `json:"dashboards"`
	}

	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to parse dashboards: " + err.Error(),
		})
	}

	// A binding key is installation-scoped in the durable schema. Multiple
	// dashboards may share it, provided they agree on its definition.
	bindingDefinitions := make(map[string]*model.MarketBundleBindingStatus)
	for _, dash := range resources.Dashboards {
		for _, db := range dash.DeviceBindings {
			if previous, ok := bindingDefinitions[db.BindingKey]; ok {
				if previous.DeviceTemplateKey != db.DeviceTemplateKey || previous.Required != db.Required {
					return nil, errcode.WithData(errcode.CodeParamError, "conflicting dashboard binding definition: "+db.BindingKey)
				}
				continue
			}
			status := model.BindingStatusPending
			if resolvedBindings[db.BindingKey] != "" {
				status = model.BindingStatusBound
			}
			bindingDefinitions[db.BindingKey] = &model.MarketBundleBindingStatus{
				InstallationID: installID, BindingKey: db.BindingKey,
				DeviceTemplateKey: db.DeviceTemplateKey, Required: db.Required,
				LocalDeviceID: resolvedBindings[db.BindingKey], Status: status,
			}
		}
	}
	// Persist the full binding intent before any dashboard import.
	keys := make([]string, 0, len(bindingDefinitions))
	for key := range bindingDefinitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := s.installRepo.CreateBindingStatus(ctx, bindingDefinitions[key]); err != nil {
			return nil, installationPersistenceError(installID, "create_binding", err)
		}
	}
	var mappings []*model.ResourceMappingResponse

	for _, dash := range resources.Dashboards {
		// Create dashboard via ThingsVis import
		dashboardID, err := s.importDashboard(ctx, tenantID, userID, &dash, resolvedBindings)
		if err != nil {
			return nil, fmt.Errorf("failed to create dashboard %s: %w", dash.Name, err)
		}

		// Record mapping
		mapping := &model.MarketResourceMapping{
			InstallationID:    installID,
			TenantID:          tenantID,
			ResourceType:      model.ResourceTypeDashboard,
			MarketResourceKey: dash.ResourceKey,
			MarketVersion:     dash.Version,
			LocalID:           dashboardID,
			LocalName:         dash.Name,
			Status:            "active",
		}
		if _, err := s.installRepo.CreateResourceMapping(ctx, mapping); err != nil {
			return nil, installationPersistenceError(installID, "dashboard_mapping", err)
		}

		mappings = append(mappings, &model.ResourceMappingResponse{
			ResourceType:      model.ResourceTypeDashboard,
			MarketResourceKey: dash.ResourceKey,
			LocalID:           dashboardID,
			LocalName:         dash.Name,
			Status:            "created",
		})

		s.recordAudit(ctx, installID, tenantID, "resource_created", "", "", &model.MarketResourceMapping{
			ResourceType:      model.ResourceTypeDashboard,
			MarketResourceKey: dash.ResourceKey,
			LocalID:           dashboardID,
		})

	}

	return mappings, nil
}

// importDashboard imports a dashboard template into ThingsVis
func (s *MarketBundleInstallService) importDashboard(ctx context.Context, tenantID, userID string, dash *struct {
	ResourceKey    string          `json:"resourceKey"`
	Version        string          `json:"version"`
	Name           string          `json:"name"`
	SchemaVersion  string          `json:"schemaVersion"`
	CanvasConfig   json.RawMessage `json:"canvasConfig"`
	Nodes          json.RawMessage `json:"nodes"`
	DataSources    json.RawMessage `json:"dataSources"`
	Variables      json.RawMessage `json:"variables"`
	DeviceBindings []struct {
		BindingKey        string `json:"bindingKey"`
		DeviceTemplateKey string `json:"deviceTemplateKey"`
		Required          bool   `json:"required"`
		AllowMany         bool   `json:"allowMany"`
		DisplayName       string `json:"displayName"`
	} `json:"deviceBindings"`
	FieldBindings []struct {
		BindingKey string `json:"bindingKey"`
		Kind       string `json:"kind"`
		Identifier string `json:"identifier"`
		Required   bool   `json:"required"`
	} `json:"fieldBindings"`
}, resolvedBindings map[string]string) (string, error) {
	importReq := ThingsVisImportRequest{
		Name: dash.Name,
		DashboardSnapshot: ThingsVisMarketSnapshot{
			Name:          dash.Name,
			SchemaVersion: dash.SchemaVersion,
			CanvasConfig:  dash.CanvasConfig,
			Nodes:         dash.Nodes,
			DataSources:   dash.DataSources,
			Variables:     dash.Variables,
		},
		DeviceBindings: func() []DeviceBindingImport {
			result := make([]DeviceBindingImport, 0, len(dash.DeviceBindings))
			for _, db := range dash.DeviceBindings {
				result = append(result, DeviceBindingImport{
					BindingKey:    db.BindingKey,
					LocalDeviceID: resolvedBindings[db.BindingKey],
				})
			}
			return result
		}(),
	}

	return s.thingsVis.ImportDashboard(ctx, tenantID, userID, &importReq)
}

// processDeviceBindings validates and records device bindings
func (s *MarketBundleInstallService) processDeviceBindings(ctx context.Context, installID, tenantID string, bundle *model.HorizonBundleDownload, bindings []model.DeviceBindingInput, templateMappings []*model.ResourceMappingResponse) ([]*model.BindingStatusResponse, []string, error) {
	var resources struct {
		Dashboards []struct {
			DeviceBindings []struct {
				BindingKey        string `json:"bindingKey"`
				DeviceTemplateKey string `json:"deviceTemplateKey"`
				Required          bool   `json:"required"`
				AllowMany         bool   `json:"allowMany"`
				DisplayName       string `json:"displayName"`
			} `json:"deviceBindings"`
		} `json:"dashboards"`
	}

	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return nil, nil, err
	}

	var allBindings []struct {
		BindingKey        string
		DeviceTemplateKey string
		Required          bool
		AllowMany         bool
		DisplayName       string
	}
	for _, dash := range resources.Dashboards {
		for _, db := range dash.DeviceBindings {
			allBindings = append(allBindings, struct {
				BindingKey        string
				DeviceTemplateKey string
				Required          bool
				AllowMany         bool
				DisplayName       string
			}{
				BindingKey:        db.BindingKey,
				DeviceTemplateKey: db.DeviceTemplateKey,
				Required:          db.Required,
				AllowMany:         db.AllowMany,
				DisplayName:       db.DisplayName,
			})
		}
	}

	var responses []*model.BindingStatusResponse
	var warnings []string

	// Build template key to ID mapping
	templateMap := make(map[string]string)
	for _, m := range templateMappings {
		if m.ResourceType == model.ResourceTypeDeviceTemplate {
			templateMap[m.MarketResourceKey] = m.LocalID
		}
	}

	for _, expectedBinding := range allBindings {
		response := &model.BindingStatusResponse{
			BindingKey:        expectedBinding.BindingKey,
			DeviceTemplateKey: expectedBinding.DeviceTemplateKey,
			Required:          expectedBinding.Required,
			Status:            model.BindingStatusPending,
		}

		// Find user-provided binding
		var userBinding *model.DeviceBindingInput
		for i := range bindings {
			if bindings[i].BindingKey == expectedBinding.BindingKey {
				userBinding = &bindings[i]
				break
			}
		}

		if userBinding != nil {
			// Validate device belongs to tenant
			device, err := dal.GetDeviceByID(userBinding.LocalDeviceID)
			if err != nil || device == nil {
				response.Status = model.BindingStatusFailed
				response.ErrorMessage = "Device not found"
				responses = append(responses, response)
				if err := s.updateBindingStatus(ctx, installID, expectedBinding.BindingKey, "", model.BindingStatusFailed, "Device not found"); err != nil {
					return nil, warnings, installationPersistenceError(installID, "update_binding", err)
				}
				continue
			}

			if device.TenantID != tenantID {
				response.Status = model.BindingStatusFailed
				response.ErrorMessage = "Device does not belong to current tenant"
				responses = append(responses, response)
				if err := s.updateBindingStatus(ctx, installID, expectedBinding.BindingKey, "", model.BindingStatusFailed, "Device does not belong to current tenant"); err != nil {
					return nil, warnings, installationPersistenceError(installID, "update_binding", err)
				}
				continue
			}

			// Validate device template is compatible
			expectedTemplateID := templateMap[expectedBinding.DeviceTemplateKey]
			if device.DeviceConfigID != nil && expectedTemplateID != "" {
				// Get device config to check template
				dc, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
				if err == nil && dc != nil && dc.DeviceTemplateID != nil && *dc.DeviceTemplateID != expectedTemplateID {
					warnings = append(warnings, fmt.Sprintf("Device %s has different template than binding %s expects", userBinding.LocalDeviceID, expectedBinding.BindingKey))
				}
			}

			// Update binding status
			if err := s.updateBindingStatus(ctx, installID, expectedBinding.BindingKey, userBinding.LocalDeviceID, model.BindingStatusBound, ""); err != nil {
				return nil, warnings, installationPersistenceError(installID, "update_binding", err)
			}

			response.LocalDeviceID = userBinding.LocalDeviceID
			response.Status = model.BindingStatusBound
		}

		responses = append(responses, response)
	}

	return responses, warnings, nil
}

// updateBindingStatus updates a binding status record
func (s *MarketBundleInstallService) updateBindingStatus(ctx context.Context, installID, bindingKey, localDeviceID, status, errorMessage string) error {
	binding, err := s.installRepo.GetBindingByKey(ctx, installID, bindingKey)
	if err != nil {
		return err
	}
	return s.installRepo.UpdateBindingDevice(ctx, binding.ID, localDeviceID, status, errorMessage)
}

// failInstallation marks an installation as failed and triggers compensation if needed
func (s *MarketBundleInstallService) failInstallation(ctx context.Context, installID, tenantID, errorCode string, cause error) error {
	inst, err := s.installRepo.GetByID(ctx, installID)
	if err != nil {
		return installationPersistenceError(installID, "failure_state_lookup", err)
	}
	if inst.TenantID != tenantID {
		return errcode.WithData(errcode.CodeOpDenied, "installation tenant mismatch")
	}
	return s.failInstallationFrom(ctx, installID, tenantID, inst.Status, errorCode, cause)
}

func (s *MarketBundleInstallService) failInstallationFrom(ctx context.Context, installID, tenantID, from, errorCode string, cause error) error {
	// After resource creation begins, absence of mappings cannot prove absence
	// of effects. Keep the review hold even when the mapping write failed.
	target := model.InstallStateFailed
	if from == model.InstallStateVerified || from == model.InstallStateModelsInstalled || from == model.InstallStateDashboardsCreated {
		target = model.InstallStateCompensationRequired
	}
	message := cause.Error()
	var structured *errcode.Error
	if errors.As(cause, &structured) {
		if data, ok := structured.Data.(map[string]interface{}); ok && data["errorCode"] == "PERSISTENCE_RESULT_UNKNOWN" {
			errorCode = "PERSISTENCE_RESULT_UNKNOWN"
			message = fmt.Sprintf("Persistence outcome at %v is unknown; reconcile using the original operation identity", data["phase"])
		}
	}
	if err := s.installRepo.TransitionStatus(ctx, installID, tenantID, from, target, errorCode, message); err != nil {
		return installationPersistenceError(installID, "failure_state", err)
	}
	s.recordAudit(ctx, installID, tenantID, "state_change", from, target, nil)
	if target == model.InstallStateFailed {
		s.checkCompensationNeeded(ctx, installID, tenantID)
	}
	return cause
}

// checkCompensationNeeded determines if compensation is required
func (s *MarketBundleInstallService) checkCompensationNeeded(ctx context.Context, installID, tenantID string) {
	mappings, err := s.installRepo.GetResourceMappingsByInstallation(ctx, installID)
	if err != nil {
		logrus.Warnf("Failed to get resource mappings for compensation check: %v", err)
		return
	}

	hasCreatedResources := false
	for _, m := range mappings {
		if m.Status == "active" {
			hasCreatedResources = true
			break
		}
	}

	if hasCreatedResources {
		updated, err := s.installRepo.HoldFailedCompensationForReview(ctx, installID, tenantID)
		if err != nil {
			logrus.Warnf("Failed to update status to COMPENSATION_REQUIRED: %v", err)
			return
		}
		if updated {
			s.recordAudit(ctx, installID, tenantID, "state_change", model.InstallStateFailed, model.InstallStateCompensationRequired, nil)
		}
	}
}

// CompensateInstallation holds unsafe automatic compensation for manual review.
func (s *MarketBundleInstallService) CompensateInstallation(ctx context.Context, installID, tenantID string) error {
	inst, err := s.installRepo.GetByID(ctx, installID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errcode.WithData(errcode.CodeDBError, "Failed to query installation")
	}
	if err != nil || inst == nil || tenantID == "" || inst.TenantID != tenantID {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	if inst.Status != model.InstallStateCompensationRequired && inst.Status != model.InstallStateFailed {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation is not in compensation required state",
		})
	}

	// Existing mappings are not an authoritative inventory: a remote create
	// may have succeeded before its mapping was saved. The current code also
	// cannot verify ownership or reconcile an unknown remote delete. Stop
	// before any deletion until the owner ports and recovery contract exist.
	if inst.Status == model.InstallStateFailed {
		updated, updateErr := s.installRepo.HoldFailedCompensationForReview(ctx, installID, tenantID)
		if updateErr != nil {
			return errcode.WithData(errcode.CodeDBError, "Failed to retain compensation state")
		}
		if !updated {
			return errcode.WithData(errcode.CodeOpDenied, "Installation changed; query its current state")
		}
	}
	return errcode.WithData(errcode.CodeOpDenied, "Automatic compensation is unavailable; reconcile resource ownership and outcomes before retry")
}

// GetInstallationStatus retrieves installation status with full details
func (s *MarketBundleInstallService) GetInstallationStatus(ctx context.Context, installID, tenantID string) (*model.InstallBundleResponse, error) {
	inst, err := s.installRepo.GetByID(ctx, installID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	if inst.TenantID != tenantID {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	mappings, _ := s.installRepo.GetResourceMappingsByInstallation(ctx, installID)
	bindings, _ := s.installRepo.GetBindingStatusesByInstallation(ctx, installID)

	var resourceMappings []*model.ResourceMappingResponse
	for _, m := range mappings {
		resourceMappings = append(resourceMappings, &model.ResourceMappingResponse{
			ResourceType:      m.ResourceType,
			MarketResourceKey: m.MarketResourceKey,
			LocalID:           m.LocalID,
			LocalName:         m.LocalName,
			Status:            m.Status,
		})
	}

	var bindingStatuses []*model.BindingStatusResponse
	for _, b := range bindings {
		bindingStatuses = append(bindingStatuses, &model.BindingStatusResponse{
			BindingKey:        b.BindingKey,
			DeviceTemplateKey: b.DeviceTemplateKey,
			Required:          b.Required,
			LocalDeviceID:     b.LocalDeviceID,
			Status:            b.Status,
			ErrorMessage:      b.ErrorMessage,
		})
	}

	var warnings []string
	if inst.Warnings != nil {
		json.Unmarshal(inst.Warnings, &warnings)
	}

	return &model.InstallBundleResponse{
		InstallationID:   inst.ID,
		BundleKey:        inst.BundleKey,
		Version:          inst.BundleVersion,
		Status:           inst.Status,
		ResourceMappings: resourceMappings,
		BindingStatus:    bindingStatuses,
		Warnings:         warnings,
		IsIdempotent:     false,
	}, nil
}

// UpdateDeviceBinding updates a device binding for a WAITING_FOR_BINDINGS installation
func (s *MarketBundleInstallService) UpdateDeviceBinding(ctx context.Context, installID, tenantID string, req *model.UpdateBindingRequest) error {
	// Keep this path closed until dashboard updates, model compatibility, and
	// binding revision/recovery are coordinated with their owning services.
	// The existing implementation below is retained for review, not enabled.
	if err := dashboardBindingUpdateReadiness(); err != nil {
		return err
	}
	inst, err := s.installRepo.GetByID(ctx, installID)
	if err != nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	if inst.TenantID != tenantID {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	if inst.Status != model.InstallStateWaitingForBindings {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation is not in WAITING_FOR_BINDINGS state",
		})
	}

	// Validate device
	device, err := dal.GetDeviceByID(req.LocalDeviceID)
	if err != nil || device == nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Device not found",
		})
	}

	if device.TenantID != tenantID {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Device does not belong to current tenant",
		})
	}

	// Update binding
	if err := s.updateBindingStatus(ctx, installID, req.BindingKey, req.LocalDeviceID, model.BindingStatusBound, ""); err != nil {
		return err
	}

	// Check if all required bindings are now complete
	bindings, err := s.installRepo.GetBindingStatusesByInstallation(ctx, installID)
	if err != nil {
		return err
	}

	allRequiredBound := true
	for _, b := range bindings {
		if b.Required && b.LocalDeviceID == "" {
			allRequiredBound = false
			break
		}
	}

	if allRequiredBound {
		// Complete the installation
		if err := s.installRepo.TransitionStatus(ctx, installID, tenantID, model.InstallStateWaitingForBindings, model.InstallStateCompleted, "", ""); err != nil {
			return err
		}
		s.recordAudit(ctx, installID, tenantID, "state_change", model.InstallStateWaitingForBindings, model.InstallStateCompleted, nil)
		s.recordAudit(ctx, installID, tenantID, "all_bindings_complete", "", "", nil)
	}

	return nil
}

// dashboardBindingUpdateReadiness deliberately has no runtime override: the
// old local-only update cannot prove that the dashboard binding was applied.
func dashboardBindingUpdateReadiness() error {
	return errcode.WithData(errcode.CodeOpDenied, "Automatic binding updates are unavailable; reconcile the existing installation first")
}

// RetryInstallation retries a failed installation
func (s *MarketBundleInstallService) RetryInstallation(ctx context.Context, installID, tenantID string, req *model.RetryInstallationRequest) (*model.InstallBundleResponse, error) {
	inst, err := s.installRepo.GetByID(ctx, installID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	if inst.TenantID != tenantID {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Installation not found",
		})
	}

	if inst.Status != model.InstallStateFailed && inst.Status != model.InstallStateCompensationRequired {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "Only failed installations can be retried",
		})
	}

	// This endpoint previously returned DOWNLOADING without resuming any work.
	// It cannot reconcile potentially applied side effects yet.
	return nil, errcode.WithData(errcode.CodeOpDenied, "Automatic retry is unavailable; reconcile the existing installation first")
}

// ListInstallations lists all installations for a tenant
func (s *MarketBundleInstallService) ListInstallations(ctx context.Context, tenantID string, q *model.ListInstallationsRequest) (*model.ListInstallationsResponse, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}

	installations, total, err := s.installRepo.ListByTenant(ctx, tenantID, q)
	if err != nil {
		return nil, err
	}

	return &model.ListInstallationsResponse{
		Data:     installations,
		Total:    int(total),
		Page:     q.Page,
		PageSize: q.PageSize,
	}, nil
}

// --- Helper methods ---

func (s *MarketBundleInstallService) buildIdempotentResponse(ctx context.Context, inst *model.MarketBundleInstallation, tenantID string) (*model.InstallBundleResponse, error) {
	if inst.TenantID != tenantID {
		return nil, errcode.WithData(errcode.CodeOpDenied, "installation tenant mismatch")
	}
	mappings, err := s.installRepo.GetResourceMappingsByInstallation(ctx, inst.ID)
	if err != nil {
		return nil, installationPersistenceError(inst.ID, "read_mappings", err)
	}
	bindings, err := s.installRepo.GetBindingStatusesByInstallation(ctx, inst.ID)
	if err != nil {
		return nil, installationPersistenceError(inst.ID, "read_bindings", err)
	}

	var resourceMappings []*model.ResourceMappingResponse
	for _, m := range mappings {
		resourceMappings = append(resourceMappings, &model.ResourceMappingResponse{
			ResourceType:      m.ResourceType,
			MarketResourceKey: m.MarketResourceKey,
			LocalID:           m.LocalID,
			LocalName:         m.LocalName,
			Status:            m.Status,
		})
	}

	var bindingStatuses []*model.BindingStatusResponse
	for _, b := range bindings {
		bindingStatuses = append(bindingStatuses, &model.BindingStatusResponse{
			BindingKey:        b.BindingKey,
			DeviceTemplateKey: b.DeviceTemplateKey,
			Required:          b.Required,
			LocalDeviceID:     b.LocalDeviceID,
			Status:            b.Status,
			ErrorMessage:      b.ErrorMessage,
		})
	}

	return &model.InstallBundleResponse{
		InstallationID:    inst.ID,
		BundleKey:         inst.BundleKey,
		Version:           inst.BundleVersion,
		Status:            inst.Status,
		ResourceMappings:  resourceMappings,
		BindingStatus:     bindingStatuses,
		IsIdempotent:      true,
		ExistingInstallID: inst.ID,
	}, nil
}

func (s *MarketBundleInstallService) buildInstallResponse(installID, bundleKey, version, status string, templateMappings []*model.ResourceMappingResponse, dashboardMappings []*model.ResourceMappingResponse, bindingStatuses []*model.BindingStatusResponse, warnings []string, isIdempotent bool, existingID string) (*model.InstallBundleResponse, error) {
	// Combine all resource mappings
	allMappings := append(templateMappings, dashboardMappings...)

	return &model.InstallBundleResponse{
		InstallationID:    installID,
		BundleKey:         bundleKey,
		Version:           version,
		Status:            status,
		ResourceMappings:  allMappings,
		BindingStatus:     bindingStatuses,
		Warnings:          warnings,
		IsIdempotent:      isIdempotent,
		ExistingInstallID: existingID,
	}, nil
}

func (s *MarketBundleInstallService) recordAudit(ctx context.Context, installID, tenantID, action, prevState, newState string, mapping *model.MarketResourceMapping) {
	audit := &model.MarketInstallationAudit{
		InstallationID: installID,
		TenantID:       tenantID,
		Action:         action,
		PrevState:      prevState,
		NewState:       newState,
	}
	if mapping != nil {
		audit.ResourceType = mapping.ResourceType
		audit.ResourceKey = mapping.MarketResourceKey
		audit.LocalID = mapping.LocalID
	}
	s.installRepo.CreateAuditEntry(ctx, audit)
}

func (s *MarketBundleInstallService) notifyHorizonInstallComplete(ctx context.Context, token, bundleKey, version, tenantID, installID string) {
	if token == "" {
		logrus.Warnf("No market token available for install notification")
		return
	}

	baseURL := s.marketClient.baseURL
	url := fmt.Sprintf("%s/api/market/bundles/installations/%s/status", baseURL, installID)

	reqBody := map[string]string{
		"bundleKey": bundleKey,
		"version":   version,
		"tenantId":  tenantID,
		"status":    "completed",
	}
	reqBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		logrus.Warnf("Failed to create install notification request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		logrus.Warnf("Failed to notify Horizon of installation: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		logrus.Warnf("Horizon install notification failed with status %d: %s", resp.StatusCode, string(body))
	} else {
		logrus.Infof("Successfully notified Horizon of installation %s", installID)
	}
}

// getStringPtr returns a pointer to a string
func getStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// This versioned encoding describes only operation-affecting request fields.
// Credentials and retry transport keys are intentionally excluded. It is a
// local replay check, not an artifact signature or a remote owner receipt.
func installationRequestDigest(req *model.InstallBundleRequest, tenantID, actorID string) string {
	bindings := append([]model.DeviceBindingInput{}, req.DeviceBindings...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].BindingKey < bindings[j].BindingKey })
	policy := req.OverwritePolicy
	if policy == "" {
		policy = "default"
	}
	payload, _ := json.Marshal(struct {
		Version       int                        `json:"digestVersion"`
		Tenant        string                     `json:"tenant"`
		Actor         string                     `json:"actor"`
		Bundle        string                     `json:"bundle"`
		BundleVersion string                     `json:"version"`
		Policy        string                     `json:"policy"`
		Bindings      []model.DeviceBindingInput `json:"bindings"`
	}{1, tenantID, actorID, req.BundleKey, req.Version, policy, bindings})
	sum := sha256.Sum256(payload)
	return "v1:" + hex.EncodeToString(sum[:])
}

func checkInstallationReplay(inst *model.MarketBundleInstallation, digest string) error {
	code := "IDEMPOTENCY_PAYLOAD_CONFLICT"
	if inst.RequestDigest == "" {
		code = "IDEMPOTENCY_LEGACY_UNVERIFIABLE"
	} else if inst.RequestDigest == digest {
		return nil
	}
	return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
		"errorCode": code, "installationId": inst.ID,
		"error": "The key cannot be replayed for this request; inspect the existing installation without starting new effects",
	})
}

func installationPersistenceError(installID, phase string, cause error) error {
	// A failed database operation may have committed. This response must never
	// claim that the result or a review hold has been persisted.
	logrus.Warnf("Installation %s persistence outcome at %s is unknown: %v", installID, phase, cause)
	return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
		"errorCode": "PERSISTENCE_RESULT_UNKNOWN", "installationId": installID, "phase": phase,
		"error": "Installation progress was not reliably acknowledged; retain the same key and reconcile before retrying effects",
	})
}

func supportedInstallPolicy(policy string) error {
	if policy == "" || policy == "default" {
		return nil
	}
	code := "OVERWRITE_POLICY_UNSUPPORTED"
	if policy != "upgrade" {
		code = "OVERWRITE_POLICY_INVALID"
	}
	return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
		"errorCode": code,
		"error":     "Only the default no-overwrite installation policy is currently supported",
	})
}

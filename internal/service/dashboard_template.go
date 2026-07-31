package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"project/internal/model"
	"project/internal/repo"
	"project/pkg/errcode"
	"project/pkg/utils"

	"gorm.io/gorm"
)

// DashboardTemplateService separates reusable template download from runtime
// dashboard creation.
type DashboardTemplateService struct {
	repo           *repo.DashboardTemplateRepo
	installService *MarketBundleInstallService
	thingsVis      *ThingsVisClient
}

func NewDashboardTemplateService() *DashboardTemplateService {
	installService := NewMarketBundleInstallService()
	return &DashboardTemplateService{
		repo:           repo.NewDashboardTemplateRepo(),
		installService: installService,
		thingsVis:      installService.thingsVis,
	}
}

// NewDashboardTemplateServiceWithThingsVisBaseURL uses the same public
// ThingsVis proxy as the browser. This avoids assuming the IoT backend shares
// Docker DNS with ThingsVis in every deployment.
func NewDashboardTemplateServiceWithThingsVisBaseURL(baseURL string) *DashboardTemplateService {
	result := NewDashboardTemplateService()
	result.thingsVis = NewThingsVisClientWithBaseURL(baseURL)
	return result
}

func (s *DashboardTemplateService) Download(
	ctx context.Context,
	req *model.DownloadDashboardTemplateRequest,
	claims *utils.UserClaims,
) (*model.DownloadDashboardTemplateResponse, error) {
	tenantID := claims.TenantID

	// Always re-read the immutable market bundle. Besides authenticating the
	// download, this lets an idempotent retry repair a previous partial save
	// instead of treating one existing dashboard as proof that the whole bundle
	// is complete.
	installation, err := s.prepareDownloadInstallation(ctx, req, tenantID)
	if err != nil {
		return nil, err
	}
	installID := installation.ID
	fail := func(code string, cause error) error {
		s.installService.failInstallation(ctx, installID, tenantID, code, cause.Error())
		return cause
	}

	bundle, err := s.installService.downloadBundleFromHorizon(ctx, req.MarketToken, req.BundleKey, req.Version)
	if err != nil {
		return nil, fail("DOWNLOAD_FAILED", err)
	}
	_ = s.installService.installRepo.UpdateStatus(ctx, installID, model.InstallStateDownloaded, "", "")

	if err := s.installService.verifyBundle(ctx, installID, tenantID, bundle, req.MarketToken); err != nil {
		return nil, fail("VERIFICATION_FAILED", err)
	}
	_ = s.installService.installRepo.UpdateStatus(ctx, installID, model.InstallStateVerified, "", "")

	templateMappings, _, err := s.installService.installDeviceTemplates(
		ctx,
		installID,
		tenantID,
		bundle,
		"default",
	)
	if err != nil {
		return nil, fail("MODELS_INSTALL_FAILED", err)
	}
	if err := s.validateDeviceTemplateMappings(ctx, tenantID, bundle, templateMappings); err != nil {
		return nil, fail("MODELS_INCOMPATIBLE", err)
	}
	_ = s.installService.installRepo.UpdateStatus(ctx, installID, model.InstallStateModelsInstalled, "", "")

	response, err := s.saveDashboardTemplates(ctx, tenantID, req, bundle, templateMappings)
	if err != nil {
		return nil, fail("TEMPLATES_SAVE_FAILED", err)
	}
	_ = s.installService.installRepo.UpdateStatus(ctx, installID, model.InstallStateCompleted, "", "")
	s.installService.recordAudit(
		ctx,
		installID,
		tenantID,
		"templates_downloaded",
		model.InstallStateModelsInstalled,
		model.InstallStateCompleted,
		nil,
	)
	go s.installService.notifyHorizonInstallComplete(
		context.Background(),
		req.MarketToken,
		req.BundleKey,
		req.Version,
		tenantID,
		installID,
	)
	return response, nil
}

func (s *DashboardTemplateService) prepareDownloadInstallation(
	ctx context.Context,
	req *model.DownloadDashboardTemplateRequest,
	tenantID string,
) (*model.MarketBundleInstallation, error) {
	idempotencyKey := "download:" + s.installService.generateIdempotencyKey(req.BundleKey, req.Version, tenantID)
	existing, err := s.installService.installRepo.GetByIdempotencyKey(ctx, idempotencyKey, tenantID)
	if err == nil {
		_ = s.installService.installRepo.UpdateStatus(ctx, existing.ID, model.InstallStateDownloading, "", "")
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dbError("query dashboard template download", err)
	}

	installation, err := s.installService.installRepo.CreateInstallation(ctx, &model.MarketBundleInstallation{
		IdempotencyKey: idempotencyKey,
		BundleKey:      req.BundleKey,
		BundleVersion:  req.Version,
		TenantID:       tenantID,
		Status:         model.InstallStateDownloading,
	})
	if err != nil {
		return nil, dbError("create dashboard template download", err)
	}
	s.installService.recordAudit(
		ctx,
		installation.ID,
		tenantID,
		"template_download_started",
		"",
		model.InstallStateDownloading,
		nil,
	)
	return installation, nil
}

func (s *DashboardTemplateService) saveDashboardTemplates(
	ctx context.Context,
	tenantID string,
	req *model.DownloadDashboardTemplateRequest,
	bundle *model.HorizonBundleDownload,
	templateMappings []*model.ResourceMappingResponse,
) (*model.DownloadDashboardTemplateResponse, error) {
	var resources model.BundleResources
	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, "invalid dashboard resources: "+err.Error())
	}
	if len(resources.Dashboards) == 0 {
		return nil, errcode.WithData(errcode.CodeParamError, "bundle contains no dashboard templates")
	}

	var metadata model.BundleMetadata
	if len(bundle.Metadata) > 0 {
		_ = json.Unmarshal(bundle.Metadata, &metadata)
	}

	localTemplates := make(map[string]*model.ResourceMappingResponse, len(templateMappings))
	for _, mapping := range templateMappings {
		localTemplates[mapping.MarketResourceKey] = mapping
	}

	response := &model.DownloadDashboardTemplateResponse{
		TemplateIDs: make([]string, 0, len(resources.Dashboards)),
	}
	for _, dashboard := range resources.Dashboards {
		existing, err := s.repo.FindMarketTemplate(
			ctx,
			tenantID,
			req.BundleKey,
			req.Version,
			dashboard.ResourceKey,
		)
		if err == nil {
			response.TemplateIDs = append(response.TemplateIDs, existing.ID)
			response.Reused++
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, dbError("query local dashboard template", err)
		}

		snapshot, err := json.Marshal(model.DashboardTemplateSnapshot{
			Name:          dashboard.Name,
			SchemaVersion: dashboard.SchemaVersion,
			CanvasConfig:  dashboard.CanvasConfig,
			Nodes:         dashboard.Nodes,
			DataSources:   dashboard.DataSources,
			Variables:     dashboard.Variables,
			FieldBindings: dashboard.FieldBindings,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal dashboard snapshot %s: %w", dashboard.ResourceKey, err)
		}

		bindings := make([]*model.LocalDashboardTemplateBinding, 0, len(dashboard.DeviceBindings))
		for _, binding := range dashboard.DeviceBindings {
			mapping := localTemplates[binding.DeviceTemplateKey]
			if mapping == nil || mapping.LocalID == "" {
				return nil, errcode.WithData(
					errcode.CodeParamError,
					fmt.Sprintf(
						"dashboard binding %s references unavailable device template %s",
						binding.BindingKey,
						binding.DeviceTemplateKey,
					),
				)
			}
			displayName := binding.DisplayName
			if displayName == "" {
				displayName = mapping.LocalName
			}
			bindings = append(bindings, &model.LocalDashboardTemplateBinding{
				BindingKey:              binding.BindingKey,
				DisplayName:             displayName,
				Required:                binding.Required,
				AllowMany:               binding.AllowMany,
				DeviceTemplateKey:       binding.DeviceTemplateKey,
				LocalDeviceTemplateID:   mapping.LocalID,
				LocalDeviceTemplateName: mapping.LocalName,
			})
		}

		description := metadata.Description
		template := &model.LocalDashboardTemplate{
			TenantID:             tenantID,
			Name:                 dashboard.Name,
			Description:          description,
			Version:              dashboard.Version,
			Source:               model.DashboardTemplateSourceMarket,
			Status:               model.DashboardTemplateStatusReady,
			BundleKey:            req.BundleKey,
			BundleVersion:        req.Version,
			DashboardResourceKey: dashboard.ResourceKey,
			Snapshot:             snapshot,
		}
		if err := s.repo.CreateTemplateWithBindings(ctx, template, bindings); err != nil {
			// A concurrent retry may have won the unique constraint race.
			existing, findErr := s.repo.FindMarketTemplate(
				ctx,
				tenantID,
				req.BundleKey,
				req.Version,
				dashboard.ResourceKey,
			)
			if findErr != nil {
				return nil, dbError("save local dashboard template", err)
			}
			response.TemplateIDs = append(response.TemplateIDs, existing.ID)
			response.Reused++
			continue
		}
		response.TemplateIDs = append(response.TemplateIDs, template.ID)
		response.Downloaded++
	}
	return response, nil
}

func (s *DashboardTemplateService) validateDeviceTemplateMappings(
	ctx context.Context,
	tenantID string,
	bundle *model.HorizonBundleDownload,
	templateMappings []*model.ResourceMappingResponse,
) error {
	var resources model.BundleResources
	if err := json.Unmarshal(bundle.Resources, &resources); err != nil {
		return errcode.WithData(errcode.CodeParamError, "invalid device template resources: "+err.Error())
	}
	mappings := make(map[string]*model.ResourceMappingResponse, len(templateMappings))
	for _, mapping := range templateMappings {
		mappings[mapping.MarketResourceKey] = mapping
	}
	for _, deviceTemplate := range resources.DeviceTemplates {
		mapping := mappings[deviceTemplate.ResourceKey]
		if mapping == nil || mapping.LocalID == "" {
			return errcode.WithData(
				errcode.CodeParamError,
				"device template dependency is unavailable: "+deviceTemplate.ResourceKey,
			)
		}
		for _, field := range deviceTemplate.ThingModel {
			exists, err := s.repo.HasThingModelField(
				ctx,
				tenantID,
				mapping.LocalID,
				string(field.Kind),
				field.Identifier,
				field.DataType,
			)
			if err != nil {
				return dbError("validate local device template model", err)
			}
			if !exists {
				return errcode.WithData(
					errcode.CodeParamError,
					fmt.Sprintf(
						"local device template %s conflicts with market dependency: missing %s field %s",
						mapping.LocalName,
						field.Kind,
						field.Identifier,
					),
				)
			}
		}
	}
	return nil
}

func (s *DashboardTemplateService) List(
	ctx context.Context,
	tenantID string,
	req *model.ListLocalDashboardTemplatesRequest,
) (*model.ListLocalDashboardTemplatesResponse, error) {
	source := strings.ToUpper(strings.TrimSpace(req.Source))
	statusFilter := strings.ToUpper(strings.TrimSpace(req.Status))
	if source != "" && source != model.DashboardTemplateSourceMarket && source != model.DashboardTemplateSourceLocal {
		return nil, errcode.WithData(errcode.CodeParamError, "invalid dashboard template source")
	}
	if statusFilter != "" &&
		statusFilter != model.DashboardTemplateStatusReady &&
		statusFilter != model.DashboardTemplateStatusDisabled &&
		statusFilter != "MISSING_DEVICE" {
		return nil, errcode.WithData(errcode.CodeParamError, "invalid dashboard template status")
	}

	templates, err := s.repo.ListAll(ctx, tenantID, strings.TrimSpace(req.Keyword), source)
	if err != nil {
		return nil, dbError("list local dashboard templates", err)
	}

	filtered := make([]*model.LocalDashboardTemplateResponse, 0, len(templates))
	for _, template := range templates {
		item, err := s.buildTemplateResponse(ctx, tenantID, template)
		if err != nil {
			return nil, err
		}
		if statusFilter == "" || item.Status == statusFilter {
			filtered = append(filtered, item)
		}
	}

	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 12
	}
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return &model.ListLocalDashboardTemplatesResponse{List: []*model.LocalDashboardTemplateResponse{}, Total: int64(len(filtered))}, nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return &model.ListLocalDashboardTemplatesResponse{
		List:  filtered[start:end],
		Total: int64(len(filtered)),
	}, nil
}

func (s *DashboardTemplateService) buildTemplateResponse(
	ctx context.Context,
	tenantID string,
	template *model.LocalDashboardTemplate,
) (*model.LocalDashboardTemplateResponse, error) {
	bindings, err := s.repo.ListBindings(ctx, template.ID)
	if err != nil {
		return nil, dbError("list dashboard template bindings", err)
	}

	status := template.Status
	var compatibleCount int64
	for _, binding := range bindings {
		count, err := s.repo.CountCompatibleDevices(ctx, tenantID, binding.LocalDeviceTemplateID)
		if err != nil {
			return nil, dbError("count compatible devices", err)
		}
		compatibleCount += count
		if status != model.DashboardTemplateStatusDisabled && binding.Required && count == 0 {
			status = "MISSING_DEVICE"
		}
	}
	instanceCount, err := s.repo.CountInstances(ctx, tenantID, template.ID)
	if err != nil {
		return nil, dbError("count dashboard template instances", err)
	}

	return &model.LocalDashboardTemplateResponse{
		ID:                    template.ID,
		Name:                  template.Name,
		Description:           template.Description,
		Version:               template.Version,
		Source:                template.Source,
		Status:                status,
		BundleKey:             template.BundleKey,
		BundleVersion:         template.BundleVersion,
		DashboardResourceKey:  template.DashboardResourceKey,
		Thumbnail:             template.Thumbnail,
		Bindings:              bindings,
		CompatibleDeviceCount: compatibleCount,
		InstanceCount:         instanceCount,
		DownloadedAt:          template.DownloadedAt,
		UpdatedAt:             template.UpdatedAt,
	}, nil
}

func (s *DashboardTemplateService) CompatibleDevices(
	ctx context.Context,
	tenantID, templateID string,
) (*model.CompatibleDevicesResponse, error) {
	if _, err := s.getTenantTemplate(ctx, tenantID, templateID); err != nil {
		return nil, err
	}
	bindings, err := s.repo.ListBindings(ctx, templateID)
	if err != nil {
		return nil, dbError("list dashboard template bindings", err)
	}

	result := &model.CompatibleDevicesResponse{
		Bindings: make([]*model.CompatibleDeviceBinding, 0, len(bindings)),
	}
	for _, binding := range bindings {
		devices, err := s.repo.ListCompatibleDevices(ctx, tenantID, binding.LocalDeviceTemplateID)
		if err != nil {
			return nil, dbError("list compatible devices", err)
		}
		result.Bindings = append(result.Bindings, &model.CompatibleDeviceBinding{
			BindingKey:              binding.BindingKey,
			DisplayName:             binding.DisplayName,
			Required:                binding.Required,
			LocalDeviceTemplateID:   binding.LocalDeviceTemplateID,
			LocalDeviceTemplateName: binding.LocalDeviceTemplateName,
			Devices:                 devices,
		})
	}
	return result, nil
}

func (s *DashboardTemplateService) CreateInstance(
	ctx context.Context,
	tenantID, userID, templateID, authorization string,
	req *model.CreateDashboardTemplateInstanceRequest,
) (*model.CreateDashboardTemplateInstanceResponse, error) {
	template, err := s.getTenantTemplate(ctx, tenantID, templateID)
	if err != nil {
		return nil, err
	}
	if template.Status == model.DashboardTemplateStatusDisabled {
		return nil, errcode.WithData(errcode.CodeParamError, "dashboard template is disabled")
	}

	bindings, err := s.repo.ListBindings(ctx, templateID)
	if err != nil {
		return nil, dbError("list dashboard template bindings", err)
	}
	resolved, err := s.validateTemplateDeviceBindings(ctx, tenantID, bindings, req.DeviceBindings)
	if err != nil {
		return nil, err
	}

	var snapshot model.DashboardTemplateSnapshot
	if err := json.Unmarshal(template.Snapshot, &snapshot); err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "invalid local dashboard template snapshot")
	}
	resolvedDataSources, err := resolveDashboardDataSourceBindings(snapshot.DataSources, resolved)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "resolve dashboard device bindings: "+err.Error())
	}

	importResult, err := s.thingsVis.CreateDashboardFromSnapshot(ctx, authorization, req.Name, ThingsVisMarketSnapshot{
		Name:          req.Name,
		SchemaVersion: snapshot.SchemaVersion,
		CanvasConfig:  snapshot.CanvasConfig,
		Nodes:         snapshot.Nodes,
		DataSources:   resolvedDataSources,
		Variables:     snapshot.Variables,
	})
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, "create ThingsVis dashboard: "+err.Error())
	}

	bindingsJSON, _ := json.Marshal(req.DeviceBindings)
	instance := &model.LocalDashboardTemplateInstance{
		DashboardTemplateID: templateID,
		TenantID:            tenantID,
		DashboardID:         importResult.DashboardID,
		ProjectID:           importResult.ProjectID,
		Name:                req.Name,
		DeviceBindings:      bindingsJSON,
	}
	if err := s.repo.CreateInstance(ctx, instance); err != nil {
		_ = s.thingsVis.DeleteDashboardWithAuthorization(ctx, importResult.DashboardID, authorization)
		return nil, dbError("record dashboard template instance", err)
	}

	return &model.CreateDashboardTemplateInstanceResponse{
		DashboardID: importResult.DashboardID,
		ProjectID:   importResult.ProjectID,
		Name:        req.Name,
	}, nil
}

func resolveDashboardDataSourceBindings(
	raw json.RawMessage,
	resolved map[string]string,
) (json.RawMessage, error) {
	var dataSources []map[string]interface{}
	if err := json.Unmarshal(raw, &dataSources); err != nil {
		return nil, fmt.Errorf("invalid dataSources: %w", err)
	}

	for _, dataSource := range dataSources {
		config, ok := dataSource["config"].(map[string]interface{})
		if !ok {
			continue
		}
		deviceBinding, ok := config["deviceBinding"].(map[string]interface{})
		if !ok {
			continue
		}
		bindingKey, _ := deviceBinding["$deviceBinding"].(string)
		if bindingKey == "" {
			continue
		}
		deviceID := resolved[bindingKey]
		if deviceID == "" {
			continue
		}

		// Keep the data-source id unchanged because nodes and event actions
		// reference it. Only replace the portable market placeholder with the
		// runtime field consumed by ThingsVis.
		delete(config, "deviceBinding")
		config["deviceId"] = deviceID
	}

	return json.Marshal(dataSources)
}

func (s *DashboardTemplateService) getTenantTemplate(
	ctx context.Context,
	tenantID, templateID string,
) (*model.LocalDashboardTemplate, error) {
	template, err := s.repo.GetByID(ctx, tenantID, templateID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errcode.WithData(errcode.CodeNotFound, "dashboard template not found")
	}
	if err != nil {
		return nil, dbError("get local dashboard template", err)
	}
	return template, nil
}

func (s *DashboardTemplateService) validateTemplateDeviceBindings(
	ctx context.Context,
	tenantID string,
	expected []*model.LocalDashboardTemplateBinding,
	input []model.DeviceBindingInput,
) (map[string]string, error) {
	expectedByKey := make(map[string]*model.LocalDashboardTemplateBinding, len(expected))
	for _, binding := range expected {
		expectedByKey[binding.BindingKey] = binding
	}

	provided := make(map[string]string, len(input))
	for _, binding := range input {
		if _, exists := provided[binding.BindingKey]; exists {
			return nil, errcode.WithData(errcode.CodeParamError, "duplicate device binding: "+binding.BindingKey)
		}
		expectedBinding := expectedByKey[binding.BindingKey]
		if expectedBinding == nil {
			return nil, errcode.WithData(errcode.CodeParamError, "unknown device binding: "+binding.BindingKey)
		}

		compatible, err := s.repo.IsCompatibleDevice(
			ctx,
			tenantID,
			expectedBinding.LocalDeviceTemplateID,
			binding.LocalDeviceID,
		)
		if err != nil {
			return nil, dbError("validate compatible device", err)
		}
		if !compatible {
			return nil, errcode.WithData(
				errcode.CodeParamError,
				fmt.Sprintf(
					"device binding %s is not compatible with device template %s",
					binding.BindingKey,
					expectedBinding.LocalDeviceTemplateName,
				),
			)
		}
		provided[binding.BindingKey] = binding.LocalDeviceID
	}
	for _, binding := range expected {
		if binding.Required && provided[binding.BindingKey] == "" {
			return nil, errcode.WithData(errcode.CodeParamError, "device binding is required: "+binding.BindingKey)
		}
	}
	return provided, nil
}

func dbError(operation string, err error) error {
	return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
		"error": operation + ": " + err.Error(),
	})
}

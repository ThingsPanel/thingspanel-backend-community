package service

import (
	"context"
	"fmt"

	"project/internal/model"
)

// MarketBundleCatalogService adapts Horizon's public catalog to ThingsPanel DTOs.
type MarketBundleCatalogService struct {
	client *MarketClient
}

func NewMarketBundleCatalogService() *MarketBundleCatalogService {
	return &MarketBundleCatalogService{client: NewMarketClient()}
}

func (s *MarketBundleCatalogService) List(ctx context.Context, query model.MarketBundleListQuery) (*model.MarketBundleListResult, error) {
	return s.client.ListMarketBundles(ctx, query)
}

func (s *MarketBundleCatalogService) Detail(ctx context.Context, bundleKey string) (*model.MarketBundleDetail, error) {
	catalog, err := s.client.GetMarketBundleCatalog(ctx, bundleKey)
	if err != nil {
		return nil, err
	}
	return mapMarketBundleDetail(catalog), nil
}

func (s *MarketBundleCatalogService) Precheck(ctx context.Context, bundleKey, version string) (*model.MarketBundlePrecheck, error) {
	catalog, err := s.client.GetMarketBundleCatalog(ctx, bundleKey)
	if err != nil {
		return nil, err
	}
	selected, err := selectCatalogVersion(catalog, version)
	if err != nil {
		return nil, err
	}

	templateNames := marketTemplateNames(selected.ResourceSummary.DeviceTemplates)
	preview := make([]model.MarketDashboardBindingSpec, 0, len(selected.ResourceSummary.Dashboards))
	for _, dashboard := range selected.ResourceSummary.Dashboards {
		bindings := make([]model.MarketDeviceBindingSpec, 0, len(dashboard.DeviceBindings))
		for _, binding := range dashboard.DeviceBindings {
			bindings = append(bindings, mapMarketBinding(binding, templateNames))
		}
		preview = append(preview, model.MarketDashboardBindingSpec{
			DashboardKey:  dashboard.ResourceKey,
			DashboardName: dashboard.Name,
			Bindings:      bindings,
		})
	}

	return &model.MarketBundlePrecheck{
		Warnings:        []string{},
		RequiredPlugins: mapMarketPlugins(selected.Compatibility.Plugins),
		BindingPreview:  preview,
	}, nil
}

func mapMarketBundleDetail(catalog *model.HorizonBundleCatalog) *model.MarketBundleDetail {
	versions := make([]model.MarketBundleDetailVersion, 0, len(catalog.Versions))
	for _, version := range catalog.Versions {
		templateNames := marketTemplateNames(version.ResourceSummary.DeviceTemplates)
		bindings := make([]model.MarketDeviceBindingSpec, 0)
		for _, dashboard := range version.ResourceSummary.Dashboards {
			for _, binding := range dashboard.DeviceBindings {
				bindings = append(bindings, mapMarketBinding(binding, templateNames))
			}
		}
		versions = append(versions, model.MarketBundleDetailVersion{
			Version:             version.Version,
			PublishedAt:         version.PublishedAt,
			ContentHash:         version.ContentHash,
			DeviceTemplateCount: len(version.ResourceSummary.DeviceTemplates),
			DashboardCount:      len(version.ResourceSummary.Dashboards),
			ContentSize:         version.FileSizeBytes,
			Compatibility: model.MarketBundleCompatibility{
				MinPlatformVersion: version.Compatibility.MinThingsPanel,
				MinThingsVis:       version.Compatibility.MinThingsVis,
				RequiredPlugins:    mapMarketPlugins(version.Compatibility.Plugins),
			},
			DeviceBindings: bindings,
		})
	}

	return &model.MarketBundleDetail{
		BundleKey:     catalog.Bundle.BundleKey,
		Name:          catalog.Bundle.Name,
		Description:   catalog.Bundle.Description,
		Category:      catalog.Bundle.Category,
		Author:        catalog.Bundle.Author,
		Thumbnail:     catalog.Bundle.CoverAssetKey,
		Versions:      versions,
		TotalInstalls: catalog.Bundle.InstallCount,
	}
}

func selectCatalogVersion(catalog *model.HorizonBundleCatalog, version string) (*model.HorizonBundleCatalogVersion, error) {
	target := version
	if target == "" {
		target = catalog.Bundle.LatestVersion
	}
	for index := range catalog.Versions {
		if catalog.Versions[index].Version == target {
			return &catalog.Versions[index], nil
		}
	}
	return nil, fmt.Errorf("published bundle version %q not found", target)
}

func marketTemplateNames(input []model.HorizonBundleDeviceTemplateSummary) map[string]string {
	result := make(map[string]string, len(input))
	for _, template := range input {
		result[template.ResourceKey] = template.Name
	}
	return result
}

func mapMarketBinding(binding model.HorizonBundleDeviceBindingSummary, templateNames map[string]string) model.MarketDeviceBindingSpec {
	displayName := binding.DisplayName
	if displayName == "" {
		displayName = templateNames[binding.DeviceTemplateKey]
	}
	if displayName == "" {
		displayName = binding.DeviceTemplateKey
	}
	return model.MarketDeviceBindingSpec{
		BindingKey:         binding.BindingKey,
		DisplayName:        displayName,
		Required:           binding.Required,
		AllowMany:          binding.AllowMany,
		DeviceTemplateKey:  binding.DeviceTemplateKey,
		DeviceTemplateName: templateNames[binding.DeviceTemplateKey],
	}
}

func mapMarketPlugins(input []model.HorizonBundlePluginDependency) []model.MarketRequiredPlugin {
	result := make([]model.MarketRequiredPlugin, 0, len(input))
	for _, plugin := range input {
		result = append(result, model.MarketRequiredPlugin{
			Key:       plugin.Identifier,
			Name:      plugin.Identifier,
			Version:   plugin.MinVersion,
			Installed: false,
		})
	}
	return result
}

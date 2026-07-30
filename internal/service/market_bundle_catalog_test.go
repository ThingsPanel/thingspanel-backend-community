package service

import (
	"testing"

	"project/internal/model"
)

func TestMapMarketBundleDetailPreservesBindingIdentity(t *testing.T) {
	catalog := &model.HorizonBundleCatalog{
		Bundle: model.HorizonBundleCatalogItem{
			BundleKey:     "temperature-dashboard",
			LatestVersion: "1.0.0",
			Name:          "Temperature Dashboard",
			InstallCount:  7,
		},
		Versions: []model.HorizonBundleCatalogVersion{
			{
				Version: "1.0.0",
				ResourceSummary: model.HorizonBundleResourceSummary{
					DeviceTemplates: []model.HorizonBundleDeviceTemplateSummary{
						{ResourceKey: "temperature-sensor", Name: "Temperature Sensor"},
					},
					Dashboards: []model.HorizonBundleDashboardSummary{
						{
							ResourceKey: "main",
							Name:        "Main Dashboard",
							DeviceBindings: []model.HorizonBundleDeviceBindingSummary{
								{
									BindingKey:        "room_sensor",
									DisplayName:       "Room Sensor",
									DeviceTemplateKey: "temperature-sensor",
									Required:          true,
								},
							},
						},
					},
				},
			},
		},
	}

	detail := mapMarketBundleDetail(catalog)
	if detail.BundleKey != "temperature-dashboard" || len(detail.Versions) != 1 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	binding := detail.Versions[0].DeviceBindings[0]
	if binding.BindingKey != "room_sensor" {
		t.Fatalf("bindingKey = %q, want room_sensor", binding.BindingKey)
	}
	if binding.DeviceTemplateName != "Temperature Sensor" {
		t.Fatalf("deviceTemplateName = %q, want Temperature Sensor", binding.DeviceTemplateName)
	}
}

func TestSelectCatalogVersionUsesLatestVersion(t *testing.T) {
	catalog := &model.HorizonBundleCatalog{
		Bundle: model.HorizonBundleCatalogItem{LatestVersion: "2.0.0"},
		Versions: []model.HorizonBundleCatalogVersion{
			{Version: "1.0.0"},
			{Version: "2.0.0"},
		},
	}

	selected, err := selectCatalogVersion(catalog, "")
	if err != nil {
		t.Fatalf("selectCatalogVersion() error = %v", err)
	}
	if selected.Version != "2.0.0" {
		t.Fatalf("selected version = %q, want 2.0.0", selected.Version)
	}
}

package model

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestPublishDashboardBundleRequestAllowsNoDeviceRoles(t *testing.T) {
	req := PublishDashboardBundleRequest{
		DashboardID: "dashboard-1",
		BundleKey:   "dashboard-one",
		Version:     "1.0.0",
		Name:        "Tenant overview",
		Category:    "other",
		MarketToken: "market-token",
		DeviceRoles: nil,
	}

	if err := validator.New().Struct(req); err != nil {
		t.Fatalf("zero-device dashboard request should be valid: %v", err)
	}
}

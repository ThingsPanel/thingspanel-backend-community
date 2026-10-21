package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fakeLegacyStore struct {
	tenants   []string
	templates map[string][]LegacyTemplate
	fields    map[string][]LegacyField
}

func (f *fakeLegacyStore) ListTenants(context.Context) ([]string, error) { return f.tenants, nil }
func (f *fakeLegacyStore) ListTemplates(_ context.Context, tenantID string) ([]LegacyTemplate, error) {
	return f.templates[tenantID], nil
}
func (f *fakeLegacyStore) ListTemplateFields(_ context.Context, tenantID, templateID string) ([]LegacyField, error) {
	return f.fields[tenantID+":"+templateID], nil
}

func TestMigrationDryRunReport(t *testing.T) {
	store := &fakeLegacyStore{
		tenants: []string{"t1"},
		templates: map[string][]LegacyTemplate{
			"t1": {{ID: "tpl-1", TenantID: "t1", Name: "Air Sensor"}},
		},
		fields: map[string][]LegacyField{
			"t1:tpl-1": {
				{Identifier: "temperature", Type: "PROPERTY", ReadWrite: "R", ValueType: json.RawMessage(`{"kind":"FLOAT"}`)},
				{Identifier: "temperature", Type: "EVENT", ReadWrite: "", ValueType: json.RawMessage(`{"kind":"FLOAT"}`)},
			},
		},
	}
	report, err := NewMigrator(store).DryRun(context.Background())
	if err != nil {
		t.Fatalf("DryRun failed: %v", err)
	}
	if report.Summary.Tenants != 1 || report.Summary.Templates != 1 || report.Summary.Items != 2 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if len(report.Renames) != 1 || report.Renames[0].To != "temperature_event" {
		t.Fatalf("unexpected renames: %#v", report.Renames)
	}
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "migration_report.json")
	if err := report.WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
}

func TestApplyChartConfig(t *testing.T) {
	items := []MigratedItem{{Identifier: "temperature"}}
	cfg := `{"layout":[{"field":"temperature","widget":"line","options":{"unit":"°C"}}]}`
	items = applyChartConfig(items, cfg, "web")
	if len(items[0].WebChartConfig) == 0 {
		t.Fatal("expected web chart config to be populated")
	}
}

func TestBuildValidationReport(t *testing.T) {
	expected := map[string][]string{"SN1": {"temperature", "humidity"}, "SN2": {"power"}}
	actual := map[string][]string{"SN1": {"temperature", "humidity"}, "SN2": {"power_command"}}
	report := buildValidationReport(expected, actual)
	if report.SampleSize != 2 {
		t.Fatalf("unexpected sample size: %d", report.SampleSize)
	}
	if report.MatchRate != 1 {
		t.Fatalf("expected match rate 1, got %f", report.MatchRate)
	}
}

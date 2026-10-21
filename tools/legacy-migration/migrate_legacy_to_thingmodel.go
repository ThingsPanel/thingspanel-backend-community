package main

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

const (
	modeDryRun = "dry-run"
	modeReal   = "real"
)

type LegacyStore interface {
	ListTenants(ctx context.Context) ([]string, error)
	ListTemplates(ctx context.Context, tenantID string) ([]LegacyTemplate, error)
	ListTemplateFields(ctx context.Context, tenantID, templateID string) ([]LegacyField, error)
}

type Migrator struct {
	legacy LegacyStore
}

func NewMigrator(legacy LegacyStore) *Migrator {
	return &Migrator{legacy: legacy}
}

func (m *Migrator) DryRun(ctx context.Context) (*MigrationReport, error) {
	report := &MigrationReport{Mode: modeDryRun}
	tenants, err := m.legacy.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	report.Summary.Tenants = len(tenants)
	report.EstimatedRealRunTimeMin = len(tenants) * 2

	for _, tenantID := range tenants {
		templates, err := m.legacy.ListTemplates(ctx, tenantID)
		if err != nil {
			report.Issues = append(report.Issues, MigrationIssue{TenantID: tenantID, Issue: err.Error()})
			continue
		}
		report.Summary.Templates += len(templates)
		for _, tpl := range templates {
			fields, err := m.legacy.ListTemplateFields(ctx, tenantID, tpl.ID)
			if err != nil {
				report.Issues = append(report.Issues, MigrationIssue{TenantID: tenantID, TemplateID: tpl.ID, Issue: err.Error()})
				continue
			}
			items := make([]MigratedItem, 0, len(fields))
			for _, field := range fields {
				items = append(items, MigratedItem{
					Identifier: field.Identifier,
					Type:       field.Type,
					ValueType:  field.ValueType,
					Access:     deriveAccess(field.Type, field.ReadWrite),
					NameI18n:   field.NameI18n,
				})
			}
			items = dedupeIdentifiers(items, report)
			items = applyChartConfig(items, tpl.WebChartConfig, "web")
			items = applyChartConfig(items, tpl.AppChartConfig, "app")
			report.Summary.Items += len(items)
		}
	}
	return report, nil
}

func (r *MigrationReport) WriteJSON(path string) error {
	r.EstimatedRealRunTimeMin = max(r.EstimatedRealRunTimeMin, 1)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx
}

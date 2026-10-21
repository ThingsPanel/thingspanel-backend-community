package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type LegacyTemplate struct {
	ID             string
	TenantID       string
	Name           string
	Brand          string
	ModelNumber    string
	WebChartConfig string
	AppChartConfig string
	AdditionalInfo json.RawMessage
}

type LegacyField struct {
	Identifier string
	Type       string
	ReadWrite  string
	ValueType  json.RawMessage
	NameI18n   json.RawMessage
}

type MigrationIssue struct {
	TenantID   string `json:"tenant_id,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	Issue      string `json:"issue"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
}

type RenamedIdentifier struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type ReportSummary struct {
	Tenants   int `json:"tenants"`
	Templates int `json:"templates"`
	Items     int `json:"items"`
	Devices   int `json:"devices"`
}

type MigrationReport struct {
	Mode                    string              `json:"mode"`
	Summary                 ReportSummary       `json:"summary"`
	Issues                  []MigrationIssue    `json:"issues,omitempty"`
	Warnings                []string            `json:"warnings,omitempty"`
	Renames                 []RenamedIdentifier `json:"renames,omitempty"`
	EstimatedRealRunTimeMin int                 `json:"estimated_real_run_time_min"`
}

type MigratedItem struct {
	Identifier     string          `json:"identifier"`
	Type           string          `json:"type"`
	ValueType      json.RawMessage `json:"value_type"`
	Access         json.RawMessage `json:"access"`
	WebChartConfig json.RawMessage `json:"web_chart_config,omitempty"`
	AppChartConfig json.RawMessage `json:"app_chart_config,omitempty"`
	NameI18n       json.RawMessage `json:"name_i18n,omitempty"`
}

func dedupeIdentifiers(items []MigratedItem, report *MigrationReport) []MigratedItem {
	seen := make(map[string]string, len(items))
	for i := range items {
		id := items[i].Identifier
		if _, exists := seen[id]; exists {
			newID := fmt.Sprintf("%s_%s", id, strings.ToLower(items[i].Type))
			report.Renames = append(report.Renames, RenamedIdentifier{From: id, To: newID, Type: items[i].Type})
			report.Issues = append(report.Issues, MigrationIssue{Issue: "identifier rename", From: id, To: newID})
			items[i].Identifier = newID
			seen[newID] = items[i].Type
			continue
		}
		seen[id] = items[i].Type
	}
	return items
}

func deriveAccess(itemType, readWriteFlag string) json.RawMessage {
	out := map[string]bool{}
	switch strings.ToUpper(itemType) {
	case "PROPERTY":
		switch strings.ToUpper(readWriteFlag) {
		case "W":
			out["write"] = true
		case "RW":
			out["read"] = true
			out["write"] = true
			out["observe"] = true
		default:
			out["read"] = true
			out["observe"] = true
		}
	case "EVENT":
		out["observe"] = true
	case "COMMAND":
		out["write"] = true
	}
	raw, _ := json.Marshal(out)
	return raw
}

func applyChartConfig(items []MigratedItem, configJSON string, platform string) []MigratedItem {
	if strings.TrimSpace(configJSON) == "" {
		return items
	}
	var cfg struct {
		Layout []struct {
			Field   string         `json:"field"`
			Widget  string         `json:"widget"`
			Options map[string]any `json:"options"`
		} `json:"layout"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return items
	}
	for _, entry := range cfg.Layout {
		payload, _ := json.Marshal(map[string]any{
			"default_widgets": []map[string]any{{
				"kind":    entry.Widget,
				"options": entry.Options,
			}},
		})
		for i := range items {
			if items[i].Identifier != entry.Field {
				continue
			}
			if platform == "web" {
				items[i].WebChartConfig = payload
			} else {
				items[i].AppChartConfig = payload
			}
		}
	}
	return items
}

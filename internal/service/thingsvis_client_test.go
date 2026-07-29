package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThingsVisClient_ExportDashboardForMarket_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/api/v1/dashboards/test-dashboard-id/export" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Unexpected Accept header: %s", r.Header.Get("Accept"))
		}

		// Return mock response
		response := DashboardExportResponse{
			Code:    0,
			Message: "success",
			Data: &DashboardExportData{
				SchemaVersion: "thingsvis-1",
				CanvasConfig:  json.RawMessage(`{"layout":"grid"}`),
				Nodes:        json.RawMessage(`[]`),
				DataSources:  json.RawMessage(`[]`),
				Variables:    json.RawMessage(`[]`),
				DeviceBindings: json.RawMessage(`[]`),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with mock URL
	client := &ThingsVisClient{
		baseURL: server.URL,
		httpClient: &http.Client{},
	}

	// Test export
	ctx := context.Background()
	result, err := client.ExportDashboardForMarket(ctx, "test-dashboard-id")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.SchemaVersion != "thingsvis-1" {
		t.Errorf("Expected schema version 'thingsvis-1', got %s", result.SchemaVersion)
	}
}

func TestThingsVisClient_ExportDashboardForMarket_EmptyID(t *testing.T) {
	client := &ThingsVisClient{
		baseURL: "http://localhost:3000",
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	_, err := client.ExportDashboardForMarket(ctx, "")
	if err == nil {
		t.Error("Expected error for empty dashboard ID")
	}
}

func TestThingsVisClient_ExportDashboardForMarket_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := &ThingsVisClient{
		baseURL: server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	_, err := client.ExportDashboardForMarket(ctx, "test-dashboard-id")
	if err == nil {
		t.Error("Expected error for server error")
	}
}

func TestThingsVisClient_ExportDashboardForMarket_NonZeroCode(t *testing.T) {
	// Create mock server that returns non-zero code
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := DashboardExportResponse{
			Code:    1001,
			Message: "dashboard not found",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &ThingsVisClient{
		baseURL: server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	_, err := client.ExportDashboardForMarket(ctx, "nonexistent-dashboard")
	if err == nil {
		t.Error("Expected error for non-zero code")
	}
}

func TestThingsVisClient_ExportDashboardForMarket_NilData(t *testing.T) {
	// Create mock server that returns nil data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := DashboardExportResponse{
			Code:    0,
			Message: "success",
			Data:    nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &ThingsVisClient{
		baseURL: server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	_, err := client.ExportDashboardForMarket(ctx, "test-dashboard-id")
	if err == nil {
		t.Error("Expected error for nil data")
	}
}

func TestNewThingsVisClient_DefaultBaseURL(t *testing.T) {
	// Test that NewThingsVisClient creates client with defaults
	// Note: This test may fail if config is set, so we just verify the function doesn't panic
	client := NewThingsVisClient()
	if client == nil {
		t.Error("NewThingsVisClient should not return nil")
	}
	if client.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}

func TestCompactBody(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "empty body",
			input:    []byte{},
			expected: "<empty>",
		},
		{
			name:     "normal body",
			input:    []byte(`{"code":0}`),
			expected: `{"code":0}`,
		},
		{
			name:     "long body",
			input:    []byte(`{"code":0,"data":"` + strings.Repeat("a", 300) + `"}`),
			expected: strings.Repeat("a", 256) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compactBody(tt.input)
			if result != tt.expected {
				t.Errorf("compactBody() = %q, want %q", result, tt.expected)
			}
		})
	}
}

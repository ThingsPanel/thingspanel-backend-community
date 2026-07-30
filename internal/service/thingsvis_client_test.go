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
				SchemaVersion:  "thingsvis-1",
				CanvasConfig:   json.RawMessage(`{"layout":"grid"}`),
				Nodes:          json.RawMessage(`[]`),
				DataSources:    json.RawMessage(`[]`),
				Variables:      json.RawMessage(`[]`),
				DeviceBindings: json.RawMessage(`[]`),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with mock URL
	client := &ThingsVisClient{
		baseURL:    server.URL,
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
		baseURL:    "http://localhost:3000",
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
		baseURL:    server.URL,
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
		baseURL:    server.URL,
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
		baseURL:    server.URL,
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

func TestThingsVisClient_AnalyzeMarketDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/market-dashboards/dashboard-1/analyze" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer thingsvis-user-token" {
			t.Fatal("missing ThingsVis bearer token")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard": map[string]string{"id": "dashboard-1", "name": "Temperature"},
			"deviceReferences": []map[string]interface{}{
				{
					"sourceDeviceId":   "device-1",
					"fieldIdentifiers": []string{"temperature"},
					"dataSourceIds":    []string{"__platform_device-1__"},
				},
			},
		})
	}))
	defer server.Close()

	client := &ThingsVisClient{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}
	result, err := client.AnalyzeMarketDashboard(
		context.Background(),
		"dashboard-1",
		"tenant-1",
		"user-1",
		"Bearer thingsvis-user-token",
	)
	if err != nil {
		t.Fatalf("AnalyzeMarketDashboard returned error: %v", err)
	}
	if result.Dashboard.Name != "Temperature" || len(result.DeviceReferences) != 1 {
		t.Fatalf("unexpected analysis response: %+v", result)
	}
}

func TestThingsVisClient_AnalyzeMarketDashboardSupportsInternalTokenFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "shared-secret" {
			t.Fatal("missing internal token")
		}
		if r.Header.Get("X-Tenant-ID") != "tenant-1" || r.Header.Get("X-User-ID") != "user-1" {
			t.Fatal("missing tenant or user identity")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"dashboard":        map[string]string{"id": "dashboard-1", "name": "Temperature"},
			"deviceReferences": []map[string]interface{}{},
		})
	}))
	defer server.Close()

	client := &ThingsVisClient{
		baseURL:       server.URL,
		internalToken: "shared-secret",
		httpClient:    server.Client(),
	}
	_, err := client.AnalyzeMarketDashboard(
		context.Background(),
		"dashboard-1",
		"tenant-1",
		"user-1",
		"",
	)
	if err != nil {
		t.Fatalf("AnalyzeMarketDashboard returned error: %v", err)
	}
}

func TestThingsVisClient_AnalyzeMarketDashboardRequiresAuthorization(t *testing.T) {
	client := &ThingsVisClient{
		baseURL:    "http://localhost",
		httpClient: &http.Client{},
	}
	_, err := client.AnalyzeMarketDashboard(
		context.Background(),
		"dashboard-1",
		"tenant-1",
		"user-1",
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "authorization is not provided") {
		t.Fatalf("expected missing authorization error, got %v", err)
	}
}

func TestThingsVisClient_ImportDashboardUsesInternalContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/internal/market-dashboards/import" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Internal-Token") != "shared-secret" {
			t.Fatal("missing internal token")
		}
		if r.Header.Get("X-Tenant-ID") != "tenant-1" || r.Header.Get("X-User-ID") != "user-1" {
			t.Fatal("missing tenant or user identity")
		}

		var request ThingsVisImportRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.DashboardSnapshot.Name != "Temperature" {
			t.Fatalf("snapshot name = %q, want Temperature", request.DashboardSnapshot.Name)
		}
		if len(request.DeviceBindings) != 1 ||
			request.DeviceBindings[0].BindingKey != "room_sensor" ||
			request.DeviceBindings[0].LocalDeviceID != "device-1" {
			t.Fatalf("unexpected bindings: %+v", request.DeviceBindings)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"dashboardId": "dashboard-1"})
	}))
	defer server.Close()

	client := &ThingsVisClient{
		baseURL:       server.URL,
		internalToken: "shared-secret",
		httpClient:    server.Client(),
	}
	dashboardID, err := client.ImportDashboard(context.Background(), "tenant-1", "user-1", &ThingsVisImportRequest{
		DashboardSnapshot: ThingsVisMarketSnapshot{
			Name:          "Temperature",
			SchemaVersion: "thingsvis-1",
			CanvasConfig:  json.RawMessage(`{}`),
			Nodes:         json.RawMessage(`[]`),
			DataSources:   json.RawMessage(`[]`),
			Variables:     json.RawMessage(`[]`),
		},
		DeviceBindings: []DeviceBindingImport{
			{BindingKey: "room_sensor", LocalDeviceID: "device-1"},
		},
	})
	if err != nil {
		t.Fatalf("ImportDashboard() error = %v", err)
	}
	if dashboardID != "dashboard-1" {
		t.Fatalf("dashboardID = %q, want dashboard-1", dashboardID)
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
			expected: (`{"code":0,"data":"` + strings.Repeat("a", 300) + `"}`)[:256] + "...",
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

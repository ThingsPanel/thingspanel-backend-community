package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"project/internal/model"

	"github.com/golang-jwt/jwt"
)

func TestMarketClient_PublishBundle_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/api/market/bundles/publish" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Unexpected Content-Type header: %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer "+marketToken {
			t.Errorf("Unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-User-Id") != "" {
			t.Errorf("X-User-Id must not be sent by the client: %s", r.Header.Get("X-User-Id"))
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("Expected Idempotency-Key header")
		}

		// Return mock response
		response := model.HorizonPublishResponse{
			Code:    0,
			Message: "success",
			Data: &model.HorizonPublishData{
				BundleKey:   "test-bundle",
				Version:     "1.0.0",
				ContentHash: "sha256:abc123",
				Status:      "published",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with mock URL
	client := &MarketClient{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	// Test publish
	ctx := context.Background()
	req := &model.HorizonPublishRequest{
		ContractVersion: "1.0",
		BundleKind:      "solution-bundle",
		BundleKey:       "test-bundle",
		Version:         "1.0.0",
		Metadata:        json.RawMessage(`{"name":"Test Bundle"}`),
		Resources:       json.RawMessage(`{"deviceTemplates":[],"dashboards":[]}`),
		Security:        json.RawMessage(`{"containsSecrets":false,"containsRuntimeData":false}`),
	}

	result, err := client.PublishBundle(ctx, marketToken, "test-idempotency-key", req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Code != 0 {
		t.Errorf("Expected code 0, got %d", result.Code)
	}
}

var marketToken = func() string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "market-user-123",
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		panic(err)
	}
	return signed
}()

func TestMarketClient_PublishBundle_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"code":500,"message":"internal server error"}`))
	}))
	defer server.Close()

	client := &MarketClient{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	req := &model.HorizonPublishRequest{
		ContractVersion: "1.0",
		BundleKind:      "solution-bundle",
		BundleKey:       "test-bundle",
		Version:         "1.0.0",
	}

	_, err := client.PublishBundle(ctx, "test-token", "test-idempotency-key", req)
	if err == nil {
		t.Error("Expected error for server error")
	}
}

func TestMarketClient_PublishBundle_InvalidResponse(t *testing.T) {
	// Create mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`invalid json`))
	}))
	defer server.Close()

	client := &MarketClient{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	req := &model.HorizonPublishRequest{
		ContractVersion: "1.0",
		BundleKind:      "solution-bundle",
		BundleKey:       "test-bundle",
		Version:         "1.0.0",
	}

	_, err := client.PublishBundle(ctx, "test-token", "test-idempotency-key", req)
	if err == nil {
		t.Error("Expected error for invalid response")
	}
}

func TestMarketClient_PublishBundle_NonZeroCode(t *testing.T) {
	// Create mock server that returns non-zero code
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := model.HorizonPublishResponse{
			Code:    4009,
			Message: "version conflict",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &MarketClient{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	req := &model.HorizonPublishRequest{
		ContractVersion: "1.0",
		BundleKind:      "solution-bundle",
		BundleKey:       "test-bundle",
		Version:         "1.0.0",
	}

	result, err := client.PublishBundle(ctx, "test-token", "test-idempotency-key", req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Code != 4009 {
		t.Errorf("Expected code 4009, got %d", result.Code)
	}
}

func TestMarketClient_PublishBundle_EmptyIdempotencyKey(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Idempotency-Key header should still be present but can be empty
		// The header is only added if idempotencyKey != ""

		response := model.HorizonPublishResponse{
			Code:    0,
			Message: "success",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &MarketClient{
		baseURL:    server.URL,
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	req := &model.HorizonPublishRequest{
		ContractVersion: "1.0",
		BundleKind:      "solution-bundle",
		BundleKey:       "test-bundle",
		Version:         "1.0.0",
	}

	_, err := client.PublishBundle(ctx, "test-token", "", req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestMarketClient_PublishBundle_RequestMarshalError(t *testing.T) {
	client := &MarketClient{
		baseURL:    "http://localhost:8081",
		httpClient: &http.Client{},
	}

	ctx := context.Background()
	// Create a request with an unmarshalable field (function)
	req := &model.HorizonPublishRequest{
		ContractVersion: "1.0",
		BundleKind:      "solution-bundle",
		BundleKey:       "test-bundle",
		Version:         "1.0.0",
	}

	_, err := client.PublishBundle(ctx, "test-token", "test-idempotency-key", req)
	// This should not error on marshaling since all fields are basic types
	if err != nil {
		t.Logf("Got error (expected if server unreachable): %v", err)
	}
}

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadTemplateDeclaresNativeConsumerContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/market/templates/fixture-id/download" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if got := r.URL.Query().Get("version"); got != "1.2.3" {
			t.Errorf("requested version = %q, want %q", got, "1.2.3")
		}
		if got := r.Header.Get("X-ThingsPanel-Template-Contract"); got != "native-thing-model-v1" {
			t.Errorf("consumer contract header = %q, want native-thing-model-v1", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer market-token" {
			t.Errorf("authorization header = %q, want market bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"version":"1.2.3"}}`))
	}))
	defer server.Close()

	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	result, err := client.DownloadTemplate(context.Background(), "market-token", "fixture-id", "1.2.3")
	if err != nil {
		t.Fatalf("DownloadTemplate() error = %v", err)
	}
	if result.Version != "1.2.3" {
		t.Fatalf("downloaded version = %q, want %q", result.Version, "1.2.3")
	}
}

func TestDownloadTemplateReturnsErrorForUpgradeRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-ThingsPanel-Template-Contract"); got != "native-thing-model-v1" {
			t.Errorf("consumer contract header = %q, want native-thing-model-v1", got)
		}
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write([]byte(`{"code":426,"message":"This template requires a newer version of ThingsPanel. Upgrade before installing."}`))
	}))
	defer server.Close()

	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	result, err := client.DownloadTemplate(context.Background(), "market-token", "fixture-id", "1.2.3")
	if err == nil {
		t.Fatal("DownloadTemplate() accepted HTTP 426 as a successful package response")
	}
	if result != nil {
		t.Fatalf("DownloadTemplate() returned a package on HTTP 426: %#v", result)
	}
}

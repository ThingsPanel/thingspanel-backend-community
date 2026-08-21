package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"project/internal/model"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R'}

func TestDownloadMarketTemplateImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/market/templates/assets/covers/template/cover.png" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testPNG)
	}))
	defer server.Close()

	storageDir := t.TempDir()
	client := &MarketClient{baseURL: server.URL, httpClient: &http.Client{Timeout: time.Second}}
	localURL, diskPath, err := downloadMarketTemplateImage(
		context.Background(), client,
		server.URL+"/api/market/templates/assets/covers/template/cover.png",
		storageDir,
	)
	if err != nil {
		t.Fatalf("downloadMarketTemplateImage() error = %v", err)
	}
	if !strings.HasPrefix(localURL, "./files/deviceConfig/") || !strings.HasSuffix(localURL, ".png") {
		t.Fatalf("unexpected local URL %q", localURL)
	}
	if filepath.Ext(diskPath) != ".png" {
		t.Fatalf("unexpected disk path %q", diskPath)
	}
	data, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read localized image: %v", err)
	}
	if string(data) != string(testPNG) {
		t.Fatalf("localized image content differs")
	}
	entries, err := os.ReadDir(filepath.Join(storageDir, ".staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging directory was not cleaned: entries=%d err=%v", len(entries), err)
	}
}

func TestLocalizeMarketTemplateImageWithoutCover(t *testing.T) {
	client := &MarketClient{baseURL: "https://resources.example.com", httpClient: &http.Client{}}
	tests := []struct {
		name string
		data *model.MarketTemplateFullData
	}{
		{name: "nil payload", data: nil},
		{name: "empty cover", data: &model.MarketTemplateFullData{}},
		{
			name: "legacy publisher image is not downloaded",
			data: &model.MarketTemplateFullData{DeviceConfig: &model.DeviceConfigPayload{
				ImageURL: "https://publisher.example/files/legacy.png",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localURL, diskPath, err := localizeMarketTemplateImage(context.Background(), client, tt.data)
			if err != nil {
				t.Fatalf("localizeMarketTemplateImage() error = %v", err)
			}
			if localURL != nil || diskPath != "" {
				t.Fatalf("expected no localized image, got url=%v path=%q", localURL, diskPath)
			}
		})
	}
}

func TestDownloadMarketTemplateImageRejectsUntrustedOrigin(t *testing.T) {
	client := &MarketClient{baseURL: "https://resources.example.com", httpClient: &http.Client{}}
	_, _, err := downloadMarketTemplateImage(
		context.Background(), client,
		"https://publisher.example/api/market/templates/assets/covers/cover.png",
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "not hosted by the configured resource center") {
		t.Fatalf("expected untrusted origin error, got %v", err)
	}
}

func TestValidateMarketTemplateAssetURLAllowsEquivalentLoopbackHost(t *testing.T) {
	err := validateMarketTemplateAssetURL(
		"http://127.0.0.1:18000",
		"http://localhost:18000/api/market/templates/assets/covers/cover.png",
	)
	if err != nil {
		t.Fatalf("expected equivalent loopback hosts to be allowed, got %v", err)
	}
}

func TestDownloadMarketTemplateImageRejectsInvalidContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	_, _, err := downloadMarketTemplateImage(
		context.Background(), client,
		server.URL+"/api/market/templates/assets/covers/cover.png",
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported market cover content type") {
		t.Fatalf("expected invalid content error, got %v", err)
	}
}

func TestDownloadMarketTemplateImageRejectsOversizeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "5242881")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	_, _, err := downloadMarketTemplateImage(
		context.Background(), client,
		server.URL+"/api/market/templates/assets/covers/cover.png",
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversize error, got %v", err)
	}
}

func TestDownloadMarketTemplateImageRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(testPNG)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/market/templates/assets/covers/cover.png", http.StatusFound)
	}))
	defer source.Close()

	client := &MarketClient{baseURL: source.URL, httpClient: source.Client()}
	_, _, err := downloadMarketTemplateImage(
		context.Background(), client,
		source.URL+"/api/market/templates/assets/covers/cover.png",
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "not hosted by the configured resource center") {
		t.Fatalf("expected cross-origin redirect error, got %v", err)
	}
}

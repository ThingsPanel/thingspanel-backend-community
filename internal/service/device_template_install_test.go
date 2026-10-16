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

func TestMarketTemplateInstallLockKeyIsStableAndTenantScoped(t *testing.T) {
	key := marketTemplateInstallLockKey("tenant-a", "开关")
	if key != marketTemplateInstallLockKey("tenant-a", "开关") {
		t.Fatal("same tenant/template should use the same advisory lock key")
	}
	if key == marketTemplateInstallLockKey("tenant-b", "开关") {
		t.Fatal("different tenants should not share an advisory lock key")
	}
	if key == marketTemplateInstallLockKey("tenant-a", "卧室灯") {
		t.Fatal("different template names should not share an advisory lock key")
	}
}

func TestParseMarketTemplateModelFieldsSupportsAllModelKindsAndCatalogAliases(t *testing.T) {
	definition := model.TemplateDefinitionPayload{
		"telemetry": []interface{}{map[string]interface{}{
			"data_name": "开关", "data_identifier": "switch", "data_type": "Boolean", "read_write_flag": "RW",
		}},
		"attributes": []interface{}{map[string]interface{}{
			"buttom_name": "亮度", "identifier": "brightness", "dataType": "Number", "accessMode": "R", "unit": "%", "remark": "当前亮度",
		}},
		"events": []interface{}{map[string]interface{}{
			"name": "状态变化", "key": "state_changed", "params": map[string]interface{}{"code": "integer"},
		}},
		"commands": []interface{}{map[string]interface{}{
			"label": "开灯", "data_identifier": "turn_on", "type": "Boolean", "additional_info": "{}",
		}},
	}

	got, err := parseMarketTemplateModelFields(&definition)
	if err != nil {
		t.Fatalf("parseMarketTemplateModelFields() error = %v", err)
	}
	want := map[string]map[string]string{
		"telemetry":  {"data_name": "开关", "data_identifier": "switch", "data_type": "Boolean", "read_write_flag": "RW"},
		"attributes": {"data_name": "亮度", "data_identifier": "brightness", "data_type": "Number", "read_write_flag": "R", "unit": "%", "description": "当前亮度"},
		"events":     {"data_name": "状态变化", "data_identifier": "state_changed", "params": `{"code":"integer"}`},
		"commands":   {"data_name": "开灯", "data_identifier": "turn_on", "data_type": "Boolean", "additional_info": "{}"},
	}
	for kind, expected := range want {
		if len(got[kind]) != 1 {
			t.Fatalf("%s rows = %d, want 1", kind, len(got[kind]))
		}
		for key, value := range expected {
			if got[kind][0][key] != value {
				t.Errorf("%s[%s] = %v, want %q", kind, key, got[kind][0][key], value)
			}
		}
	}
}

func TestCreateMarketTemplateModelsRejectsMalformedDefinitions(t *testing.T) {
	tests := []struct {
		name string
		def  model.TemplateDefinitionPayload
		want string
	}{
		{name: "nil definition", def: nil, want: "expected an object"},
		{name: "non-array model kind", def: model.TemplateDefinitionPayload{"telemetry": map[string]interface{}{}}, want: "template_definition.telemetry: expected an array"},
		{name: "non-object model row", def: model.TemplateDefinitionPayload{"attributes": []interface{}{"bad row"}}, want: "template_definition.attributes[0]: expected an object"},
		{name: "missing identifier", def: model.TemplateDefinitionPayload{"events": []interface{}{map[string]interface{}{"name": "started"}}}, want: "missing data_identifier"},
		{name: "wrong field type", def: model.TemplateDefinitionPayload{"commands": []interface{}{map[string]interface{}{"key": "start", "label": "Start", "type": true}}}, want: "template_definition.commands[0].type: expected a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := createMarketTemplateModels(nil, &tt.def, "template-id", "tenant-id", time.Now())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("createMarketTemplateModels() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

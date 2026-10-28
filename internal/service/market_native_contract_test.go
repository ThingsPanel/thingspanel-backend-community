package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"project/internal/model"
)

func TestNativePublicationSeparatesCatalogCoverFromModelResource(t *testing.T) {
	dc, snapshot := nativePublishFixture()
	packageRequest, err := buildNativeDeviceTemplateMarketPackage(model.PublishToMarketReq{CoverURL: "https://catalog.example/marketing.png"}, dc, snapshot, "https://community.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if packageRequest.CoverURL != "https://catalog.example/marketing.png" || packageRequest.NativeCoverURL == nil || *packageRequest.NativeCoverURL != "https://community.example/files/deviceConfig/qa-cover.png" {
		t.Fatal("catalog image replaced native model resource")
	}
	var profile map[string]interface{}
	if err := json.Unmarshal(snapshot.Resources.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	profile["cover"] = ""
	snapshot.Resources.Profile, _ = json.Marshal(profile)
	packageRequest, err = buildNativeDeviceTemplateMarketPackage(model.PublishToMarketReq{CoverURL: "https://catalog.example/marketing.png"}, dc, snapshot, "https://community.example", nil)
	if err != nil || packageRequest.NativeCoverURL == nil || *packageRequest.NativeCoverURL != "" {
		t.Fatal("catalog image prevented publishing a model without a cover")
	}
}

func TestNativeMarketSharedFixtureDownload(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-market-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(envelope["template_definition"], &definition); err != nil {
		t.Fatal(err)
	}
	dependency, err := decodeNativeMarketDependency(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyMarketNativeDependency(dependency); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(dependency.Model, []byte("9007199254740993")) || !bytes.Contains(dependency.Resources, []byte("9007199254740995")) {
		t.Fatal("shared contract lost integer precision")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ThingsPanel-Template-Contract") != "native-thing-model-v1" || r.URL.Query().Get("version") != "1.0.0+fixture" {
			t.Error("download contract/version not pinned")
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"version":"1.0.0+fixture","cover_url":"/api/market/templates/assets/covers/frozen.png","template_definition":`+string(envelope["template_definition"])+`}}`)
	}))
	defer server.Close()
	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	downloaded, err := client.DownloadTemplate(context.Background(), "fixture-token", "fixture-id", "1.0.0+fixture")
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.CoverURL != server.URL+"/api/market/templates/assets/covers/frozen.png" {
		t.Fatal("relative market-owned asset was not resolved")
	}
	decoded, err := decodeMarketTemplateDefinition(downloaded.TemplateDefinition)
	if err != nil {
		t.Fatal(err)
	}
	dep, err := decodeNativeMarketDependency(decoded)
	if err != nil || verifyMarketNativeDependency(dep) != nil {
		t.Fatal("shared package not accepted after market download")
	}
}

func TestNativeModelCoverRejectsChangedBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("changed bytes")) }))
	defer server.Close()
	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	if _, _, err := downloadMarketTemplateImageVerified(context.Background(), client, server.URL+"/api/market/templates/assets/covers/frozen.png", t.TempDir(), "fixture", "expected-checksum"); err == nil {
		t.Fatal("changed cover bytes accepted")
	}
}

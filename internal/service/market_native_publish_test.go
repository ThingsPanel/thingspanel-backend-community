package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/model"

	"github.com/spf13/viper"
)

func nativePublishFixture() (*model.DeviceConfig, *nativeMarketSnapshotResponse) {
	modelID := "5da00000-0000-4000-8000-000000000003"
	snapshotID := "8b304178-68df-4c39-9d4c-e674bedb0adc"
	revision := 3
	profile := json.RawMessage(`{"author":"QA","tags":[],"cover":"./files/deviceConfig/qa-cover.png","version":"3.0","brand":"Acme QA","modelNumber":"S-3"}`)
	snapshot := &nativeMarketSnapshotResponse{
		ModelID: modelID, PublishedRevision: revision, SnapshotID: snapshotID, Checksum: "qa-checksum",
		Model: json.RawMessage(`{"name":"QA Sensor","descriptionI18n":{"default":"QA model"},"category":"test","fields":[{"kind":"TELEMETRY","identifier":"temperature","nameI18n":{"default":"Temperature"},"access":{"read":true,"write":false,"observe":true},"unit":"C","sortOrder":1,"valueType":{"kind":"NUMBER","constraint":{"min":-40,"max":125,"step":0.1}}}]}`),
	}
	snapshot.Resources.ModelID = modelID
	snapshot.Resources.Revision = 7
	snapshot.Resources.Profile = profile
	snapshot.Resources.WebChartConfig = json.RawMessage(`{"dataSources":[{"config":{"deviceId":"__template__","threshold":9007199254740993}}],"widgets":[{"id":"temperature-chart"}]}`)
	snapshot.Resources.AppChartConfig = json.RawMessage(`{"pages":[{"id":"overview","widgets":[{"type":"value","field":"temperature"}]}]}`)
	return &model.DeviceConfig{
		ID: "12d00000-0000-4000-8000-000000000012", Name: "QA Sensor Config", NativeThingModelID: &modelID,
		NativeThingModelSnapshotID: &snapshotID, NativeThingModelRevision: &revision,
		DeviceType: "1", ProtocolConfig: marketPublishStringPtr(`{"client_id":"qa-client","password":"qa-password","nested":{"access_token":"qa-access-token","region":"lab"}}`),
		OtherConfig:    marketPublishStringPtr(`{"tenant_id":"qa-tenant-secret","device_id":"real-device-secret","keep":"safe"}`),
		AdditionalInfo: marketPublishStringPtr(`{"user_id":"qa-user-secret","operator":"facility-team","label":"portable"}`),
	}, snapshot
}

func TestBuildNativeDeviceTemplateMarketPackagePreservesPinnedPayloadAndScrubsSecrets(t *testing.T) {
	dc, snapshot := nativePublishFixture()
	plugins := []model.PluginDependency{{PluginName: "qa-protocol", PluginType: "protocol", MinVersion: "1.2.3", Required: true}}
	marketReq, err := buildNativeDeviceTemplateMarketPackage(model.PublishToMarketReq{}, dc, snapshot, "https://community.qa", plugins)
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	if marketReq.SourceTemplateID != dc.ID || marketReq.Name != dc.Name || marketReq.Version != "3.0" || marketReq.CoverURL != "https://community.qa/files/deviceConfig/qa-cover.png" {
		t.Fatalf("unexpected package metadata: source=%q name=%q version=%q cover=%q", marketReq.SourceTemplateID, marketReq.Name, marketReq.Version, marketReq.CoverURL)
	}
	if len(marketReq.PluginDependencies) != 1 || marketReq.PluginDependencies[0] != plugins[0] {
		t.Fatalf("resolved plugin dependencies were not preserved: %#v", marketReq.PluginDependencies)
	}
	if marketReq.DeviceConfig == nil || marketReq.DeviceConfig.ProtocolConfig["client_id"] != "qa-client" || marketReq.DeviceConfig.OtherConfig["keep"] != "safe" || marketReq.DeviceConfig.AdditionalInfo["label"] != "portable" {
		t.Fatalf("safe device-config fields were not preserved: %#v", marketReq.DeviceConfig)
	}
	if _, ok := marketReq.DeviceConfig.ProtocolConfig["password"]; ok {
		t.Fatal("protocol password was included in the market package")
	}
	if _, ok := marketReq.DeviceConfig.OtherConfig["tenant_id"]; ok {
		t.Fatal("tenant identifier was included in the market package")
	}
	if _, ok := marketReq.DeviceConfig.OtherConfig["device_id"]; ok {
		t.Fatal("device identifier was included in the market package")
	}
	if marketReq.DeviceConfig.AdditionalInfo["operator"] != "facility-team" {
		t.Fatal("valid operator configuration was not preserved")
	}
	if _, ok := marketReq.DeviceConfig.AdditionalInfo["user_id"]; ok {
		t.Fatal("user identifier was included in the market package")
	}

	definition, ok := marketReq.TemplateDefinition["native_thing_model"].(string)
	if !ok || marketReq.TemplateDefinition["native_thing_model_encoding"] != "raw-json-string-v1" {
		t.Fatalf("native dependency transport is not raw-json-string-v1: %#v", marketReq.TemplateDefinition)
	}
	var dependency model.MarketNativeThingModelDependency
	if err := json.Unmarshal([]byte(definition), &dependency); err != nil {
		t.Fatalf("decode native dependency: %v", err)
	}
	if dependency.SourceModelID != *dc.NativeThingModelID || dependency.SourceSnapshotID != *dc.NativeThingModelSnapshotID || dependency.SourceRevision != *dc.NativeThingModelRevision {
		t.Fatalf("dependency lost pinned source provenance: %#v", dependency)
	}
	if err := verifyMarketNativeDependency(&dependency); err != nil {
		t.Fatalf("dependency did not verify: %v", err)
	}
	gotModel, err := canonicalJSON(dependency.Model)
	if err != nil {
		t.Fatalf("canonicalize packaged model: %v", err)
	}
	wantModel, err := canonicalJSON(snapshot.Model)
	if err != nil || !bytes.Equal(gotModel, wantModel) {
		t.Fatalf("model payload changed during packaging: got %s want %s (err %v)", gotModel, wantModel, err)
	}
	var packagedResources map[string]json.RawMessage
	if err := json.Unmarshal(dependency.Resources, &packagedResources); err != nil {
		t.Fatalf("decode packaged resources: %v", err)
	}
	expectedProfile := json.RawMessage(`{"author":"QA","tags":[],"cover":"asset://native-profile-cover","version":"3.0","brand":"Acme QA","modelNumber":"S-3"}`)
	gotProfile, err := canonicalJSON(packagedResources["profile"])
	if err != nil {
		t.Fatalf("canonicalize packaged profile: %v", err)
	}
	wantProfile, err := canonicalJSON(expectedProfile)
	if err != nil || !bytes.Equal(gotProfile, wantProfile) {
		t.Fatalf("profile fields changed beyond the cover asset reference: got %s want %s (err %v)", gotProfile, wantProfile, err)
	}
	for name, want := range map[string]json.RawMessage{
		"webChartConfig": snapshot.Resources.WebChartConfig,
		"appChartConfig": snapshot.Resources.AppChartConfig,
	} {
		gotCanonical, err := canonicalJSON(packagedResources[name])
		if err != nil {
			t.Fatalf("canonicalize packaged %s: %v", name, err)
		}
		wantCanonical, err := canonicalJSON(want)
		if err != nil || !bytes.Equal(gotCanonical, wantCanonical) {
			t.Fatalf("%s changed during packaging: got %s want %s (err %v)", name, gotCanonical, wantCanonical, err)
		}
	}
	if len(dependency.Assets) != 1 || dependency.Assets[0] != (model.MarketNativeThingModelAsset{Key: "native-profile-cover", Source: "market-cover"}) {
		t.Fatalf("profile cover asset manifest was not preserved: %#v", dependency.Assets)
	}
	if !strings.Contains(string(dependency.Resources), `"cover":"asset://native-profile-cover"`) {
		t.Fatalf("profile fields or asset reference were lost: %s", dependency.Resources)
	}
	if !bytes.Contains(dependency.Resources, []byte("9007199254740993")) {
		t.Fatalf("large nested chart number lost precision: %s", dependency.Resources)
	}
	if !bytes.Contains(dependency.Resources, []byte(`"temperature-chart"`)) || !bytes.Contains(dependency.Resources, []byte(`"overview"`)) {
		t.Fatalf("Web/App chart resources were not both retained: %s", dependency.Resources)
	}
	packageJSON, err := json.Marshal(marketReq)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	for _, forbidden := range []string{"qa-password", "qa-access-token", "qa-tenant-secret", "real-device-secret", "qa-user-secret", "market_token", "tenant_id"} {
		if bytes.Contains(packageJSON, []byte(forbidden)) {
			t.Fatalf("sensitive value %q was serialized into package", forbidden)
		}
	}
}

func TestBuildNativeDeviceTemplateMarketPackageRejectsMismatchedPinnedSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*nativeMarketSnapshotResponse)
	}{
		{name: "snapshot id", mutate: func(s *nativeMarketSnapshotResponse) { s.SnapshotID = "another-snapshot" }},
		{name: "model id", mutate: func(s *nativeMarketSnapshotResponse) { s.ModelID = "another-model" }},
		{name: "published revision", mutate: func(s *nativeMarketSnapshotResponse) { s.PublishedRevision++ }},
		{name: "resources model id", mutate: func(s *nativeMarketSnapshotResponse) { s.Resources.ModelID = "another-model" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc, snapshot := nativePublishFixture()
			tt.mutate(snapshot)
			if _, err := buildNativeDeviceTemplateMarketPackage(model.PublishToMarketReq{}, dc, snapshot, "https://community.qa", nil); err == nil {
				t.Fatal("mismatched snapshot was accepted")
			}
		})
	}
}

func TestPublishNativeDeviceConfigUsesNativeMarketPackageBuilder(t *testing.T) {
	dc, snapshot := nativePublishFixture()
	var publishedBody []byte
	var marketCalls int
	market := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marketCalls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/market/templates/publish" {
			t.Errorf("unexpected market request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer qa-market-token" {
			t.Errorf("market auth header = %q", got)
		}
		publishedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"message":"accepted","data":{"status":"pending"}}`)
	}))
	defer market.Close()
	previousMarketURL := viper.GetString("market.base_url")
	viper.Set("market.base_url", market.URL)
	t.Cleanup(func() { viper.Set("market.base_url", previousMarketURL) })

	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/thing-models/5da00000-0000-4000-8000-000000000003/versions/3" {
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer qa-community-session" {
			t.Errorf("native auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(snapshot); err != nil {
			t.Errorf("write native snapshot: %v", err)
		}
	}))
	defer native.Close()
	t.Setenv("TP_THINGMODEL_NATIVE_URL", native.URL)

	response, err := publishNativeDeviceConfig(context.Background(), model.PublishToMarketReq{
		DeviceConfigID: dc.ID, MarketToken: "qa-market-token",
	}, dc, "qa-community-session", "https://community.qa")
	if err != nil {
		t.Fatalf("publish native device config: %v", err)
	}
	if response == nil || response.Code != 0 || marketCalls != 1 {
		t.Fatalf("publish response=%#v marketCalls=%d", response, marketCalls)
	}
	var published model.PublishTemplateReq
	if err := json.Unmarshal(publishedBody, &published); err != nil {
		t.Fatalf("decode published body: %v", err)
	}
	if published.TemplateDefinition["native_thing_model_encoding"] != "raw-json-string-v1" || published.SourceTemplateID != dc.ID {
		t.Fatalf("publisher did not send the native package built from the pinned snapshot: %#v", published)
	}
	for _, forbidden := range []string{"qa-community-session", "qa-market-token", "qa-password", "qa-tenant-secret", "real-device-secret"} {
		if bytes.Contains(publishedBody, []byte(forbidden)) {
			t.Fatalf("sensitive value %q leaked into publish body", forbidden)
		}
	}
}

func marketPublishStringPtr(value string) *string { return &value }

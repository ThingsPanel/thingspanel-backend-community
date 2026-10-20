package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/model"
)

func TestNativeMarketImportPreservesLargeEnumAndNestedChartNumbers(t *testing.T) {
	const largeEnum = "9007199254740993"
	const nestedChartNumber = "9007199254740995"
	modelJSON := json.RawMessage(`{"name":"precision-fixture","properties":[{"identifier":"mode","enum":[` + largeEnum + `]}]}`)
	resourcesJSON := json.RawMessage(`{"profile":{"name":"precision-fixture","cover":""},"webChartConfig":{"panels":[{"series":[{"threshold":` + nestedChartNumber + `}]}]},"appChartConfig":null}`)
	checksum, err := marketNativeDependencyHash(modelJSON, resourcesJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	dependency := &model.MarketNativeThingModelDependency{
		ContractVersion: marketNativeThingModelContractVersion, SourceModelID: "source-model",
		SourceSnapshotID: "source-snapshot", SourceRevision: 3, SourceResourcesRevision: 2,
		SourceChecksum: "source-checksum", DependencyChecksum: checksum, Model: modelJSON, Resources: resourcesJSON,
	}
	var createBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/thing-model-commands/create":
			body, _ := io.ReadAll(r.Body)
			createBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"modelID":"local-model","draftRevision":1,"resourcesRevision":1}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/thing-model-commands/publish":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"modelID":"local-model","publishedRevision":1}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/thing-models/local-model/versions/1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				ModelID           string          `json:"modelID"`
				PublishedRevision int             `json:"publishedRevision"`
				SnapshotID        string          `json:"snapshotID"`
				Checksum          string          `json:"checksum"`
				Model             json.RawMessage `json:"model"`
				Resources         struct {
					ModelID        string          `json:"modelID"`
					Revision       int             `json:"revision"`
					Profile        json.RawMessage `json:"profile"`
					WebChartConfig json.RawMessage `json:"webChartConfig"`
					AppChartConfig json.RawMessage `json:"appChartConfig"`
				} `json:"resources"`
			}{ModelID: "local-model", PublishedRevision: 1, SnapshotID: "local-snapshot", Checksum: "checksum", Model: modelJSON, Resources: struct {
				ModelID        string          `json:"modelID"`
				Revision       int             `json:"revision"`
				Profile        json.RawMessage `json:"profile"`
				WebChartConfig json.RawMessage `json:"webChartConfig"`
				AppChartConfig json.RawMessage `json:"appChartConfig"`
			}{ModelID: "local-model", Revision: 1, Profile: json.RawMessage(`{"name":"precision-fixture","cover":""}`), WebChartConfig: json.RawMessage(`{"panels":[{"series":[{"threshold":9007199254740995}]}]}`), AppChartConfig: json.RawMessage(`null`)}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &nativeMarketImportClient{baseURL: server.URL, httpClient: server.Client()}
	result, err := client.importSnapshot(context.Background(), "Bearer test-community-token", "stable-import-id", dependency, resourcesJSON)
	if err != nil {
		t.Fatal(err)
	}
	if result.SnapshotID != "local-snapshot" {
		t.Fatalf("verified snapshot id = %q", result.SnapshotID)
	}
	if !strings.Contains(createBody, largeEnum) || !strings.Contains(createBody, nestedChartNumber) {
		t.Fatalf("large integers were not preserved in native create request: %s", createBody)
	}
}

func TestNativeMarketImportReusesExactSourceSnapshot(t *testing.T) {
	modelJSON := json.RawMessage(`{"name":"source-model","fields":[]}`)
	dependencyResources := json.RawMessage(`{"profile":{"name":"source-model","cover":"asset://native-profile-cover"},"webChartConfig":null,"appChartConfig":null}`)
	assets := []model.MarketNativeThingModelAsset{{Key: "native-profile-cover", Source: "market-cover"}}
	checksum, err := marketNativeDependencyHash(modelJSON, dependencyResources, assets)
	if err != nil {
		t.Fatal(err)
	}
	dependency := &model.MarketNativeThingModelDependency{
		ContractVersion: marketNativeThingModelContractVersion, SourceModelID: "source-model-id",
		SourceSnapshotID: "source-snapshot-id", SourceRevision: 3,
		DependencyChecksum: checksum, Model: modelJSON, Resources: dependencyResources, Assets: assets,
	}
	targetResources := json.RawMessage(`{"profile":{"name":"source-model","cover":"./files/deviceConfig/native-cover.png"},"webChartConfig":null,"appChartConfig":null}`)
	var createOrPublishCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/thing-models/source-model-id/versions/3" {
			createOrPublishCalled = true
			http.Error(w, "unexpected import request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"modelID":"source-model-id","publishedRevision":3,"snapshotID":"source-snapshot-id","checksum":"source-checksum","model":{"name":"source-model","fields":[]},"resources":{"modelID":"source-model-id","revision":2,"profile":{"name":"source-model","cover":"./files/deviceConfig/original-cover.png"},"webChartConfig":null,"appChartConfig":null}}`)
	}))
	defer server.Close()
	client := &nativeMarketImportClient{baseURL: server.URL, httpClient: server.Client()}
	result, err := client.importSnapshot(context.Background(), "Bearer test-community-token", "stable-import-id", dependency, targetResources)
	if err != nil {
		t.Fatal(err)
	}
	if createOrPublishCalled {
		t.Fatal("exact source snapshot should be reused without create or publish")
	}
	if result.ModelID != dependency.SourceModelID || result.SnapshotID != dependency.SourceSnapshotID || result.PublishedRevision == nil || *result.PublishedRevision != 3 {
		t.Fatalf("unexpected reused source result: %+v", result)
	}
}

func TestNativeMarketSourceSnapshotReuseRejectsChangedContent(t *testing.T) {
	dependency := &model.MarketNativeThingModelDependency{
		SourceModelID: "source-model-id", SourceSnapshotID: "source-snapshot-id", SourceRevision: 3,
		Model:     json.RawMessage(`{"name":"source-model","fields":[]}`),
		Resources: json.RawMessage(`{"profile":{"name":"source-model","cover":"asset://native-profile-cover"},"webChartConfig":null,"appChartConfig":null}`),
		Assets:    []model.MarketNativeThingModelAsset{{Key: "native-profile-cover", Source: "market-cover"}},
	}
	snapshot := &nativeMarketSnapshotResponse{ModelID: dependency.SourceModelID, PublishedRevision: 3, SnapshotID: dependency.SourceSnapshotID, Model: json.RawMessage(`{"name":"other-model","fields":[]}`)}
	snapshot.Resources.Profile = json.RawMessage(`{"name":"source-model","cover":"./files/deviceConfig/original-cover.png"}`)
	snapshot.Resources.WebChartConfig = json.RawMessage(`null`)
	snapshot.Resources.AppChartConfig = json.RawMessage(`null`)
	if err := verifyMarketSourceSnapshotContent(dependency, snapshot); err == nil {
		t.Fatal("source snapshot with changed model content was accepted for reuse")
	}
}

func TestNativeMarketDependencyOpaqueEncodingSurvivesHorizonMapRoundTrip(t *testing.T) {
	const large = "9007199254740993"
	modelJSON := json.RawMessage(`{"properties":[{"enum":[` + large + `]}]}`)
	resourcesJSON := json.RawMessage(`{"profile":{"cover":""},"webChartConfig":{"threshold":` + large + `},"appChartConfig":null}`)
	checksum, err := marketNativeDependencyHash(modelJSON, resourcesJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	dependency := model.MarketNativeThingModelDependency{ContractVersion: marketNativeThingModelContractVersion, SourceModelID: "m", SourceSnapshotID: "s", SourceRevision: 1, SourceChecksum: "source", DependencyChecksum: checksum, Model: modelJSON, Resources: resourcesJSON}
	raw, err := json.Marshal(dependency)
	if err != nil {
		t.Fatal(err)
	}
	// Horizon's current template DTO is map[string]interface{}; this round trip
	// models its float64 conversion. The dependency remains an opaque string.
	requestBody, err := json.Marshal(map[string]interface{}{"template_definition": map[string]interface{}{"native_thing_model": string(raw), "native_thing_model_encoding": "raw-json-string-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	var horizonRequest struct {
		TemplateDefinition map[string]interface{} `json:"template_definition"`
	}
	if err := json.Unmarshal(requestBody, &horizonRequest); err != nil {
		t.Fatal(err)
	}
	storedBody, err := json.Marshal(horizonRequest.TemplateDefinition)
	if err != nil {
		t.Fatal(err)
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(storedBody, &definition); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeMarketDependency(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyMarketNativeDependency(decoded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded.Model), large) || !strings.Contains(string(decoded.Resources), large) {
		t.Fatalf("opaque dependency changed large integers: model=%s resources=%s", decoded.Model, decoded.Resources)
	}
}

func TestNativeMarketTemplateCoverURLIsStableAcrossRetries(t *testing.T) {
	oldDir := marketTemplateImageDir
	marketTemplateImageDir = t.TempDir()
	t.Cleanup(func() { marketTemplateImageDir = oldDir })
	image := []byte("\x89PNG\r\n\x1a\nfixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/market/templates/assets/cover.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(image)
	}))
	defer server.Close()
	client := &MarketClient{baseURL: server.URL, httpClient: server.Client()}
	fullData := &model.MarketTemplateFullData{CoverURL: server.URL + "/api/market/templates/assets/cover.png"}
	one, err := localizeMarketTemplateImageStable(context.Background(), client, fullData, "native-retry-fixture")
	if err != nil {
		t.Fatal(err)
	}
	two, err := localizeMarketTemplateImageStable(context.Background(), client, fullData, "native-retry-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if one == nil || two == nil || *one != *two {
		t.Fatalf("retry cover URL changed: %v then %v", one, two)
	}
}

func TestNativeMarketClientDoesNotExposeErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"private-token-value"}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	client := &nativeMarketImportClient{baseURL: server.URL, httpClient: server.Client()}
	err := client.doJSON(context.Background(), http.MethodGet, "/private", "Bearer request-secret", "", nil, nil)
	if err == nil {
		t.Fatal("expected status error")
	}
	if strings.Contains(err.Error(), "private-token-value") || strings.Contains(err.Error(), "request-secret") {
		t.Fatalf("error exposed sensitive content: %v", err)
	}
}

func TestNativeMarketReadbackRejectsLostFields(t *testing.T) {
	expectedModel := json.RawMessage(`{"properties":[{"enum":[9007199254740993],"constraints":{"minimum":-9007199254740995}}]}`)
	expectedResources := json.RawMessage(`{"profile":{"cover":""},"webChartConfig":{"threshold":9007199254740995},"appChartConfig":null}`)
	good := &nativeMarketSnapshotResponse{Model: expectedModel}
	good.Resources.Profile = json.RawMessage(`{"cover":""}`)
	good.Resources.WebChartConfig = json.RawMessage(`{"threshold":9007199254740995}`)
	good.Resources.AppChartConfig = json.RawMessage(`null`)
	if err := verifyImportedNativeSnapshotContent(expectedModel, expectedResources, good); err != nil {
		t.Fatalf("complete exact readback was rejected: %v", err)
	}
	bad := *good
	bad.Model = json.RawMessage(`{"properties":[{"enum":[9007199254740992]}]}`)
	if err := verifyImportedNativeSnapshotContent(expectedModel, expectedResources, &bad); err == nil {
		t.Fatal("readback with a rounded enum was accepted")
	}
}

func TestNativeMarketDependencyRejectsUnknownAssetAndSourceIdentity(t *testing.T) {
	modelJSON := json.RawMessage(`{"name":"x"}`)
	resourcesJSON := json.RawMessage(`{"profile":{"cover":"asset://other"},"webChartConfig":null,"appChartConfig":null}`)
	checksum, err := marketNativeDependencyHash(modelJSON, resourcesJSON, []model.MarketNativeThingModelAsset{{Key: "other", Source: "market-cover"}})
	if err != nil {
		t.Fatal(err)
	}
	dependency := &model.MarketNativeThingModelDependency{ContractVersion: marketNativeThingModelContractVersion, SourceModelID: "m", SourceSnapshotID: "s", SourceRevision: 1, SourceChecksum: "c", DependencyChecksum: checksum, Model: modelJSON, Resources: resourcesJSON, Assets: []model.MarketNativeThingModelAsset{{Key: "other", Source: "market-cover"}}}
	if _, err := resolveMarketNativeDependencyResources(dependency, nil); err == nil {
		t.Fatal("expected unknown asset to be rejected")
	}
	if err := rejectNonPortableIdentityKeys(json.RawMessage(`{"resources":{"device_id":"source-device"}}`)); err == nil {
		t.Fatal("expected source device identity to be rejected")
	}
	if err := rejectNonPortableIdentityKeys(json.RawMessage(`{"resources":{"appChartConfig":{"dataSources":[{"config":{"deviceId":"__template__"}}]}}}`)); err != nil {
		t.Fatalf("template device placeholder must remain portable: %v", err)
	}
}

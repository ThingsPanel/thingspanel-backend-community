package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"project/internal/model"
)

const marketNativeThingModelContractVersion = "1"
const maxMarketNativeModelPayload = 4 << 20

type nativeMarketSnapshotResponse struct {
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
}

type nativeMarketImportResult struct {
	ModelID           string  `json:"modelID"`
	DraftRevision     uint64  `json:"draftRevision"`
	PublishedRevision *uint64 `json:"publishedRevision"`
	ResourcesRevision *uint64 `json:"resourcesRevision"`
	SnapshotID        string  `json:"snapshotID,omitempty"`
}

type nativeMarketCreateRequest struct {
	Model     json.RawMessage `json:"model"`
	Resources json.RawMessage `json:"resources"`
}

type nativeMarketHTTPError struct {
	StatusCode int
}

func (e *nativeMarketHTTPError) Error() string {
	return fmt.Sprintf("native thing-model request returned status %d", e.StatusCode)
}

type nativeMarketImportClient struct {
	baseURL    string
	httpClient *http.Client
}

func newNativeMarketImportClient() (*nativeMarketImportClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("TP_THINGMODEL_NATIVE_URL")), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("native thing-model service URL is not configured or invalid")
	}
	return &nativeMarketImportClient{
		baseURL: baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (c *nativeMarketImportClient) readSnapshot(ctx context.Context, communityAuthorization, modelID string, revision int) (*nativeMarketSnapshotResponse, error) {
	var out nativeMarketSnapshotResponse
	path := "/api/v2/thing-models/" + url.PathEscape(modelID) + "/versions/" + strconv.Itoa(revision)
	if err := c.doJSON(ctx, http.MethodGet, path, communityAuthorization, "", nil, &out); err != nil {
		return nil, err
	}
	if out.ModelID != modelID || out.PublishedRevision != revision || strings.TrimSpace(out.SnapshotID) == "" || strings.TrimSpace(out.Checksum) == "" {
		return nil, fmt.Errorf("native snapshot identity does not match the device-config binding")
	}
	if out.Resources.ModelID != modelID || out.Resources.Revision < 0 || !json.Valid(out.Model) || !json.Valid(out.Resources.Profile) {
		return nil, fmt.Errorf("native snapshot model or resources payload is incomplete")
	}
	return &out, nil
}

func (c *nativeMarketImportClient) importSnapshot(ctx context.Context, communityAuthorization, idempotencyKey string, dependency *model.MarketNativeThingModelDependency, targetResources json.RawMessage) (*nativeMarketImportResult, error) {
	if dependency == nil || dependency.ContractVersion != marketNativeThingModelContractVersion {
		return nil, fmt.Errorf("unsupported native thing-model dependency contract")
	}
	modelJSON, err := canonicalJSON(dependency.Model)
	if err != nil || len(modelJSON) == 0 || bytes.Equal(modelJSON, []byte("null")) {
		return nil, fmt.Errorf("native dependency model is invalid")
	}
	resourcesJSON, err := canonicalJSON(dependency.Resources)
	if err != nil || len(resourcesJSON) == 0 || bytes.Equal(resourcesJSON, []byte("null")) {
		return nil, fmt.Errorf("native dependency resources are invalid")
	}
	expectedChecksum, err := marketNativeDependencyHash(modelJSON, resourcesJSON, dependency.Assets)
	if err != nil || expectedChecksum != dependency.DependencyChecksum {
		return nil, fmt.Errorf("native dependency checksum does not match its model and resources")
	}
	var created nativeMarketImportResult
	createKey := nativeMarketOperationKey(idempotencyKey, "create")
	if !json.Valid(targetResources) {
		return nil, fmt.Errorf("resolved native dependency resources are invalid")
	}
	// Publishing a template and installing it back into its source tenant is a
	// supported round trip. The source model already occupies its tenant-scoped
	// name, so reuse it only when the immutable source snapshot and full package
	// payload match exactly. Other tenants receive a 404 here and follow the
	// normal create/import path below.
	sourceSnapshot, sourceErr := c.readSnapshot(ctx, communityAuthorization, dependency.SourceModelID, dependency.SourceRevision)
	if sourceErr == nil {
		if sourceSnapshot.SnapshotID != dependency.SourceSnapshotID {
			return nil, fmt.Errorf("source native model exists, but its published snapshot does not match this market package")
		}
		if err := verifyMarketSourceSnapshotContent(dependency, sourceSnapshot); err != nil {
			return nil, fmt.Errorf("source native model exists, but cannot be safely reused: %w", err)
		}
		publishedRevision := uint64(sourceSnapshot.PublishedRevision)
		resourcesRevision := uint64(sourceSnapshot.Resources.Revision)
		return &nativeMarketImportResult{
			ModelID: sourceSnapshot.ModelID, DraftRevision: publishedRevision,
			PublishedRevision: &publishedRevision, ResourcesRevision: &resourcesRevision,
			SnapshotID: sourceSnapshot.SnapshotID,
		}, nil
	}
	var sourceHTTPError *nativeMarketHTTPError
	if !errors.As(sourceErr, &sourceHTTPError) || sourceHTTPError.StatusCode != http.StatusNotFound {
		return nil, fmt.Errorf("check source native model before import: %w", sourceErr)
	}
	createRequest, err := json.Marshal(struct {
		Model     json.RawMessage `json:"model"`
		Resources json.RawMessage `json:"resources"`
	}{Model: modelJSON, Resources: targetResources})
	if err != nil {
		return nil, fmt.Errorf("encode native import create request: %w", err)
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v2/thing-model-commands/create", communityAuthorization, createKey, json.RawMessage(createRequest), &created); err != nil {
		return nil, fmt.Errorf("create imported native model: %w", err)
	}
	if created.ModelID == "" || created.DraftRevision == 0 || created.ResourcesRevision == nil {
		return nil, fmt.Errorf("native model create response is incomplete")
	}
	resourcesRevision := *created.ResourcesRevision
	var published nativeMarketImportResult
	publishKey := nativeMarketOperationKey(idempotencyKey, "publish")
	if err := c.doJSON(ctx, http.MethodPost, "/api/v2/thing-model-commands/publish", communityAuthorization, publishKey, map[string]interface{}{
		"modelID": created.ModelID, "expectedDraftRevision": created.DraftRevision,
		"changelog": "Imported from device-template market package", "expectedResourcesRevision": resourcesRevision,
	}, &published); err != nil {
		return nil, fmt.Errorf("publish imported native model: %w", err)
	}
	if published.ModelID != created.ModelID || published.PublishedRevision == nil || *published.PublishedRevision == 0 {
		return nil, fmt.Errorf("native model publish response is incomplete")
	}
	createdSnapshot, err := c.readSnapshot(ctx, communityAuthorization, created.ModelID, int(*published.PublishedRevision))
	if err != nil {
		return nil, fmt.Errorf("verify published native model snapshot: %w", err)
	}
	if err := verifyImportedNativeSnapshotContent(modelJSON, targetResources, createdSnapshot); err != nil {
		return nil, err
	}
	published.SnapshotID = createdSnapshot.SnapshotID
	return &published, nil
}

func verifyMarketSourceSnapshotContent(dependency *model.MarketNativeThingModelDependency, snapshot *nativeMarketSnapshotResponse) error {
	if dependency == nil || snapshot == nil || snapshot.ModelID != dependency.SourceModelID || snapshot.PublishedRevision != dependency.SourceRevision || snapshot.SnapshotID != dependency.SourceSnapshotID {
		return fmt.Errorf("source snapshot identity does not match the immutable market dependency")
	}
	actualModel, err := canonicalJSON(snapshot.Model)
	if err != nil {
		return fmt.Errorf("source model snapshot is invalid")
	}
	expectedModel, err := canonicalJSON(dependency.Model)
	if err != nil || !bytes.Equal(actualModel, expectedModel) {
		return fmt.Errorf("source model payload differs from the immutable market dependency")
	}
	actualResources, err := json.Marshal(struct {
		Profile        json.RawMessage `json:"profile"`
		WebChartConfig json.RawMessage `json:"webChartConfig"`
		AppChartConfig json.RawMessage `json:"appChartConfig"`
	}{snapshot.Resources.Profile, nullIfEmptyJSON(snapshot.Resources.WebChartConfig), nullIfEmptyJSON(snapshot.Resources.AppChartConfig)})
	if err != nil {
		return fmt.Errorf("source model resources are invalid")
	}
	if len(dependency.Assets) > 0 {
		if len(dependency.Assets) != 1 || dependency.Assets[0].Key != "native-profile-cover" || dependency.Assets[0].Source != "market-cover" {
			return fmt.Errorf("source snapshot contains an unsupported market asset mapping")
		}
		profile, err := rawObject(json.RawMessage(actualResources))
		if err != nil {
			return fmt.Errorf("source model profile is invalid")
		}
		profileObject, err := rawObject(profile["profile"])
		if err != nil || rawString(profile["profile"], "cover") == "" {
			return fmt.Errorf("source model cover is missing")
		}
		profileObject["cover"] = json.RawMessage(`"asset://native-profile-cover"`)
		profile["profile"], err = json.Marshal(profileObject)
		if err != nil {
			return fmt.Errorf("normalize source model cover for package comparison")
		}
		actualResources, err = json.Marshal(profile)
		if err != nil {
			return fmt.Errorf("encode normalized source model resources")
		}
	}
	actualCanonicalResources, err := canonicalJSON(actualResources)
	if err != nil {
		return fmt.Errorf("source model resources are invalid")
	}
	expectedCanonicalResources, err := canonicalJSON(dependency.Resources)
	if err != nil || !bytes.Equal(actualCanonicalResources, expectedCanonicalResources) {
		return fmt.Errorf("source model resources differ from the immutable market dependency")
	}
	return nil
}

func verifyImportedNativeSnapshotContent(expectedModel, expectedResources json.RawMessage, snapshot *nativeMarketSnapshotResponse) error {
	actualModel, err := canonicalJSON(snapshot.Model)
	if err != nil {
		return fmt.Errorf("read back imported native model: invalid JSON")
	}
	expectedCanonicalModel, err := canonicalJSON(expectedModel)
	if err != nil || !bytes.Equal(actualModel, expectedCanonicalModel) {
		return fmt.Errorf("native import readback differs from the complete model payload")
	}
	actualResources, err := json.Marshal(struct {
		Profile        json.RawMessage `json:"profile"`
		WebChartConfig json.RawMessage `json:"webChartConfig"`
		AppChartConfig json.RawMessage `json:"appChartConfig"`
	}{snapshot.Resources.Profile, nullIfEmptyJSON(snapshot.Resources.WebChartConfig), nullIfEmptyJSON(snapshot.Resources.AppChartConfig)})
	if err != nil {
		return fmt.Errorf("read back imported native resources")
	}
	actualCanonicalResources, err := canonicalJSON(actualResources)
	if err != nil {
		return fmt.Errorf("read back imported native resources: invalid JSON")
	}
	expectedCanonicalResources, err := canonicalJSON(expectedResources)
	if err != nil || !bytes.Equal(actualCanonicalResources, expectedCanonicalResources) {
		return fmt.Errorf("native import readback differs from the complete resources payload")
	}
	return nil
}

func (c *nativeMarketImportClient) doJSON(ctx context.Context, method, path, authorization, idempotencyKey string, input, output interface{}) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode native thing-model request: %w", err)
		}
		if len(data) > maxMarketNativeModelPayload {
			return fmt.Errorf("native thing-model request exceeds %d bytes", maxMarketNativeModelPayload)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build native thing-model request: %w", err)
	}
	authorization = strings.TrimSpace(authorization)
	if authorization == "" {
		return fmt.Errorf("authenticated community token is required for native thing-model access")
	}
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		authorization = "Bearer " + authorization
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("native thing-model request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMarketNativeModelPayload+1))
	if err != nil {
		return fmt.Errorf("read native thing-model response: %w", err)
	}
	if len(data) > maxMarketNativeModelPayload {
		return fmt.Errorf("native thing-model response exceeds %d bytes", maxMarketNativeModelPayload)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &nativeMarketHTTPError{StatusCode: resp.StatusCode}
	}
	if output != nil {
		if err := json.Unmarshal(data, output); err != nil {
			return fmt.Errorf("decode native thing-model response: %w", err)
		}
	}
	return nil
}

func nativeMarketOperationKey(identity, operation string) string {
	hash := sha256.Sum256([]byte(identity + ":" + operation))
	return "market-import-" + operation + "-" + hex.EncodeToString(hash[:])
}

func buildMarketNativeThingModelDependency(snapshot *nativeMarketSnapshotResponse) (*model.MarketNativeThingModelDependency, error) {
	if snapshot == nil || !json.Valid(snapshot.Model) || !json.Valid(snapshot.Resources.Profile) {
		return nil, fmt.Errorf("native snapshot is incomplete")
	}
	modelJSON, err := canonicalJSON(snapshot.Model)
	if err != nil {
		return nil, fmt.Errorf("canonicalize native model snapshot: %w", err)
	}
	resourcesRaw, err := json.Marshal(struct {
		Profile        json.RawMessage `json:"profile"`
		WebChartConfig json.RawMessage `json:"webChartConfig"`
		AppChartConfig json.RawMessage `json:"appChartConfig"`
	}{
		Profile:        snapshot.Resources.Profile,
		WebChartConfig: nullIfEmptyJSON(snapshot.Resources.WebChartConfig),
		AppChartConfig: nullIfEmptyJSON(snapshot.Resources.AppChartConfig),
	})
	if err != nil {
		return nil, fmt.Errorf("encode native snapshot resources: %w", err)
	}
	resourcesJSON, err := canonicalJSON(resourcesRaw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize native snapshot resources: %w", err)
	}
	dependencyHash, err := marketNativeDependencyHash(modelJSON, resourcesJSON, nil)
	if err != nil {
		return nil, err
	}
	return &model.MarketNativeThingModelDependency{
		ContractVersion: marketNativeThingModelContractVersion,
		SourceModelID:   snapshot.ModelID, SourceSnapshotID: snapshot.SnapshotID,
		SourceRevision: snapshot.PublishedRevision, SourceResourcesRevision: snapshot.Resources.Revision,
		SourceChecksum: snapshot.Checksum, DependencyChecksum: dependencyHash,
		Model: modelJSON, Resources: resourcesJSON,
	}, nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	return json.RawMessage(canonical), err
}

func canonicalHash(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

func marketNativeDependencyHash(modelJSON, resourcesJSON json.RawMessage, assets []model.MarketNativeThingModelAsset) (string, error) {
	return canonicalHash(struct {
		Model     json.RawMessage                     `json:"model"`
		Resources json.RawMessage                     `json:"resources"`
		Assets    []model.MarketNativeThingModelAsset `json:"assets,omitempty"`
	}{modelJSON, resourcesJSON, assets})
}

func verifyMarketNativeDependency(dependency *model.MarketNativeThingModelDependency) error {
	if dependency == nil || dependency.ContractVersion != marketNativeThingModelContractVersion ||
		strings.TrimSpace(dependency.SourceModelID) == "" || strings.TrimSpace(dependency.SourceSnapshotID) == "" ||
		dependency.SourceRevision < 1 || dependency.SourceResourcesRevision < 0 || strings.TrimSpace(dependency.SourceChecksum) == "" {
		return fmt.Errorf("native thing-model dependency identity is incomplete")
	}
	modelJSON, err := canonicalJSON(dependency.Model)
	if err != nil || len(modelJSON) == 0 || bytes.Equal(modelJSON, []byte("null")) {
		return fmt.Errorf("native thing-model dependency model is invalid")
	}
	resourcesJSON, err := canonicalJSON(dependency.Resources)
	if err != nil || len(resourcesJSON) == 0 || bytes.Equal(resourcesJSON, []byte("null")) {
		return fmt.Errorf("native thing-model dependency resources are invalid")
	}
	checksum, err := marketNativeDependencyHash(modelJSON, resourcesJSON, dependency.Assets)
	if err != nil || checksum != dependency.DependencyChecksum {
		return fmt.Errorf("native thing-model dependency checksum mismatch")
	}
	if err := rejectNonPortableIdentityKeys(modelJSON); err != nil {
		return err
	}
	if err := rejectNonPortableIdentityKeys(resourcesJSON); err != nil {
		return err
	}
	return nil
}

func decodeNativeMarketDependency(definition map[string]json.RawMessage) (*model.MarketNativeThingModelDependency, error) {
	var encoding string
	if err := json.Unmarshal(definition["native_thing_model_encoding"], &encoding); err != nil || encoding != "raw-json-string-v1" {
		return nil, fmt.Errorf("unsupported or missing native dependency encoding")
	}
	var rawDependency string
	if err := json.Unmarshal(definition["native_thing_model"], &rawDependency); err != nil || strings.TrimSpace(rawDependency) == "" {
		return nil, fmt.Errorf("native dependency must be carried as raw-json-string-v1")
	}
	var dependency model.MarketNativeThingModelDependency
	if err := json.Unmarshal([]byte(rawDependency), &dependency); err != nil {
		return nil, fmt.Errorf("native thing-model dependency is invalid")
	}
	return &dependency, nil
}

func resolveMarketNativeDependencyResources(dependency *model.MarketNativeThingModelDependency, localCoverURL *string) (json.RawMessage, error) {
	if err := verifyMarketNativeDependency(dependency); err != nil {
		return nil, err
	}
	resources, err := dependencyResourceObject(dependency.Resources)
	if err != nil {
		return nil, fmt.Errorf("decode native dependency resources: %w", err)
	}
	for _, asset := range dependency.Assets {
		if asset.Key != "native-profile-cover" || asset.Source != "market-cover" {
			return nil, fmt.Errorf("unsupported native dependency asset %q", asset.Key)
		}
		if localCoverURL == nil || strings.TrimSpace(*localCoverURL) == "" {
			return nil, fmt.Errorf("market package is missing the native model cover asset")
		}
		profile, err := rawObject(resources["profile"])
		if err != nil {
			return nil, fmt.Errorf("native dependency profile is invalid")
		}
		if rawString(resources["profile"], "cover") != "asset://native-profile-cover" {
			return nil, fmt.Errorf("native profile cover reference does not match its asset manifest")
		}
		localCoverJSON, _ := json.Marshal(*localCoverURL)
		profile["cover"] = localCoverJSON
		resources["profile"], err = json.Marshal(profile)
		if err != nil {
			return nil, fmt.Errorf("encode localized native model profile")
		}
	}
	return json.Marshal(resources)
}

func rejectNonPortableIdentityKeys(raw json.RawMessage) error {
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode native dependency for portability check: %w", err)
	}
	var walk func(interface{}) error
	walk = func(node interface{}) error {
		switch typed := node.(type) {
		case map[string]interface{}:
			for key, child := range typed {
				normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
				if normalized == "tenantid" || normalized == "userid" || normalized == "deviceid" {
					if normalized == "deviceid" {
						if value, ok := child.(string); ok && (value == "" || value == "__template__") {
							continue
						}
					}
					if child == nil {
						continue
					}
					return fmt.Errorf("native dependency contains a non-portable %s field", key)
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range typed {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}

func nullIfEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

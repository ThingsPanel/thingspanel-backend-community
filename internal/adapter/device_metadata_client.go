package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	dal "project/internal/dal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// ─── Response types from new device-metadata service ─────────────────────────

// deviceTemplateResp is the envelope returned by GET /api/device-templates/{id}
type deviceTemplateResp struct {
	ThingModelID string `json:"thing_model_id"`
}

// listItemsResp is the envelope returned by GET /api/thing-models/{id}/items
type listItemsResp struct {
	Items []*ThingModelItem `json:"items"`
	Total int               `json:"total"`
}

type thingModelVersionResp struct {
	Content json.RawMessage `json:"content"`
}

type snapshotContent struct {
	Items []snapshotContentItem `json:"items"`
}

type snapshotContentItem struct {
	Type            string          `json:"type"`
	Identifier      string          `json:"identifier"`
	NameI18n        json.RawMessage `json:"name_i18n"`
	DescriptionI18n json.RawMessage `json:"description_i18n,omitempty"`
	ValueType       json.RawMessage `json:"value_type"`
	Access          json.RawMessage `json:"access"`
	WebChartConfig  json.RawMessage `json:"web_chart_config,omitempty"`
	AppChartConfig  json.RawMessage `json:"app_chart_config,omitempty"`
	MetaItems       json.RawMessage `json:"meta_items,omitempty"`
	SortOrder       int             `json:"sort_order"`
}

type legacyThingModelBinding struct {
	ThingModelID         string `json:"thingModelId"`
	ThingModelSnapshotID string `json:"thingModelSnapshotId,omitempty"`
	SnapshotVersion      int    `json:"snapshotVersion,omitempty"`
}

// ─── Client ──────────────────────────────────────────────────────────────────

// DeviceMetadataClient proxies read requests to the new device-metadata service.
// It caches results in memory to reduce cross-service latency (read-heavy workload).
type DeviceMetadataClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
	cache        *cache.Cache // patrickmn/go-cache; TTL = 5 min
}

const defaultCacheTTL = 5 * time.Minute

var (
	globalClient *DeviceMetadataClient
	clientOnce   sync.Once
)

// DeviceMetadata returns the process-wide singleton client.
// Configuration is read from viper on first call:
//
//	device_metadata.base_url       (e.g. "http://localhost:4000")
//	device_metadata.service_token  (JWT for service-to-service auth)
func DeviceMetadata() *DeviceMetadataClient {
	clientOnce.Do(func() {
		globalClient = &DeviceMetadataClient{
			baseURL:      viper.GetString("device_metadata.base_url"),
			serviceToken: viper.GetString("device_metadata.service_token"),
			httpClient:   &http.Client{Timeout: 10 * time.Second},
			cache:        cache.New(defaultCacheTTL, 10*time.Minute),
		}
	})
	return globalClient
}

// GetItemsByTemplate returns all ThingModel items bound to the given device template.
// Results are cached per (tenantID, templateID) key for 5 minutes.
func (c *DeviceMetadataClient) GetItemsByTemplate(ctx context.Context, tenantID, templateID string) ([]*ThingModelItem, error) {
	binding := resolveLegacyThingModelBinding(templateID)
	cacheKey := buildTemplateCacheKey(tenantID, templateID, binding)

	if cached, found := c.cache.Get(cacheKey); found {
		logrus.WithFields(logrus.Fields{
			"tenant_id":   tenantID,
			"template_id": templateID,
		}).Debug("[adapter] cache hit for GetItemsByTemplate")
		return cached.([]*ThingModelItem), nil
	}

	items, err := c.fetchItemsForTemplate(ctx, tenantID, templateID, binding)
	if err != nil {
		// Return stale cache if available (stale-while-revalidate pattern)
		if stale, found := c.cache.Get(cacheKey + ":stale"); found {
			logrus.WithFields(logrus.Fields{
				"tenant_id":   tenantID,
				"template_id": templateID,
				"error":       err.Error(),
			}).Warn("[adapter] device-metadata unreachable, serving stale cache")
			return stale.([]*ThingModelItem), nil
		}
		return nil, err
	}

	c.cache.Set(cacheKey, items, defaultCacheTTL)
	// Keep a stale copy with a longer TTL for fallback
	c.cache.Set(cacheKey+":stale", items, 30*time.Minute)

	return items, nil
}

// InvalidateTemplate removes cached entries for a given template.
// Call this after a ThingModel publish event invalidates the previous snapshot.
func (c *DeviceMetadataClient) InvalidateTemplate(tenantID, templateID string) {
	cacheKey := tenantID + ":" + templateID
	c.cache.Delete(cacheKey)
	logrus.WithFields(logrus.Fields{
		"tenant_id":   tenantID,
		"template_id": templateID,
	}).Info("[adapter] cache invalidated for template")
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (c *DeviceMetadataClient) fetchItemsByTemplate(ctx context.Context, tenantID, templateID string) ([]*ThingModelItem, error) {
	// Step 1: resolve thing_model_id from the template
	var tpl deviceTemplateResp
	if err := c.get(ctx, tenantID, fmt.Sprintf("/api/device-templates/%s", templateID), &tpl); err != nil {
		return nil, fmt.Errorf("adapter: fetch device template %s: %w", templateID, err)
	}
	if tpl.ThingModelID == "" {
		// Template has no bound thing model — return empty list
		return nil, nil
	}

	// Step 2: list items for that thing model
	var resp listItemsResp
	if err := c.get(ctx, tenantID, fmt.Sprintf("/api/thing-models/%s/items", tpl.ThingModelID), &resp); err != nil {
		return nil, fmt.Errorf("adapter: fetch thing-model items for %s: %w", tpl.ThingModelID, err)
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id":      tenantID,
		"template_id":    templateID,
		"thing_model_id": tpl.ThingModelID,
		"item_count":     len(resp.Items),
	}).Debug("[adapter] fetched items from device-metadata service")

	return resp.Items, nil
}

func (c *DeviceMetadataClient) fetchItemsForTemplate(ctx context.Context, tenantID, templateID string, binding *legacyThingModelBinding) ([]*ThingModelItem, error) {
	if binding != nil && binding.ThingModelID != "" {
		if binding.SnapshotVersion > 0 {
			return c.fetchItemsByThingModelVersion(ctx, tenantID, binding.ThingModelID, binding.SnapshotVersion)
		}
		return c.fetchItemsByThingModel(ctx, tenantID, binding.ThingModelID)
	}

	return c.fetchItemsByTemplate(ctx, tenantID, templateID)
}

func (c *DeviceMetadataClient) fetchItemsByThingModel(ctx context.Context, tenantID, thingModelID string) ([]*ThingModelItem, error) {
	var resp listItemsResp
	if err := c.get(ctx, tenantID, fmt.Sprintf("/api/thing-models/%s/items", thingModelID), &resp); err != nil {
		return nil, fmt.Errorf("adapter: fetch thing-model items for %s: %w", thingModelID, err)
	}
	return resp.Items, nil
}

func (c *DeviceMetadataClient) fetchItemsByThingModelVersion(ctx context.Context, tenantID, thingModelID string, version int) ([]*ThingModelItem, error) {
	var resp thingModelVersionResp
	if err := c.get(ctx, tenantID, fmt.Sprintf("/api/thing-models/%s/versions/%d", thingModelID, version), &resp); err != nil {
		return nil, fmt.Errorf("adapter: fetch thing-model version %s@v%d: %w", thingModelID, version, err)
	}

	var content snapshotContent
	if err := json.Unmarshal(resp.Content, &content); err != nil {
		return nil, fmt.Errorf("adapter: decode snapshot content for %s@v%d: %w", thingModelID, version, err)
	}

	items := make([]*ThingModelItem, 0, len(content.Items))
	for index, item := range content.Items {
		items = append(items, &ThingModelItem{
			ID:              fmt.Sprintf("%s:%d", thingModelID, index),
			ThingModelID:    thingModelID,
			TenantID:        tenantID,
			Type:            item.Type,
			Identifier:      item.Identifier,
			NameI18n:        item.NameI18n,
			DescriptionI18n: item.DescriptionI18n,
			ValueType:       item.ValueType,
			Access:          item.Access,
			WebChartConfig:  item.WebChartConfig,
			AppChartConfig:  item.AppChartConfig,
			MetaItems:       item.MetaItems,
			SortOrder:       item.SortOrder,
		})
	}

	return items, nil
}

func resolveLegacyThingModelBinding(templateID string) *legacyThingModelBinding {
	template, err := dal.GetDeviceTemplateById(templateID)
	if err != nil || template == nil || template.Remark == nil {
		return nil
	}

	var binding legacyThingModelBinding
	if err := json.Unmarshal([]byte(*template.Remark), &binding); err != nil {
		return nil
	}
	if binding.ThingModelID == "" {
		return nil
	}

	return &binding
}

func buildTemplateCacheKey(tenantID, templateID string, binding *legacyThingModelBinding) string {
	cacheKey := tenantID + ":" + templateID
	if binding == nil || binding.ThingModelID == "" {
		return cacheKey
	}

	return cacheKey + ":" + binding.ThingModelID + ":" + strconv.Itoa(binding.SnapshotVersion)
}

// get performs a GET request to the device-metadata service, decoding the
// JSON response body into out.
func (c *DeviceMetadataClient) get(ctx context.Context, tenantID, path string, out interface{}) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if token := c.authTokenForTenant(tenantID); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"url":     url,
			"elapsed": elapsed,
			"error":   err.Error(),
		}).Error("[adapter] HTTP request failed")
		return fmt.Errorf("http GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	logrus.WithFields(logrus.Fields{
		"url":     url,
		"status":  resp.StatusCode,
		"elapsed": elapsed,
	}).Debug("[adapter] HTTP response")

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("resource not found: %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upstream error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", url, err)
	}
	return nil
}

func (c *DeviceMetadataClient) authTokenForTenant(tenantID string) string {
	if token := strings.TrimSpace(c.serviceToken); token != "" {
		return token
	}
	if tenantID == "" {
		return ""
	}
	// Local Encore dev auth accepts `user_id:tenant_id:roles:scopes`.
	return fmt.Sprintf("thingspanel-backend:%s:tenant_admin:*", tenantID)
}

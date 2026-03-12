package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"project/internal/model"

	"github.com/spf13/viper"
)

// MarketClient handles communication with the external market API.
type MarketClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMarketClient creates a new client using configs from viper.
func NewMarketClient() *MarketClient {
	// fallback if not provided
	baseURL := viper.GetString("market.base_url")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8081"
	}

	return &MarketClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Login authenticates with the market service to get an access token.
func (c *MarketClient) Login(ctx context.Context, username, password string) (string, error) {
	reqBody := map[string]string{
		"username": username,
		"password": password,
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login body: %w", err)
	}

	url := fmt.Sprintf("%s/api/account/auth/login", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read login response: %w", err)
	}

	var loginResp model.MarketLoginRsp
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}

	return loginResp.Token, nil
}

// PublishTemplate publishes a template to the market.
func (c *MarketClient) PublishTemplate(ctx context.Context, token string, userID string, req *model.PublishTemplateReq) (*model.MarketPublishApiResponse, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	// ...
	url := fmt.Sprintf("%s/api/market/templates/publish", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if userID != "" {
		httpReq.Header.Set("X-User-Id", userID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()
	// ...
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResp model.MarketPublishApiResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse market response: %w (%s)", err, string(bodyBytes))
	}

	return &apiResp, nil
}

// CheckTemplateExists checks if a template with the given name+version already exists on the market.
func (c *MarketClient) CheckTemplateExists(ctx context.Context, token string, name, version string) (bool, error) {
	url := fmt.Sprintf("%s/api/market/templates?name=%s&version=%s", c.baseURL, name, version)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create check request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("check request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read check response: %w", err)
	}

	var result struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return false, nil // 解析失败不阻止发布
	}

	return result.Data.Total > 0, nil
}

// ListMarketTemplates fetches the list of templates from the market (public, no token needed).
func (c *MarketClient) ListMarketTemplates(ctx context.Context, keyword, category, sortBy string, page, pageSize int) (interface{}, error) {
	url := fmt.Sprintf("%s/api/market/templates?page=%d&page_size=%d", c.baseURL, page, pageSize)
	if keyword != "" {
		url += "&keyword=" + keyword
	}
	if category != "" {
		url += "&category=" + category
	}
	if sortBy != "" {
		url += "&sort_by=" + sortBy
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read list response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse list response: %w", err)
	}

	// 转换结构以匹配前端习惯：将 market-service 的 data 字段映射为 list，并提取 total
	// market-service 格式: {code:0, data:[], total:N, page:1, page_size:12}
	// 期望输出格式: {list:[], total:N, page:1, page_size:12}
	flattened := make(map[string]interface{})
	if data, ok := result["data"]; ok && data != nil {
		flattened["list"] = data
	} else {
		flattened["list"] = []interface{}{}
	}
	if total, ok := result["total"]; ok {
		flattened["total"] = total
	}
	if page, ok := result["page"]; ok {
		flattened["page"] = page
	}
	if pageSize, ok := result["page_size"]; ok {
		flattened["page_size"] = pageSize
	}

	return flattened, nil
}

// GetMarketTemplateDetail fetches a single template's detail from the market.
func (c *MarketClient) GetMarketTemplateDetail(ctx context.Context, marketTemplateID string) (interface{}, error) {
	url := fmt.Sprintf("%s/api/market/templates/%s", c.baseURL, marketTemplateID)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create detail request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("detail request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read detail response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse detail response: %w", err)
	}

	// 提取内层数据，去掉 code 包装
	if data, ok := result["data"].(map[string]interface{}); ok {
		// 如果包含 template 和 versions，则进行合并以适配前端
		if tpl, ok := data["template"].(map[string]interface{}); ok {
			if vers, ok := data["versions"]; ok {
				tpl["versions"] = vers
			}
			return tpl, nil
		}
		return data, nil
	}

	return result, nil
}

// DownloadTemplate downloads the full template definition (with device model) from the market.
func (c *MarketClient) DownloadTemplate(ctx context.Context, token string, marketTemplateID string, version string) (*model.MarketTemplateFullData, error) {
	url := fmt.Sprintf("%s/api/market/templates/%s/download", c.baseURL, marketTemplateID)
	if version != "" {
		url += "?version=" + version
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Code int                          `json:"code"`
		Data model.MarketTemplateFullData `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse download response: %w", err)
	}

	return &result.Data, nil
}

// InstallTemplate notifies the market service that a template has been installed.
// InstallTemplate notifies the market service that a template has been installed.
func (c *MarketClient) InstallTemplate(ctx context.Context, token string, marketTemplateID string, versionID string, userID string, orgID string) error {
	url := fmt.Sprintf("%s/api/market/templates/%s/install", c.baseURL, marketTemplateID)
	reqBody := map[string]string{
		"version_id": versionID,
	}
	reqBytes, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if userID != "" {
		httpReq.Header.Set("X-User-Id", userID)
		httpReq.Header.Set("X-Org-Id", orgID)
		// For market installations, we permit the installer to act as an org_admin locally
		httpReq.Header.Set("X-Roles", "org_admin,super_admin")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("install notification failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

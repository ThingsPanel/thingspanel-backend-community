package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ThingsVisClient handles communication with the ThingsVis service for dashboard export.
type ThingsVisClient struct {
	baseURL       string
	internalToken string
	httpClient    *http.Client
}

var (
	// ErrThingsVisServiceUnavailable indicates ThingsVis service is unreachable.
	ErrThingsVisServiceUnavailable = errors.New("thingsvis service unavailable")
	// ErrThingsVisRequestRejected indicates non-200 HTTP responses from ThingsVis.
	ErrThingsVisRequestRejected = errors.New("thingsvis request rejected")
	// ErrThingsVisInvalidResponse indicates malformed ThingsVis response.
	ErrThingsVisInvalidResponse = errors.New("thingsvis response invalid")
)

// NewThingsVisClient creates a new ThingsVis client using configs from viper.
func NewThingsVisClient() *ThingsVisClient {
	baseURL := viper.GetString("thingsvis.base_url")
	if baseURL == "" {
		baseURL = "http://thingsvis-server:8000"
	}

	return &ThingsVisClient{
		baseURL:       baseURL,
		internalToken: viper.GetString("thingsvis.internal_token"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewThingsVisClientWithBaseURL creates a client for the current public ThingsVis proxy.
func NewThingsVisClientWithBaseURL(baseURL string) *ThingsVisClient {
	return &ThingsVisClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ThingsVisDeviceReference is a real device reference discovered in one dashboard.
type ThingsVisDeviceReference struct {
	SourceDeviceID   string   `json:"sourceDeviceId"`
	SourceDeviceName string   `json:"sourceDeviceName,omitempty"`
	DataSourceIDs    []string `json:"dataSourceIds"`
	FieldIdentifiers []string `json:"fieldIdentifiers"`
}

// ThingsVisAnalyzeResponse is returned by the internal dashboard analysis API.
type ThingsVisAnalyzeResponse struct {
	Dashboard struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"dashboard"`
	DeviceReferences []ThingsVisDeviceReference `json:"deviceReferences"`
}

// ThingsVisDeviceRole maps a publisher device to a stable market role.
type ThingsVisDeviceRole struct {
	SourceDeviceID string `json:"sourceDeviceId"`
	BindingKey     string `json:"bindingKey"`
	DisplayName    string `json:"displayName,omitempty"`
}

// ThingsVisMarketSnapshot is the portable dashboard representation.
type ThingsVisMarketSnapshot struct {
	Name          string          `json:"name"`
	SchemaVersion string          `json:"schemaVersion"`
	CanvasConfig  json.RawMessage `json:"canvasConfig"`
	Nodes         json.RawMessage `json:"nodes"`
	DataSources   json.RawMessage `json:"dataSources"`
	Variables     json.RawMessage `json:"variables"`
}

// ThingsVisMarketExportResponse is returned by the internal export API.
type ThingsVisMarketExportResponse struct {
	Snapshot         ThingsVisMarketSnapshot    `json:"snapshot"`
	DeviceReferences []ThingsVisDeviceReference `json:"deviceReferences"`
}

// AnalyzeMarketDashboard discovers all real device references in a tenant-owned dashboard.
func (c *ThingsVisClient) AnalyzeMarketDashboard(ctx context.Context, dashboardID, tenantID, userID, authorization string) (*ThingsVisAnalyzeResponse, error) {
	var result ThingsVisAnalyzeResponse
	path := fmt.Sprintf("/market-dashboards/%s/analyze", dashboardID)
	if err := c.doMarketInternalJSON(ctx, http.MethodPost, path, tenantID, userID, authorization, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ExportMarketDashboard converts all publisher device references to bindingKey placeholders.
func (c *ThingsVisClient) ExportMarketDashboard(ctx context.Context, dashboardID, tenantID, userID, authorization string, roles []ThingsVisDeviceRole) (*ThingsVisMarketExportResponse, error) {
	var result ThingsVisMarketExportResponse
	body := struct {
		DeviceRoles []ThingsVisDeviceRole `json:"deviceRoles"`
	}{DeviceRoles: roles}
	path := fmt.Sprintf("/market-dashboards/%s/export", dashboardID)
	if err := c.doMarketInternalJSON(ctx, http.MethodPost, path, tenantID, userID, authorization, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *ThingsVisClient) doMarketInternalJSON(ctx context.Context, method, path, tenantID, userID, authorization string, input, output interface{}) error {
	if authorization == "" && c.internalToken == "" {
		return fmt.Errorf("%w: ThingsVis authorization is not provided", ErrThingsVisServiceUnavailable)
	}
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("failed to encode ThingsVis request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("failed to create ThingsVis request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	} else {
		req.Header.Set("X-Internal-Token", c.internalToken)
		req.Header.Set("X-Tenant-ID", tenantID)
		req.Header.Set("X-User-ID", userID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrThingsVisServiceUnavailable, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrThingsVisInvalidResponse, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: status=%d body=%s", ErrThingsVisRequestRejected, resp.StatusCode, compactBody(payload))
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return fmt.Errorf("%w: %v", ErrThingsVisInvalidResponse, err)
	}
	return nil
}

// DashboardExportRequest is the request to export a dashboard as a market template.
type DashboardExportRequest struct {
	DashboardID string `json:"dashboardId"`
	ExportMode  string `json:"exportMode"` // "market-template"
}

// DashboardExportResponse is the response from dashboard export API.
type DashboardExportResponse struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Data    *DashboardExportData `json:"data,omitempty"`
}

// DashboardExportData contains the exported dashboard template data.
type DashboardExportData struct {
	SchemaVersion  string          `json:"schemaVersion"`
	CanvasConfig   json.RawMessage `json:"canvasConfig"`
	Nodes          json.RawMessage `json:"nodes"`
	DataSources    json.RawMessage `json:"dataSources"`
	Variables      json.RawMessage `json:"variables"`
	DeviceBindings json.RawMessage `json:"deviceBindings"`
	FieldBindings  json.RawMessage `json:"fieldBindings,omitempty"`
}

// ExportDashboardForMarket exports a dashboard for use in market bundle templates.
func (c *ThingsVisClient) ExportDashboardForMarket(ctx context.Context, dashboardID string) (*DashboardExportData, error) {
	if dashboardID == "" {
		return nil, fmt.Errorf("dashboard ID is required")
	}

	url := fmt.Sprintf("%s/api/v1/dashboards/%s/export", c.baseURL, dashboardID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create export request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-Export-Mode", "market-template")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrThingsVisServiceUnavailable, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read export response: %v", ErrThingsVisInvalidResponse, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status=%d body=%s", ErrThingsVisRequestRejected, resp.StatusCode, compactBody(bodyBytes))
	}

	var exportResp DashboardExportResponse
	if err := json.Unmarshal(bodyBytes, &exportResp); err != nil {
		return nil, fmt.Errorf("%w: failed to parse export response: %v", ErrThingsVisInvalidResponse, err)
	}

	if exportResp.Code != 0 {
		return nil, fmt.Errorf("thingsvis export failed: code=%d message=%s", exportResp.Code, exportResp.Message)
	}

	if exportResp.Data == nil {
		return nil, fmt.Errorf("%w: empty data in export response", ErrThingsVisInvalidResponse)
	}

	return exportResp.Data, nil
}

// ExportDashboardForMarketWithMode exports a dashboard using specific export mode.
func (c *ThingsVisClient) ExportDashboardForMarketWithMode(ctx context.Context, dashboardID string, exportMode string) (*DashboardExportData, error) {
	if dashboardID == "" {
		return nil, fmt.Errorf("dashboard ID is required")
	}
	if exportMode == "" {
		exportMode = "market-template"
	}

	url := fmt.Sprintf("%s/api/v1/dashboards/%s/export?exportMode=%s", c.baseURL, dashboardID, exportMode)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create export request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrThingsVisServiceUnavailable, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read export response: %v", ErrThingsVisInvalidResponse, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status=%d body=%s", ErrThingsVisRequestRejected, resp.StatusCode, compactBody(bodyBytes))
	}

	var exportResp DashboardExportResponse
	if err := json.Unmarshal(bodyBytes, &exportResp); err != nil {
		return nil, fmt.Errorf("%w: failed to parse export response: %v", ErrThingsVisInvalidResponse, err)
	}

	if exportResp.Code != 0 {
		return nil, fmt.Errorf("thingsvis export failed: code=%d message=%s", exportResp.Code, exportResp.Message)
	}

	if exportResp.Data == nil {
		return nil, fmt.Errorf("%w: empty data in export response", ErrThingsVisInvalidResponse)
	}

	return exportResp.Data, nil
}

// compactBody returns a compact representation of body for error messages.
func compactBody(bodyBytes []byte) string {
	if len(bodyBytes) == 0 {
		return "<empty>"
	}
	body := string(bodyBytes)
	if len(body) > 256 {
		return body[:256] + "..."
	}
	return body
}

// ThingsVisImportRequest is the request structure for importing a dashboard
type ThingsVisImportRequest struct {
	DashboardSnapshot ThingsVisMarketSnapshot `json:"dashboardSnapshot"`
	DeviceBindings    []DeviceBindingImport   `json:"deviceBindings"`
	Name              string                  `json:"name,omitempty"`
	ProjectID         string                  `json:"projectId,omitempty"`
}

// FieldBindingImport represents a field binding in the import request
type FieldBindingImport struct {
	BindingKey string `json:"bindingKey"`
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
	Required   bool   `json:"required"`
}

// DeviceBindingImport represents a device binding in the import request
type DeviceBindingImport struct {
	BindingKey    string `json:"bindingKey"`
	LocalDeviceID string `json:"localDeviceId"`
}

// ThingsVisImportResponse identifies the concrete dashboard created by ThingsVis.
type ThingsVisImportResponse struct {
	DashboardID string `json:"dashboardId"`
	ProjectID   string `json:"projectId"`
}

type thingsVisProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type thingsVisProjectList struct {
	Data []thingsVisProject `json:"data"`
}

type thingsVisDashboard struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

type thingsVisCreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type thingsVisCreateDashboardRequest struct {
	Name         string          `json:"name"`
	ProjectID    string          `json:"projectId"`
	CanvasConfig json.RawMessage `json:"canvasConfig,omitempty"`
}

type thingsVisUpdateDashboardRequest struct {
	Name         string          `json:"name"`
	CanvasConfig json.RawMessage `json:"canvasConfig"`
	Nodes        json.RawMessage `json:"nodes"`
	DataSources  json.RawMessage `json:"dataSources"`
	Variables    json.RawMessage `json:"variables"`
}

// CreateDashboardFromSnapshot uses the same authenticated public API that the
// ThingsPanel browser client already uses. There is deliberately no dependency
// on an internal ThingsVis route, token, or Docker DNS name here.
func (c *ThingsVisClient) CreateDashboardFromSnapshot(
	ctx context.Context,
	authorization, name string,
	snapshot ThingsVisMarketSnapshot,
) (*ThingsVisImportResponse, error) {
	if authorization == "" {
		return nil, fmt.Errorf("%w: ThingsVis authorization is not provided", ErrThingsVisServiceUnavailable)
	}

	var projects thingsVisProjectList
	if err := c.doMarketInternalJSON(
		ctx,
		http.MethodGet,
		"/projects?page=1&limit=100",
		"",
		"",
		authorization,
		nil,
		&projects,
	); err != nil {
		return nil, err
	}

	const marketProjectName = "市场看板"
	projectID := ""
	for _, project := range projects.Data {
		if project.Name == marketProjectName {
			projectID = project.ID
			break
		}
	}
	if projectID == "" {
		var project thingsVisProject
		if err := c.doMarketInternalJSON(
			ctx,
			http.MethodPost,
			"/projects",
			"",
			"",
			authorization,
			thingsVisCreateProjectRequest{
				Name:        marketProjectName,
				Description: "从 ThingsPanel 看板模板创建",
			},
			&project,
		); err != nil {
			return nil, err
		}
		projectID = project.ID
	}
	if projectID == "" {
		return nil, fmt.Errorf("%w: project id is empty", ErrThingsVisInvalidResponse)
	}

	var dashboard thingsVisDashboard
	if err := c.doMarketInternalJSON(
		ctx,
		http.MethodPost,
		"/dashboards",
		"",
		"",
		authorization,
		thingsVisCreateDashboardRequest{
			Name:         name,
			ProjectID:    projectID,
			CanvasConfig: snapshot.CanvasConfig,
		},
		&dashboard,
	); err != nil {
		return nil, err
	}
	if dashboard.ID == "" {
		return nil, fmt.Errorf("%w: dashboard id is empty", ErrThingsVisInvalidResponse)
	}

	if err := c.doMarketInternalJSON(
		ctx,
		http.MethodPut,
		"/dashboards/"+dashboard.ID,
		"",
		"",
		authorization,
		thingsVisUpdateDashboardRequest{
			Name:         name,
			CanvasConfig: snapshot.CanvasConfig,
			Nodes:        snapshot.Nodes,
			DataSources:  snapshot.DataSources,
			Variables:    snapshot.Variables,
		},
		&dashboard,
	); err != nil {
		_ = c.DeleteDashboardWithAuthorization(ctx, dashboard.ID, authorization)
		return nil, err
	}

	return &ThingsVisImportResponse{
		DashboardID: dashboard.ID,
		ProjectID:   projectID,
	}, nil
}

// ImportDashboard imports a dashboard template into ThingsVis
func (c *ThingsVisClient) ImportDashboard(ctx context.Context, tenantID, userID string, req *ThingsVisImportRequest) (string, error) {
	response, err := c.ImportDashboardWithResult(ctx, tenantID, userID, "", req)
	if err != nil {
		return "", err
	}
	return response.DashboardID, nil
}

// ImportDashboardWithResult imports a template and preserves all identifiers
// returned by ThingsVis. ImportDashboard remains as a compatibility wrapper.
func (c *ThingsVisClient) ImportDashboardWithResult(
	ctx context.Context,
	tenantID, userID, authorization string,
	req *ThingsVisImportRequest,
) (*ThingsVisImportResponse, error) {
	var response ThingsVisImportResponse
	path := "/api/internal/market-dashboards/import"
	if authorization != "" {
		// Browser/user authorization goes through the public /thingsvis-api
		// proxy, whose upstream prefix is /api/v1.
		path = "/market-dashboards/import"
	}
	if err := c.doMarketInternalJSON(
		ctx,
		http.MethodPost,
		path,
		tenantID,
		userID,
		authorization,
		req,
		&response,
	); err != nil {
		return nil, err
	}
	if response.DashboardID == "" {
		return nil, fmt.Errorf("%w: dashboardId is empty", ErrThingsVisInvalidResponse)
	}
	return &response, nil
}

// DeleteDashboard deletes a dashboard from ThingsVis
func (c *ThingsVisClient) DeleteDashboard(ctx context.Context, tenantID, dashboardID string) error {
	url := fmt.Sprintf("%s/api/v1/dashboards/%s", c.baseURL, dashboardID)

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	httpReq.Header.Set("X-Tenant-ID", tenantID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrThingsVisServiceUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status=%d body=%s", ErrThingsVisRequestRejected, resp.StatusCode, string(body))
	}

	return nil
}

// DeleteDashboardWithAuthorization deletes a dashboard through the public API.
func (c *ThingsVisClient) DeleteDashboardWithAuthorization(ctx context.Context, dashboardID, authorization string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/dashboards/"+dashboardID, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authorization)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrThingsVisServiceUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status=%d body=%s", ErrThingsVisRequestRejected, resp.StatusCode, compactBody(body))
	}
	return nil
}

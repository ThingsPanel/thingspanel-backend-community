package communityclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ServicePlugin struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ServiceIdentifier string `json:"service_identifier"`
	ServiceType       int16  `json:"service_type"`
	ServiceHeartbeat  int    `json:"service_heartbeat"`
	LastActiveTime    string `json:"last_active_time"`
}

type ServicePluginUpsertReq struct {
	Name              string `json:"name"`
	ServiceIdentifier string `json:"service_identifier"`
	ServiceType       int16  `json:"service_type"`
	Description       string `json:"description"`
	Version           string `json:"version"`
	ServiceHeartbeat  int    `json:"service_heartbeat"`
	ServiceConfig     string `json:"service_config"`
}

type serviceListResp struct {
	Code int `json:"code"`
	Data struct {
		List []ServicePlugin `json:"list"`
	} `json:"data"`
}

func (c *Client) ListServicePlugins(ctx context.Context, serviceType int16) ([]ServicePlugin, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("page", "1")
	q.Set("page_size", "200")
	if serviceType > 0 {
		q.Set("service_type", strconv.Itoa(int(serviceType)))
	}
	path := "/api/v1/service/list?" + q.Encode()
	var out serviceListResp
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

func (c *Client) UpsertServicePluginByIdentifier(ctx context.Context, req ServicePluginUpsertReq) (string, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Name) == "" {
		return "", fmt.Errorf("name required")
	}
	if strings.TrimSpace(req.ServiceIdentifier) == "" {
		return "", fmt.Errorf("service_identifier required")
	}
	if req.ServiceType == 0 {
		req.ServiceType = 1
	}
	if req.ServiceHeartbeat <= 0 {
		req.ServiceHeartbeat = 1
	}
	if strings.TrimSpace(req.Version) == "" {
		req.Version = "v1.0.0"
	}
	if strings.TrimSpace(req.ServiceConfig) == "" {
		req.ServiceConfig = "{}"
	}

	plugins, err := c.ListServicePlugins(ctx, req.ServiceType)
	if err != nil {
		return "", err
	}
	var existingID string
	for _, p := range plugins {
		if strings.EqualFold(strings.TrimSpace(p.ServiceIdentifier), strings.TrimSpace(req.ServiceIdentifier)) {
			existingID = p.ID
			break
		}
	}
	if existingID == "" {
		for _, p := range plugins {
			if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(req.Name)) {
				existingID = p.ID
				break
			}
		}
	}

	body := map[string]any{
		"name":               req.Name,
		"service_identifier": req.ServiceIdentifier,
		"service_type":       req.ServiceType,
		"description":        req.Description,
		"version":            req.Version,
		"service_heartbeat":  req.ServiceHeartbeat,
		"service_config":     req.ServiceConfig,
	}
	method := http.MethodPost
	if existingID != "" {
		method = http.MethodPut
		body["id"] = existingID
	}
	raw, err := c.doRaw(ctx, method, "/api/v1/service", token, body)
	if err != nil {
		return "", err
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("service upsert invalid response: %s", string(raw))
	}
	return string(raw), nil
}

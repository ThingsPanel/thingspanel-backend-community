package communityclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

type RuntimeSummary struct {
	DeviceID        string     `json:"deviceId"`
	Connectivity    string     `json:"connectivity"`
	ObservedAt      *time.Time `json:"observedAt"`
	LastTelemetryAt *time.Time `json:"lastTelemetryAt"`
	Available       bool       `json:"available"`
}
type AlarmSummary struct {
	DeviceID    string     `json:"deviceId"`
	State       string     `json:"state"`
	ActiveCount *int64     `json:"activeCount"`
	ObservedAt  *time.Time `json:"observedAt"`
	Available   bool       `json:"available"`
}

func (c *Client) GetRuntimeSummaries(ctx context.Context, ids []string) ([]RuntimeSummary, error) {
	if _, ok := ctx.Value(authenticatedReadKey{}).(authenticatedRead); !ok {
		return nil, ErrRequestAuthentication
	}
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/device/status/evidence/batch", token, map[string]any{"deviceIds": ids})
	if err != nil {
		return nil, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, err
	}
	var out []RuntimeSummary
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// The base URL comes from Encore deployment metadata, never from request Host.
func (c *Client) GetAlarmSummaries(ctx context.Context, baseURL string, ids []string) ([]AlarmSummary, error) {
	if _, ok := ctx.Value(authenticatedReadKey{}).(authenticatedRead); !ok {
		return nil, ErrRequestAuthentication
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("alarm owner URL unavailable")
	}
	clone := *c
	clone.baseURL = strings.TrimRight(baseURL, "/")
	token, err := clone.Login(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := clone.doRaw(ctx, "POST", "/v1/device-alarm-summaries", token, map[string]any{"deviceIds": ids})
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []AlarmSummary `json:"items"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

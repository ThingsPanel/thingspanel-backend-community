package communityclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type telemetryCurrentResp struct {
	Code int               `json:"code"`
	Data []json.RawMessage `json:"data"`
}

type TelemetryPoint struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	Ts    string `json:"ts"`
	Value string `json:"value"`
}

func (c *Client) GetTelemetryCurrent(ctx context.Context, deviceID string) ([]TelemetryPoint, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/telemetry/datas/current/" + url.PathEscape(deviceID)
	var out telemetryCurrentResp
	if err := c.doJSON(ctx, "GET", path, token, nil, &out); err != nil {
		return nil, err
	}
	points := make([]TelemetryPoint, 0, len(out.Data))
	for _, raw := range out.Data {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		points = append(points, TelemetryPoint{
			Key:   toString(item["key"]),
			Label: toString(item["label"]),
			Unit:  toString(item["unit"]),
			Ts:    toString(item["ts"]),
			Value: fmt.Sprintf("%v", item["value"]),
		})
	}
	return points, nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

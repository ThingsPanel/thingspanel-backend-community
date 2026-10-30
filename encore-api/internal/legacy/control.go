package legacy

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

type controlReq struct {
	DeviceID string `json:"device_id"`
	Identify string `json:"identify"`
	Value    string `json:"value"`
}

type telemetryControlReq struct {
	DeviceID string `json:"device_id"`
	Value    string `json:"value"`
}

type controlResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type commandIdentifiersResp struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) PublishCommand(ctx context.Context, deviceID, identify, value string) (int, string, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return 0, "", err
	}
	var out controlResp
	err = c.doJSON(ctx, "POST", "/api/v1/command/datas/pub", token, controlReq{
		DeviceID: deviceID,
		Identify: identify,
		Value:    value,
	}, &out)
	if err != nil {
		return 0, "", err
	}
	return out.Code, out.Message, nil
}

func (c *Client) hasCommandIdentifiers(ctx context.Context, token, deviceID string) (bool, error) {
	var out commandIdentifiersResp
	if err := c.doJSON(ctx, "GET", "/api/v1/command/datas/"+url.PathEscape(deviceID), token, nil, &out); err != nil {
		return false, err
	}
	data := strings.TrimSpace(string(out.Data))
	if data == "" || data == "null" || data == "[]" {
		return false, nil
	}
	if strings.HasPrefix(data, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal(out.Data, &items); err != nil {
			return false, err
		}
		return len(items) > 0, nil
	}
	// Non-array non-empty payloads are treated as having command descriptors.
	return true, nil
}

func buildTelemetryControlValue(identify, value string) (string, error) {
	identify = strings.TrimSpace(identify)
	if identify == "" {
		identify = "switch"
	}
	value = strings.TrimSpace(value)

	var parsed any
	if value == "" {
		parsed = nil
	} else if json.Valid([]byte(value)) {
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return "", err
		}
	} else {
		parsed = value
	}
	payload := map[string]any{identify: parsed}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

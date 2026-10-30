package legacy

import (
	"context"
	"encoding/json"
	"net/url"
)

func (c *Client) GetTelemetryHistory(ctx context.Context, deviceID, key string, startTime, endTime int64, page, pageSize int) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	q.Set("key", key)
	q.Set("start_time", i64toa(startTime))
	q.Set("end_time", i64toa(endTime))
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	return c.doRaw(ctx, "GET", "/api/v1/telemetry/datas/history/page?"+q.Encode(), token, nil)
}

func (c *Client) GetTelemetryControlLogs(ctx context.Context, deviceID string, page, pageSize int) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	return c.doRaw(ctx, "GET", "/api/v1/telemetry/datas/set/logs?"+q.Encode(), token, nil)
}

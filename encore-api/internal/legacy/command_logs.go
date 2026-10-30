package legacy

import (
	"context"
	"encoding/json"
	"net/url"
)

func (c *Client) GetCommandLogs(ctx context.Context, deviceID string, page, pageSize int) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	return c.doRaw(ctx, "GET", "/api/v1/command/datas/set/logs?"+q.Encode(), token, nil)
}

func (c *Client) GetCommandIdentifiers(ctx context.Context, deviceID string) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "GET", "/api/v1/command/datas/"+url.PathEscape(deviceID), token, nil)
}

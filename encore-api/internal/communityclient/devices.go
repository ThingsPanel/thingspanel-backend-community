package communityclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

func (c *Client) GetDeviceDetail(ctx context.Context, deviceID string) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "GET", "/api/v1/device/detail/"+url.PathEscape(deviceID), token, nil)
}

func (c *Client) ListDevices(ctx context.Context, page, pageSize int) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	return c.doRaw(ctx, "GET", "/api/v1/device?"+q.Encode(), token, nil)
}

func (c *Client) ListDevicesSimple(ctx context.Context) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "GET", "/api/v1/device/list", token, nil)
}

func (c *Client) GetDeviceOnlineStatus(ctx context.Context, deviceID string) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "GET", "/api/v1/device/online/status/"+url.PathEscape(deviceID), token, nil)
}

func (c *Client) CreateDevice(ctx context.Context, body any) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "POST", "/api/v1/device", token, body)
}

func (c *Client) UpdateDevice(ctx context.Context, body any) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "PUT", "/api/v1/device", token, body)
}

func (c *Client) DeleteDevice(ctx context.Context, deviceID string) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "DELETE", "/api/v1/device/"+url.PathEscape(deviceID), token, nil)
}

func (c *Client) ActivateDevice(ctx context.Context, body any) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "PUT", "/api/v1/device/active", token, body)
}

func (c *Client) CheckDeviceNumber(ctx context.Context, deviceNumber string) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "GET", "/api/v1/device/check/"+url.PathEscape(deviceNumber), token, nil)
}

func (c *Client) DeviceSelector(ctx context.Context, page, pageSize int, search string) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	q.Set("search", search)
	return c.doRaw(ctx, "GET", "/api/v1/device/selector?"+q.Encode(), token, nil)
}

func (c *Client) UpdateDeviceVoucher(ctx context.Context, body any) (json.RawMessage, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	return c.doRaw(ctx, "POST", "/api/v1/device/update/voucher", token, body)
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// The helpers below are for the metadata runtime dispatcher. Each returns
// nil only when Community answered with a success envelope, so a caller can
// treat nil as "Community applied it".

// SetDeviceNumber changes the device number. Community checks uniqueness and
// rejects a number that belongs to another device.
func (c *Client) SetDeviceNumber(ctx context.Context, deviceID, deviceNumber string) error {
	raw, err := c.UpdateDevice(ctx, map[string]any{"id": deviceID, "device_number": deviceNumber})
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

// BindSubdevice attaches a device to a gateway at the given sub-device address.
func (c *Client) BindSubdevice(ctx context.Context, deviceID, gatewayID, address string) error {
	raw, err := c.UpdateDevice(ctx, map[string]any{"id": deviceID, "parent_id": gatewayID, "sub_device_addr": address})
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

// SetDeviceVoucher replaces the credential Community authenticates the device with.
// The voucher is the JSON text Community stores, for example {"username":"<token>"};
// once it changes the previous credential no longer logs in. Community refuses a
// voucher another device already uses.
func (c *Client) SetDeviceVoucher(ctx context.Context, deviceID, voucher string) error {
	token, err := c.Login(ctx)
	if err != nil {
		return err
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/device/update/voucher", token, map[string]any{"device_id": deviceID, "voucher": voucher})
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

// UnbindSubdevice detaches a sub-device from its gateway so it can be bound elsewhere.
// Community clears the gateway reference; repeating it for an already free device is harmless.
func (c *Client) UnbindSubdevice(ctx context.Context, deviceID string) error {
	token, err := c.Login(ctx)
	if err != nil {
		return err
	}
	raw, err := c.doRaw(ctx, "PUT", "/api/v1/device/sub-remove", token, map[string]any{"sub_device_id": deviceID})
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

// RemoveDevice deletes the device from Community.
func (c *Client) RemoveDevice(ctx context.Context, deviceID string) error {
	raw, err := c.DeleteDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

// DeviceGone reports true only when Community answers that the device does not
// exist. Any other outcome, including transport errors, is returned as an error
// so a caller never treats an unknown state as "deleted".
func (c *Client) DeviceGone(ctx context.Context, deviceID string) (bool, error) {
	raw, err := c.GetDeviceDetail(ctx, deviceID)
	if err == nil {
		_, err = decodeEnvelope(raw)
		if err == nil {
			return false, nil
		}
	}
	if errors.Is(err, ErrRejected) {
		return true, nil
	}
	return false, err
}

// SimulationSend asks Community to publish one JSON message as the device on the
// telemetry topic. No server, port or topic is passed on purpose: Community
// publishes to its own configured broker with the device's own credential.
func (c *Client) SimulationSend(ctx context.Context, deviceID, dataJSON string) error {
	token, err := c.Login(ctx)
	if err != nil {
		return err
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/telemetry/datas/simulation/send", token,
		map[string]any{"device_id": deviceID, "data": dataJSON})
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

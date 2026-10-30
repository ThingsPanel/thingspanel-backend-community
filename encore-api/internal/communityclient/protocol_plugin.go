package communityclient

import "context"

type ProtocolPluginCreateReq struct {
	Name                 string `json:"name"`
	DeviceType           int16  `json:"device_type"`
	ProtocolType         string `json:"protocol_type"`
	ServiceAccessAddress string `json:"service_access_address"`
	Description          string `json:"description"`
	AdditionalInfo       string `json:"additional_info"`
	LanguageCode         string `json:"language_code"`
}

func (c *Client) CreateProtocolPlugin(ctx context.Context, req ProtocolPluginCreateReq) (string, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return "", err
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/protocol_plugin", token, req)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

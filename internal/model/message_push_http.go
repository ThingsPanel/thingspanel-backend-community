package model

type CreateMessagePushReq struct {
	PushId     string `json:"pushId" validate:"required"`
	DeviceType string `json:"deviceType" validate:"required"`
}

type MessagePushMangeLogoutReq struct {
	PushId string `json:"pushId" validate:"required"`
}

type MessagePushConfigRes struct {
	Url string `json:"url"`
}

type MessagePushConfigReq struct {
	Url string `json:"url"`
}

type MessagePushSend struct {
	PushClientId string                            `json:"push_clientid"`
	Title        string                            `json:"title"`
	Content      string                            `json:"content"`
	AlarmId      *string                           `json:"alarm_id,omitempty"`
	Category     map[string]string                 `json:"category,omitempty"`
	Options      map[string]map[string]interface{} `json:"options,omitempty"`
}
type MessagePushSendPayload struct {
	AlarmConfigId string `json:"alarm_config_id"`
	TenantId      string `json:"tenant_id"`
}

type MessagePushSendRes struct {
	ErrCode interface{} `json:"errCode"`
	ErrMsg  string      `json:"errMsg"`
	Data    interface{} `json:"data,omitempty"`
}

package model

type CreateProtocolPluginReq struct {
	Name           string  `json:"name" validate:"required,max=36"`
	DeviceType     int16   `json:"device_type" validate:"required,max=10"`
	ProtocolType   string  `json:"protocol_type" validate:"required,max=50"`
	AccessAddress  *string `json:"access_address" validate:"omitempty,max=500"`
	HTTPAddress    *string `json:"http_address" validate:"omitempty,max=500"`
	SubTopicPrefix *string `json:"sub_topic_prefix" validate:"omitempty,max=500"`
	Description    *string `json:"description" validate:"omitempty,max=500"`
	AdditionalInfo *string `json:"additional_info" validate:"omitempty,max=1000"`
	Remark         *string `json:"remark" validate:"omitempty,max=255"`

	LanguageCode string `json:"language_code"  validate:"required,max=36"`
}

type UpdateProtocolPluginReq struct {
	Id             string  `json:"id" validate:"required,max=36"`
	Name           string  `json:"name" validate:"required,max=36"`
	DeviceType     int16   `json:"device_type" validate:"required,max=10"`
	ProtocolType   string  `json:"protocol_type" validate:"required,max=50"`
	AccessAddress  *string `json:"access_address" validate:"omitempty,max=500"`
	HTTPAddress    *string `json:"http_address" validate:"omitempty,max=500"`
	SubTopicPrefix *string `json:"sub_topic_prefix" validate:"omitempty,max=500"`
	Description    *string `json:"description" validate:"omitempty,max=500"`
	AdditionalInfo *string `json:"additional_info" validate:"omitempty,max=1000"`
	Remark         *string `json:"remark" validate:"omitempty,max=255"`

	LanguageCode string `json:"language_code"  validate:"required,max=36"`
}

type GetProtocolPluginListByPageReq struct {
	PageReq
}

type GetProtocolPluginFormReq struct {
	DeviceId string `json:"device_id"  form:"device_id" validate:"required,max=36"`
}

type GetProtocolPluginFormByProtocolType struct {
	ProtocolType string `json:"protocol_type"  form:"protocol_type" validate:"required,max=255"`
	DeviceType   string `json:"device_type"  form:"device_type" validate:"required,max=10"`
}

// 协议插件获取设备配置请求
type GetDeviceConfigReq struct {
	DeviceId     string `json:"device_id"  form:"device_id" validate:"omitempty,max=36"`
	Voucher      string `json:"voucher"  form:"voucher" validate:"omitempty,max=255"`
	DeviceNumber string `json:"device_number"  form:"device_number" validate:"omitempty,max=255"`
}

// 协议插件获取设备配置
type DeviceConfigForProtocolPlugin struct {
	ID                     string                             `json:"id"`
	Voucher                string                             `json:"voucher"`
	DeviceType             string                             `json:"device_type"`
	ProtocolType           string                             `json:"protocol_type"`
	DeviceNumber           string                             `json:"device_number"`
	Config                 map[string]interface{}             `json:"config"`
	ProtocolConfigTemplate map[string]interface{}             `json:"protocol_config_template"` // 设备模板的protocol_config
	SubDivices             []SubDeviceConfigForProtocolPlugin `json:"sub_devices"`
}

// 协议插件获取设备配置的子设备配置
type SubDeviceConfigForProtocolPlugin struct {
	DeviceID               string                 `json:"device_id"`
	DeviceNumber           string                 `json:"device_number"`
	Voucher                string                 `json:"voucher"`
	SubDeviceAddr          string                 `json:"sub_device_addr"`
	Config                 map[string]interface{} `json:"config"`
	ProtocolConfigTemplate map[string]interface{} `json:"protocol_config_template"` // 子设备模板的protocol_config
}

type GetDevicesByProtocolPluginRsp struct {
	List  []DeviceConfigForProtocolPlugin `json:"list"`
	Total int64                           `json:"total"`
}

type GetDevicesByProtocolPluginReq struct {
	ServiceIdentifier string `json:"service_identifier"  form:"service_identifier" validate:"required,max=255"`
	DeviceType        string `json:"device_type"  form:"device_type" validate:"required,max=10"`
	PageReq
}

package model

// DeviceAuthReq 设备动态认证请求结构体
type DeviceAuthReq struct {
	TemplateSecret string  `json:"template_secret" validate:"required,max=255"` // 模板密钥
	DeviceNumber   string  `json:"device_number" validate:"required,max=255"`   // 设备唯一标识
	DeviceName     *string `json:"device_name" validate:"omitempty,max=255"`    // 设备名称(可选)
	ProductKey     *string `json:"product_key" validate:"omitempty,max=255"`    // 产品密钥(可选，用于产品关联)
}

// DeviceAuthRes 设备动态认证响应结构体
type DeviceAuthRes struct {
	DeviceID string `json:"device_id"` // 设备ID
	Voucher  string `json:"voucher"`   // 设备凭证
}

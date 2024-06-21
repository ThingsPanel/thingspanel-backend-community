package model

import "time"

type CreateDeviceConfigReq struct {
	Name             string  `json:"name"  validate:"required,max=99"`                  // 设备配置名称
	DeviceTemplateId *string `json:"device_template_id" validate:"omitempty,max=36"`    // 设备模板id
	DeviceType       string  `json:"device_type" validate:"required,max=9,oneof=1 2 3"` // 设备类型
	ProtocolType     *string `json:"protocol_type" validate:"omitempty,max=36"`         // 协议类型 （枚举在字典里获取）
	VoucherType      *string `json:"voucher_type" validate:"omitempty,max=36"`          // 凭证类型 凭证类型（没有具体的枚举，不同协议有不同的类型）
	ProtocolConfig   *string `json:"protocol_config" validate:"omitempty"`              // 协议配置
	DeviceConnType   *string `json:"device_conn_type" validate:"omitempty,oneof=A B"`   // 设备连接方式（默认A）A-设备连接平台B-平台连接设备
	AdditionalInfo   *string `json:"additional_info" validate:"omitempty"`              // 附加信息
	Description      *string `json:"description" validate:"omitempty,max=255"`          // 描述
	Remark           *string `json:"remark" validate:"omitempty,max=255"`               // 备注
}

type UpdateDeviceConfigReq struct {
	Id               string     `json:"id" validate:"required,max=36"`                   // 设备配置id
	Name             *string    `json:"name"  validate:"omitempty,max=99"`               // 设备配置名称
	DeviceTemplateId *string    `json:"device_template_id" validate:"omitempty,max=36"`  // 设备模板id
	ProtocolType     *string    `json:"protocol_type" validate:"omitempty,max=100"`      // 协议类型 （枚举在字典里获取）
	VoucherType      *string    `json:"voucher_type" validate:"omitempty,max=500"`       // 凭证类型 凭证类型（没有具体的枚举，不同协议有不同的类型）
	ProtocolConfig   *string    `json:"protocol_config" validate:"omitempty"`            // 协议配置
	DeviceConnType   *string    `json:"device_conn_type" validate:"omitempty,oneof=A B"` // 设备连接方式（默认A）A-设备连接平台B-平台连接设备
	AdditionalInfo   *string    `json:"additional_info" validate:"omitempty"`            // 附加信息
	Description      *string    `json:"description" validate:"omitempty,max=255"`        // 描述
	Remark           *string    `json:"remark" validate:"omitempty,max=255"`             // 备注
	UpdatedAt        *time.Time `json:"updated_at" validate:"omitempty"`                 // 更新时间
	OtherConfig      *string    `json:"other_config" validate:"omitempty"`               //其他配置
}

type GetDeviceConfigListByPageReq struct {
	PageReq
	DeviceTemplateId *string `json:"device_template_id" form:"device_template_id" validate:"omitempty,max=36"` // 设备模板id
	DeviceType       *string `json:"device_type" form:"device_type" validate:"omitempty,max=9,oneof=1 2 3"`    // 设备类型
	ProtocolType     *string `json:"protocol_type" form:"protocol_type" validate:"omitempty,max=36"`           // 协议类型
	Name             *string `json:"name" form:"name" validate:"omitempty,max=99"`                             // 设备配置名称
}

type GetDeviceConfigListMenuReq struct {
	DeviceConfigName *string `json:"device_config_name" form:"device_config_name" validate:"omitempty,max=99"` // 设备配置名称
	DeviceType       *string `json:"device_type" form:"device_type" validate:"omitempty,max=9,oneof=1 2 3"`    // 设备类型
	ProtocolType     *string `json:"protocol_type" form:"protocol_type" validate:"omitempty,max=50"`           // 协议类型
}

type BatchUpdateDeviceConfigReq struct {
	DeviceConfigID string   `json:"device_config_id" validate:"required,uuid"` // 设备配置id
	DeviceIds      []string `json:"device_ids" validate:"omitempty,max=36"`    // 设备id数组
}

type DeviceConfigRsp struct {
	*DeviceConfig
	DeviceCount int64 `json:"device_count"`
}

type DeviceConfigConnectRes struct {
	AccessToken string `json:"AccessToken接入"`
	Basic       string `json:"Basic"`
}

// DeviceConfigsRes
// 固定返回设备配置详情页返参
type DeviceConfigsRes struct {
	ID               string    `json:"id"`                 // Id
	Name             string    `json:"name"`               // 名称
	DeviceTemplateID string    `json:"device_template_id"` // 设备模板id
	DeviceType       string    `json:"device_type"`        // 设备类型
	ProtocolType     string    `json:"protocol_type"`      // 协议类型
	VoucherType      string    `json:"voucher_type"`       // 凭证类型
	ProtocolConfig   string    `json:"protocol_config"`    // 协议表单配置
	DeviceConnType   string    `json:"device_conn_type"`   // 设备连接方式（默认A）A-设备连接平台B-平台连接设备
	AdditionalInfo   string    `json:"additional_info"`    // 附加信息
	Description      string    `json:"description"`        // 描述
	CreatedAt        time.Time `json:"created_at"`         // 创建时间
	UpdatedAt        time.Time `json:"updated_at"`         // 更新时间
	Remark           string    `json:"remark"`             // 备注
}

type DeviceOnline struct {
	DeviceConfigId *string `json:"device_config_id"`
	DeviceId       string  `json:"device_id"`
	//Online         int     `json:"online"`
	//OtherConfig    *DeviceConfigOtherConfig `json:"other_config"`
}

type DeviceConfigOtherConfig struct {
	OnlineTimeout int `json:"online_timeout"` //在线超时时间  分
	Heartbeat     int `json:"heartbeat"`      //心跳 秒
}

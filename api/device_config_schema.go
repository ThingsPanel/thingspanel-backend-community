package api

import (
	"time"
)

// // DeviceConfig mapped from table <device_configs>
// type DeviceConfig struct {
// 	ID               string    `gorm:"column:id;primaryKey;comment:Id" json:"id"`                                           // Id
// 	Name             string    `gorm:"column:name;not null;comment:名称" json:"name"`                                         // 名称
// 	DeviceTemplateID *string   `gorm:"column:device_template_id;comment:设备模板id" json:"device_template_id"`                  // 设备模板id
// 	DeviceType       string    `gorm:"column:device_type;not null;comment:设备类型" json:"device_type"`                         // 设备类型
// 	ProtocolType     *string   `gorm:"column:protocol_type;comment:协议类型" json:"protocol_type"`                              // 协议类型
// 	VoucherType      *string   `gorm:"column:voucher_type;comment:凭证类型" json:"voucher_type"`                                // 凭证类型
// 	ProtocolConfig   *string   `gorm:"column:protocol_config;comment:协议表单配置" json:"protocol_config"`                        // 协议表单配置
// 	DeviceConnType   *string   `gorm:"column:device_conn_type;comment:设备连接方式（默认A）A-设备连接平台B-平台连接设备" json:"device_conn_type"` // 设备连接方式（默认A）A-设备连接平台B-平台连接设备
// 	AdditionalInfo   *string   `gorm:"column:additional_info;default:{};comment:附加信息" json:"additional_info"`               // 附加信息
// 	Description      *string   `gorm:"column:description;comment:描述" json:"description"`                                    // 描述
// 	TenantID         string    `gorm:"column:tenant_id;not null;comment:租户id" json:"tenant_id"`                             // 租户id
// 	CreatedAt        time.Time `gorm:"column:created_at;not null;comment:创建时间" json:"created_at"`                           // 创建时间
// 	UpdatedAt        time.Time `gorm:"column:updated_at;not null;comment:更新时间" json:"updated_at"`                           // 更新时间
// 	Remark           *string   `gorm:"column:remark;comment:备注" json:"remark"`                                              // 备注
// }


type DeviceConfigReadSchema struct {
	ID               string    `json:"id"`                                           // Id
	Name             string    `json:"name"`                                         // 名称
	DeviceTemplateID *string   `json:"device_template_id"`                          // 设备模板id
	DeviceType       string    `json:"device_type"`                                  // 设备类型
	ProtocolType     *string   `json:"protocol_type"`                                // 协议类型
	VoucherType      *string   `json:"voucher_type"`                                 // 凭证类型
	ProtocolConfig   *string   `json:"protocol_config"`                              // 协议表单配置
	DeviceConnType   *string   `json:"device_conn_type"`                            // 设备连接方式（默认A）A-设备连接平台B-平台连接设备
	AdditionalInfo   *string   `json:"additional_info"`                              // 附加信息
	Description      *string   `json:"description"`                                  // 描述
	TenantID         string    `json:"tenant_id"`                                    // 租户id
	CreatedAt        time.Time `json:"created_at"`                                   // 创建时间
	UpdatedAt        time.Time `json:"updated_at"`                                   // 更新时间
	Remark           *string   `json:"remark"`                                       // 备注
}


type GetDeviceConfigResponse struct {
	Code    int                       `json:"code" example:"200"`
	Message string                    `json:"message" example:"success"`
	Data    DeviceConfigReadSchema `json:"data"`
}


type GetDeviceConfigListResponse struct {
	Code    int                       `json:"code" example:"200"`
	Message string                    `json:"message" example:"success"`
	Data    GetDeviceConfigListData `json:"data"`
}

type GetDeviceConfigListData struct {
	Total int64                      `json:"total"`
	List  []DeviceTemplateReadSchema `json:"list"`
}

package model

import "time"

type CreateDeviceReq struct {
	Name           *string `json:"name" validate:"omitempty,max=255"`            // 设备名称
	Voucher        *string `json:"voucher" validate:"omitempty,max=500"`         // 凭证
	DeviceNumber   *string `json:"device_number" validate:"omitempty,max=36"`    // 设备编号
	ProductID      *string `json:"product_id" validate:"omitempty,max=36"`       // 产品ID
	ParentID       *string `json:"parent_id" validate:"omitempty,max=36"`        // 父设备ID
	Protocol       *string `json:"protocol" validate:"omitempty,max=36"`         // 协议
	Label          *string `json:"label" validate:"omitempty,max=255"`           // 标签
	Location       *string `json:"location" validate:"omitempty,max=36"`         // 位置
	SubDeviceAddr  *string `json:"sub_device_addr" validate:"omitempty,max=36"`  // 子设备地址
	CurrentVersion *string `json:"current_version" validate:"omitempty,max=36"`  // 当前版本
	AdditionalInfo *string `json:"additional_info" validate:"omitempty"`         // 附加信息
	ProtocolConfig *string `json:"protocol_config" validate:"omitempty"`         // 协议配置
	Remark1        *string `json:"remark1" validate:"omitempty,max=255"`         // 备注1
	Remark2        *string `json:"remark2" validate:"omitempty,max=255"`         // 备注2
	Remark3        *string `json:"remark3" validate:"omitempty,max=255"`         // 备注3
	DeviceConfigId *string `json:"device_config_id" validate:"omitempty,max=36"` // 设备配置ID
	AccessWay      *string `json:"access_way" validate:"omitempty,max=36"`       // 接入方式
	Description    *string `json:"description" validate:"omitempty,max=500"`     // 接入方式
}

type BatchCreateDeviceReq struct {
	ServiceAccessId string `json:"service_access_id" validate:"required,max=36"` // 服务接入点ID
	DeviceList      []struct {
		DeviceName     string  `json:"device_name" validate:"required,max=255"`      // 设备名称
		DeviceNumber   string  `json:"device_number" validate:"required,max=36"`     // 设备编号
		Description    *string `json:"description" validate:"omitempty,max=500"`     // 描述
		DeviceConfigId string  `json:"device_config_id" validate:"omitempty,max=36"` // 设备配置ID
	} `json:"device_list" validate:"required"`
}

type UpdateDeviceReq struct {
	Id             string  `json:"id" validate:"required,max=36"`                // 设备ID
	Name           *string `json:"name" validate:"omitempty,max=255"`            // 设备名称
	Voucher        *string `json:"voucher" validate:"omitempty,max=500"`         // 凭证
	DeviceNumber   *string `json:"device_number" validate:"omitempty,max=36"`    // 设备编号
	ProductID      *string `json:"product_id" validate:"omitempty,max=36"`       // 产品ID
	ParentID       *string `json:"parent_id" validate:"omitempty,max=36"`        // 父设备ID
	Label          *string `json:"label" validate:"omitempty,max=255"`           // 标签
	Location       *string `json:"location" validate:"omitempty,max=100"`        // 位置
	SubDeviceAddr  *string `json:"sub_device_addr" validate:"omitempty,max=36"`  // 子设备地址
	CurrentVersion *string `json:"current_version" validate:"omitempty,max=36"`  // 当前版本
	AdditionalInfo *string `json:"additional_info" validate:"omitempty"`         // 附加信息
	ProtocolConfig *string `json:"protocol_config" validate:"omitempty"`         // 协议配置
	Remark1        *string `json:"remark1" validate:"omitempty,max=255"`         // 备注1
	Remark2        *string `json:"remark2" validate:"omitempty,max=255"`         // 备注2
	Remark3        *string `json:"remark3" validate:"omitempty,max=255"`         // 备注3
	DeviceConfigId *string `json:"device_config_id" validate:"omitempty,max=36"` // 设备配置ID
	AccessWay      *string `json:"access_way" validate:"omitempty,max=36"`       // 接入方式
	Description    *string `json:"description" validate:"omitempty,max=500"`     // 接入方式
	IsOnline       *int16  `json:"is_online" validate:"omitempty"`               // 是否在线
}

type ActiveDeviceReq struct {
	DeviceNumber string `json:"device_number" validate:"required,max=36"` // 设备编号
	Name         string `json:"name" validate:"max=255"`                  // 设备名称
}

type GetDeviceListByPageReq struct {
	PageReq
	ActivateFlag   *string `json:"activate_flag" form:"activate_flag" validate:"omitempty,max=36"`       // 激活状态
	DeviceNumber   *string `json:"device_number" form:"device_number" validate:"omitempty,max=36"`       // 设备编号
	IsEnabled      *string `json:"is_enabled" form:"is_enabled" validate:"omitempty,max=36"`             // 是否启用
	ProductID      *string `json:"product_id" form:"product_id" validate:"omitempty,max=36"`             // 产品ID
	ProtocolType   *string `json:"protocol_type" form:"protocol_type" validate:"omitempty,max=36"`       // 协议
	Label          *string `json:"label" form:"label" validate:"omitempty,max=255"`                      // 标签
	Name           *string `json:"name" form:"name" validate:"omitempty,max=255"`                        // 设备名称
	CurrentVersion *string `json:"current_version" form:"current_version" validate:"omitempty,max=36"`   // 当前版本
	GroupId        *string `json:"group_id" form:"group_id" validate:"omitempty,max=36"`                 //组id
	DeviceConfigId *string `json:"device_config_id" form:"device_config_id" validate:"omitempty,max=36"` // 设备配置ID
	IsOnline       *int    `json:"is_online" form:"is_online" validate:"omitempty,max=36"`               // 组id
	WarnStatus     *string `json:"warn_status" form:"warn_status" validate:"omitempty,max=36"`           // 预留 TODO
	Search         *string `json:"search" form:"search" validate:"omitempty,max=36"`                     // 设备名称或编号的模糊匹配
	AccessWay      *string `json:"access_way" form:"access_way" validate:"omitempty,max=36"`             // 接入方式
	BatchNumber    *string `json:"batch_number" form:"batch_number" validate:"omitempty"`
	DeviceType     *string `json:"device_type" form:"device_type" validate:"omitempty,oneof=1 2 3"` // 设备类型
}

type GetDeviceListByPageRsp struct {
	ID               string     `json:"id"`                 // 设备ID
	DeviceNumber     string     `json:"device_number"`      // 设备编号
	Name             string     `json:"name"`               // 设备名称
	DeviceConfigID   string     `json:"device_config_id"`   // 设备配置ID
	DeviceConfigName string     `json:"device_config_name"` // 设备配置名称
	Ts               *time.Time `json:"ts"`                 // 上次推送时间
	ActivateFlag     string     `json:"activate_flag"`      // 激活状态
	ActivateAt       *time.Time `json:"activate_at"`        // 激活时间
	BatchNumber      string     `json:"batch_number"`       // 批次编号
	CurrentVersion   string     `json:"current_version"`    // 当前版本
	CreatedAt        *time.Time `json:"created_at"`         // 创建时间
	IsOnline         int        `json:"is_online"`          // 是否在线
	Location         string     `json:"location"`           // 位置
	AccessWay        string     `json:"access_way"`         // 接入方式
	ProtocolType     string     `json:"protocol_type"`      // 协议类型
	DeviceStatus     int        `json:"device_status"`      // 设备状态
	WarnStatus       string     `json:"warn_status"`        //设备是否告警 Y告警 N未告警
}

type CreateDeviceGroupReq struct {
	ParentId    *string `json:"parent_id" validate:"omitempty,max=36"`    // 父设备组ID
	Name        string  `json:"name" validate:"required,max=255"`         // 设备组名称
	Description *string `json:"description" validate:"omitempty,max=255"` // 描述
	Remark      *string `json:"remark" validate:"omitempty,max=255"`      // 备注
}

type UpdateDeviceGroupReq struct {
	Id          string  `json:"id" validate:"required,max=36"`            // 设备组ID
	ParentId    string  `json:"parent_id" validate:"required,max=36"`     // 父设备组ID
	Name        string  `json:"name" validate:"required,max=255"`         // 设备组名称
	Description *string `json:"description" validate:"omitempty,max=255"` // 描述
	Remark      *string `json:"remark" validate:"omitempty,max=255"`      // 备注
}

type GetDeviceGroupsListByPageReq struct {
	PageReq
	ParentId *string `json:"parent_id" form:"parent_id" validate:"omitempty,max=36"` // 父设备组ID
	Name     *string `json:"name" form:"name" validate:"omitempty,max=255"`          // 设备组名称
}

type GetDeviceListByGroup struct {
	PageReq
	GroupId string `json:"group_id" form:"group_id" validate:"required,max=36"` // 设备组ID
}

type GetDeviceListByGroupRsp struct {
	GroupId            string `json:"group_id"`
	Id                 string `json:"id"`
	DeviceNumber       string `json:"device_number"`
	Name               string `json:"name"`
	Device_config_name string `json:"device_config_name"`
}

type GetDeviceGroupListByDeviceIdReq struct {
	DeviceId string `json:"device_id" form:"device_id" validate:"required,max=36"` // 父设备组ID
}

type CreateDeviceGroupRelationReq struct {
	GroupId      string   `json:"group_id" validate:"required,max=36"` // 设备组ID
	DeviceIDList []string `json:"device_id_list" validate:"required"`  // 设备ID列表
}

type DeleteDeviceGroupRelationReq struct {
	GroupId  string `json:"group_id" form:"group_id" validate:"required,max=36"`   // 设备组ID
	DeviceId string `json:"device_id" form:"device_id" validate:"required,max=36"` // 设备ID
}

type CreateDevicePreRegisterReq struct {
	ProductID      string  `json:"product_id" validate:"required,max=36"`             // 产品ID
	BatchNumber    string  `json:"batch_number" validate:"required,max=36"`           // 批次编号
	CurrentVersion *string `json:"current_version" validate:"omitempty,max=36"`       // 固件版本
	DeviceCount    *int    `json:"device_count" validate:"omitempty,min=1,max=10000"` // 设备数量，添加类型为1时必填
	CreateType     string  `json:"create_type" validate:"required,oneof=1 2"`         // 添加类型1-自动 2-文件
	BatchFile      *string `json:"batch_file" validate:"omitempty,max=500"`           // 批次文件
}

type GetDevicePreRegisterListByPageReq struct {
	PageReq
	ProductID      string  `json:"product_id" form:"product_id" validate:"omitempty,max=36"`                       // 产品ID
	BatchNumber    *string `json:"batch_number" form:"batch_number" validate:"omitempty"`                          // 批次编号
	DeviceNumber   *string `json:"device_number" form:"device_number" validate:"omitempty"`                        // 设备编号
	IsEnabled      *string `json:"is_enabled" form:"is_enabled" validate:"omitempty"`                              // 是否启用
	ActivateFlag   *string `json:"activate_flag"  form:"activate_flag" validate:"omitempty,oneof=active inactive"` // 激活状态
	Name           *string `json:"name"  form:"name" validate:"omitempty"`                                         //
	DeviceConfigID *string `json:"device_config_id"  form:"device_config_id" validate:"omitempty"`                 //设备配置                  //
}

type GetDevicePreRegisterListByPageRsp struct {
	ID             string     `json:"id"`              // 设备ID
	Name           string     `json:"name"`            // 设备名称
	DeviceNumber   string     `json:"device_number"`   // 设备编号
	ActivateFlag   string     `json:"activate_flag"`   // 激活状态
	ActivateAt     *time.Time `json:"activate_at"`     // 激活时间
	BatchNumber    string     `json:"batch_number"`    // 批次编号
	CurrentVersion string     `json:"current_version"` // 当前版本
	CreatedAt      *time.Time `json:"created_at"`      // 创建时间
}

type ExportPreRegisterReq struct {
	ProductID    string  `json:"product_id" form:"product_id" validate:"required,max=36"`                        // 产品ID
	BatchNumber  *string `json:"batch_number" form:"batch_number" validate:"omitempty,max=36"`                   // 批次编号
	ActivateFlag *string `json:"activate_flag"  form:"activate_flag" validate:"omitempty,oneof=active inactive"` // 激活状态
}

// 移除子设备
type RemoveSonDeviceReq struct {
	SubDeviceId string `json:"sub_device_id" validate:"required,max=36"` // 设备 ID
}

// 获取设备下拉菜单
type GetDeviceMenuReq struct {
	GroupId    string `json:"group_id" form:"group_id" validate:"omitempty,max=36"`        // 设备组ID
	DeviceName string `json:"device_name" form:"device_name" validate:"omitempty,max=255"` // 设备名称
	BindConfig int    `json:"bind_config" form:"bind_config" validate:"omitempty"`         //绑定设置 0全部 1绑定 2未绑定
}

type GetTenantDeviceListReq struct {
	ID               string `json:"id"`                 // 设备 ID
	Name             string `json:"name"`               // 设备 名称
	DeviceConfigID   string `json:"device_config_id"`   // 设备配置 ID
	DeviceConfigName string `json:"device_config_name"` // 设备配置名称 device.configs.name
}

type CreateSonDeviceRes struct {
	ID    string `json:"id" validate:"required,max=36"`       // 设备 ID
	SonID string `json:"son_id" validate:"required,max=3600"` // 子设备 ID,英文逗号分割
}

type DeviceConnectFormReq struct {
	DeviceID string `query:"device_id" form:"device_id" json:"device_id" validate:"required,max=36"`
}

type DeviceConnectFormRes struct {
	DataKey     string                       `json:"dataKey"`
	Label       string                       `json:"label"`
	Placeholder string                       `json:"placeholder"`
	Type        string                       `json:"type"`
	Validate    DeviceConnectFormValidateRes `json:"validate"`
}

type DeviceConnectFormValidateRes struct {
	Message  string `json:"message,omitempty"`
	Required bool   `json:"required"`
	Type     string `json:"type"`
}

type DeviceConnectReq struct {
	Port            string `json:"接入地址"`
	Info            string `json:"MQTT ClientlD(需要唯一不重复)"`
	DevicePubTopic  string `json:"设备上报遥测主题"`
	DeviceSubTopic  string `json:"设备订阅遥测主题"`
	DevicePubRemark string `json:"设备上报数据示例"`
}

type DeviceIDReq struct {
	DeviceID string `query:"device_id" form:"device_id" json:"device_id" validate:"required,max=36"`
}

type GetVoucherTypeReq struct {
	DeviceType   string `json:"device_type"  form:"device_type"  validate:"required,max=36,oneof=1 2 3"`
	ProtocolType string `json:"protocol_type"  form:"protocol_type"  validate:"required,max=255"`
}

type UpdateDeviceVoucherReq struct {
	DeviceID string `json:"device_id" validate:"required,max=36"`
	Voucher  any    `json:"voucher" validate:"required"`
}

type GetSubListResp struct {
	Name          string `json:"name"`
	Id            string `json:"id"`
	SubDeviceAddr string `json:"subDeviceAddr"`
}

type GetDeviceTemplateChartSelectReq struct {
	GroupID string `json:"group_id" form:"group_id" validate:"required,max=36"`
}

type GetActionByDeviceConfigIDReq struct {
	DeviceConfigID string `json:"device_config_id" form:"device_config_id" validate:"required,max=36"`
}

type GetActionByDeviceIDReq struct {
	DeviceID string `json:"device_id" form:"device_id" validate:"required,max=36"`
}

// 更新设备配置
type ChangeDeviceConfigReq struct {
	DeviceID       string  `json:"device_id" validate:"required,max=36"` // 设备ID
	DeviceConfigID *string `json:"device_config_id" validate:"max=36"`
}

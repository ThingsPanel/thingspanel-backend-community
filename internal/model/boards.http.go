package model

import "time"

type CreateBoardReq struct {
	Name        string  `json:"name" validate:"required,max=255"`         // 看板名称
	Config      *string `json:"config" validate:"omitempty"`              // 看板配置
	HomeFlag    string  `json:"home_flag"  validate:"required,max=2"`     // 首页标志默认N，Y
	MenuFlag    string  `json:"menu_flag"`                                // 菜单标志默认N，Y
	Description *string `json:"description" validate:"omitempty,max=500"` // 描述
	Remark      *string `json:"remark" validate:"omitempty,max=255"`
	TenantID    string  `json:"tenant_id" validate:"omitempty,max=36"` //租户id
	VisType     *string `json:"vis_type" validate:"omitempty,max=50"`
}

type UpdateBoardReq struct {
	Id          string  `json:"id" validate:"omitempty,max=36"`
	Name        string  `json:"name" validate:"omitempty,max=255"`        // 看板名称
	Config      *string `json:"config" validate:"omitempty"`              // 看板配置
	HomeFlag    string  `json:"home_flag"  validate:"omitempty,max=2"`    // 首页标志默认N，Y
	MenuFlag    string  `json:"menu_flag"  validate:"omitempty,max=2"`    // 菜单标志默认N，Y
	Description *string `json:"description" validate:"omitempty,max=500"` // 描述
	Remark      *string `json:"remark" validate:"omitempty,max=255"`
	TenantID    string  `json:"tenant_id" validate:"omitempty,max=36"` //租户id
	VisType     *string `json:"vis_type" validate:"omitempty,max=50"`
}

type GetBoardListByPageReq struct {
	PageReq
	Name     *string `json:"name" form:"name" validate:"omitempty,max=255"`
	HomeFlag *string `json:"home_flag" form:"home_flag"  validate:"omitempty,max=2"`
	VisType  *string `json:"vis_type" validate:"omitempty,max=50"`
}

// DeviceTrendReq 设备趋势请求
type DeviceTrendReq struct {
	TenantID *string `form:"tenant_id" json:"tenant_id" validate:"omitempty,max=36"` // 租户ID
}

// DeviceTrendPoint 趋势数据点
type DeviceTrendPoint struct {
	Timestamp     time.Time `json:"timestamp"`      // 时间点
	DeviceTotal   int64     `json:"device_total"`   // 设备总数
	DeviceOnline  int64     `json:"device_online"`  // 在线设备数
	DeviceOffline int64     `json:"device_offline"` // 离线设备数
}

// DeviceTrendRes 设备趋势响应
type DeviceTrendRes struct {
	Points []DeviceTrendPoint `json:"points"` // 趋势数据点列表
}

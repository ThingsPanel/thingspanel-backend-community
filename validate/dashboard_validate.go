package valid

// PaginateDashBoard  校验
type PaginateDashBoard struct {
	Title string `json:"title" alias:"名称" valid:"MaxSize(255)"`
	Limit int    `json:"limit" alias:"条数" valid:"Max(100)"`
	Page  int    `json:"page" alias:"页面" valid:"Min(1)"`
}

// AddDashBoard 校验
type AddDashBoard struct {
	BusinessId string `json:"business_id" alias:"业务" valid:"Required; MaxSize(36)"`
	Title      string `json:"title" alias:"名称" valid:"Required; MaxSize(255)"`
}

// EditDashBoard 校验
type EditDashBoard struct {
	ID         string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	BusinessID string `json:"business_id" alias:"业务" valid:"Required; MaxSize(36)"`
	Title      string `json:"title" alias:"名称" valid:"Required; MaxSize(255)"`
}

// DeleteDashBoard 校验
type DeleteDashBoard struct {
	ID string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
}

// ListDashBoard 校验
type ListDashBoard struct {
	DashBoardID string `json:"wid" alias:"可视化" valid:"Required; MaxSize(36)"`
}

// Inserttime DashBoard struct
type InserttimeDashBoard struct {
	ID           string `json:"dashboard_id" alias:"业务" valid:"Required; MaxSize(36)"`
	StartTime    string `json:"start_time" alias:"开始时间"` // 时间不做限制
	EndTime      string `json:"end_time" alias:"结束时间"`   // 时间不做限制
	Theme        int64  `json:"theme" alias:"主题" valid:"Min(0)"`
	IntervalTime int64  `json:"interval_time" alias:"间隔" valid:"Min(0)"`
	BgTheme      int64  `json:"bg_theme" alias:"背景主题" valid:"Min(0)"`
}

// Gettime DashBoard struct
type GettimeDashBoard struct {
	ID string `json:"chart_id" alias:"业务" valid:"Required; MaxSize(36)"`
}

// DashBoard DashBoard struct
type DashBoardDashBoard struct {
	DashboardID string `json:"chart_id" alias:"业务" valid:"Required; MaxSize(36)"`
}

// realTime struct
type RealtimeDashBoard struct {
	Type int64 `json:"type" json:"Type" alias:"类型" valid:"Required"`
}

type DeviceDashBoard struct {
	AssetID string `json:"asset_id" alias:"业务" valid:"Required"`
}

type UpdateDashBoard struct {
	WidgetID string `json:"id" alias:"组件" valid:"Required"`
	Config   string `json:"config" alias:"配置信息" valid:"Required"`
}

type ComponentDashBoard struct {
	DeviceID string `json:"device_id" alias:"设备" valid:"Required"`
}

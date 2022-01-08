package valid

// IndexWidget 校验
type IndexWidget struct {
}

// AddWidget 校验
type AddWidget struct {
	DashboardID      string `json:"chart_id" alias:"可视化" valid:"Required;MaxSize(36)"`
	AssetID          string `json:"asset_id" alias:"资产" valid:"Required;MaxSize(36)"`
	DeviceID         string `json:"device_id" alias:"设备" valid:"Required;MaxSize(36)"`
	WidgetIdentifier string `json:"widget_identifier" alias:"图表标识" valid:"Required;MaxSize(255)"`
}

// EditWidget 校验
type EditWidget struct {
	ID               string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	DashboardID      string `json:"dashboard_id" alias:"可视化" valid:"Required;MaxSize(36)"`
	AssetID          string `json:"asset_id" alias:"资产" valid:"Required;MaxSize(36)"`
	DeviceID         string `json:"device_id" alias:"设备" valid:"Required;MaxSize(36)"`
	WidgetIdentifier string `json:"widget_identifier" alias:"图表标识" valid:"Required;MaxSize(255)"`
}

// DeleteWidget 校验
type DeleteWidget struct {
	ID string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
}

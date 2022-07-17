package valid

// WarningConfigIndex 校验
type WarningConfigIndex struct {
	Wid   string `json:"wid" alias:"业务" valid:"Required;MaxSize(255)"`
	Page  int    `json:"page" alias:"页码" valid:"Required;Min(1)"`
	Limit int    `json:"limit" alias:"条数" valid:"Required;Min(10)"`
}

// WarningConfigAdd 校验
type WarningConfigAdd struct {
	Wid          string `json:"wid" alias:"业务ID" valid:"Required;MaxSize(255)"`
	Name         string `json:"name" alias:"预警名称" valid:"Required;MaxSize(255)"`
	Describe     string `json:"describe" alias:"预警描述" valid:"MaxSize(255)"`
	Config       string `json:"config" alias:"配置" valid:"Required"`
	Message      string `json:"message" alias:"消息模板"`
	Bid          string `json:"bid" alias:"设备" valid:"Required;MaxSize(255)"`
	Sensor       string `json:"sensor" alias:"场景" valid:"MaxSize(100)"`
	CustomerID   string `json:"customer_id" alias:"客户" valid:"MaxSize(36)"`
	OtherMessage string `json:"other_message" alias:"客户" valid:"MaxSize(255)"`
}

// WarningConfigEdit 校验
type WarningConfigEdit struct {
	ID           string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	Wid          string `json:"wid" alias:"业务ID" valid:"MaxSize(255)"`
	Name         string `json:"name" alias:"预警名称" valid:"MaxSize(255)"`
	Describe     string `json:"describe" alias:"预警描述" valid:"MaxSize(255)"`
	Config       string `json:"config" alias:"配置"`
	Message      string `json:"message" alias:"消息模板"`
	Bid          string `json:"bid" alias:"设备" valid:"MaxSize(255)"`
	Sensor       string `json:"sensor" alias:"场景" valid:"MaxSize(100)"`
	CustomerID   string `json:"customer_id" alias:"客户" valid:"MaxSize(36)"`
	OtherMessage string `json:"other_message" alias:"客户" valid:"MaxSize(255)"`
}

// WarningConfigDelete 校验
type WarningConfigDelete struct {
	ID string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
}

// WarningConfigGet 校验
type WarningConfigGet struct {
	ID string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
}

// WarningConfigField 校验
type WarningConfigField struct {
	DeviceID string `json:"bid" alias:"bid" valid:"Required;MaxSize(36)"`
}

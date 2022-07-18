package valid

// AutomationIndex 校验
type AutomationIndex struct {
	BusinessId string `json:"business_id" alias:"业务" valid:"Required; MaxSize(36)"`
	Page       int    `json:"page" alias:"页码" valid:"Min(1)"`
	Limit      int    `json:"limit" alias:"条数" valid:"Min(10)"`
}

// AutomationAdd 校验
type AutomationAdd struct {
	BusinessID string `json:"business_id" alias:"业务" valid:"Required; MaxSize(36)"`
	Name       string `json:"name" alias:"名称" valid:"Required; MaxSize(36)"`
	Describe   string `json:"describe" alias:"描述" valid:"Required; MaxSize(36)"`
	Status     int64  `json:"status" alias:"状态" valid:""`
	Config     string `json:"config" alias:"配置" valid:"Required;"`
	Sort       string `json:"sort" alias:"排序" valid:""`
	Type       int64  `json:"type" alias:"类型" valid:""`
	Issued     int64  `json:"issued" alias:"发布" valid:""`
	CustomerID string `json:"customer_id" alias:"客户" valid:"MaxSize(36)"`
}

// AutomationEdit 校验
type AutomationEdit struct {
	ID         string `json:"id" alias:"ID" valid:"MaxSize(36)"`
	BusinessID string `json:"business_id" alias:"业务" valid:"Required; MaxSize(36)"`
	Name       string `json:"name" alias:"名称" valid:"Required; MaxSize(36)"`
	Describe   string `json:"describe" alias:"描述" valid:"Required; MaxSize(36)"`
	Status     int64  `json:"status" alias:"状态" valid:""`
	Config     string `json:"config" alias:"配置" valid:"Required;"`
	Sort       string `json:"sort" alias:"排序" valid:""`
	Type       int64  `json:"type" alias:"类型" valid:""`
	Issued     int64  `json:"issued" alias:"发布" valid:""`
	CustomerID string `json:"customer_id" alias:"客户" valid:"MaxSize(36)"`
}

// AutomationDelete 校验
type AutomationDelete struct {
	ID string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
}

// AutomationGet 校验
type AutomationGet struct {
	ID string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
}

// AutomationProperty 校验
type AutomationProperty struct {
	BusinessID string `json:"business_id" alias:"业务" valid:"Required;MaxSize(36)"`
}

// AutomationShow 校验
type AutomationShow struct {
	Bid string `json:"bid" alias:"设备" valid:"Required;MaxSize(36)"`
}

// AutomationUpdate
type AutomationUpdate struct {
	ID string `json:"id" alias:"设备" valid:"Required;MaxSize(36)"`
}

// AutomationInstruct
type AutomationInstruct struct {
	Bid string `json:"bid" alias:"设备" valid:"Required;MaxSize(36)"`
}

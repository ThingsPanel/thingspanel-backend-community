package valid

// WarningLogListValidate 校验
type WarningLogListValidate struct {
	StartDate string `json:"start_date" alias:"开始时间" valid:"MaxSize(36)"`
	EndDate   string `json:"end_date" alias:"结束时间" valid:"MaxSize(36)"`
	Page      int    `json:"page" alias:"页码" valid:"Min(1)"`
	Limit     int    `json:"limit" alias:"条数" valid:"Min(10)"`
}

// WarningLogListValidate 校验
type WarningLogPageListValidate struct {
	AssetId    string `json:"asset_id" alias:"资产id" valid:"MaxSize(36)"`
	BusinessId string `json:"business_id" alias:"业务id" valid:"MaxSize(36)"`
	DeviceId   string `json:"device_id" alias:"设备id" valid:"MaxSize(36)"`
	StartDate  string `json:"start_date" alias:"开始时间" valid:"MaxSize(36)"`
	EndDate    string `json:"end_date" alias:"结束时间" valid:"MaxSize(36)"`
	Page       int    `json:"page" alias:"页码" valid:"Min(1)"`
	Limit      int    `json:"limit" alias:"条数" valid:"Min(10)"`
}

type DeviceWarningLogListValidate struct {
	Limit    int    `json:"limit" alias:"条数" valid:"Required;Min(10)"`
	DeviceId string `json:"device_id" alias:"设备id" valid:"Required;MaxSize(99)"`
}

package valid

// KVIndex 校验
type KVIndexValidate struct {
	BusinessId string `json:"business_id" alias:"业务" valid:"MaxSize(36)"`
	AssetId    string `json:"asset_id" alias:"资产id" valid:"MaxSize(36)"`
	Token      string `json:"token" alias:"token" valid:"MaxSize(36)"`
	Type       int64  `json:"type" alias:"类型" valid:"Required;"`
	Limit      int    `json:"limit" alias:"条数" valid:"Max(100)"`
	Page       int    `json:"page" alias:"页面" valid:"Min(1)"`
	StartTime  string `json:"start_time" alias:"开始时间"`
	EndTime    string `json:"end_time" alias:"结束时间"`
	Key        string `json:"key" alias:"数据标签"`
}
type KVExcelValidate struct {
	BusinessId string `json:"business_id" alias:"业务" valid:"MaxSize(36)"`
	AssetId    string `json:"asset_id" alias:"资产id" valid:"MaxSize(36)"`
	Token      string `json:"token" alias:"token" valid:"MaxSize(36)"`
	Type       int64  `json:"type" alias:"类型" valid:"Required;"`
	Limit      int    `json:"limit" alias:"条数" valid:"Max(1000000)"`
	StartTime  string `json:"start_time" alias:"开始时间"`
	EndTime    string `json:"end_time" alias:"结束时间"`
	Key        string `json:"key" alias:"数据标签"`
}

// KVExport 校验
type KVExportValidate struct {
	EntityID  string `json:"entity_id" alias:"设备" valid:"MaxSize(36)"`
	Type      int64  `json:"type" alias:"类型" valid:"Required;"`
	StartTime string `json:"start_time" alias:"开始时间"`
	EndTime   string `json:"end_time" alias:"结束时间"`
}

type CurrentKV struct {
	EntityID string `json:"entity_id" alias:"设备" valid:"MaxSize(36)"`
}

type DeviceHistoryDataValidate struct {
	DeviceId string `json:"device_id" alias:"设备" valid:"MaxSize(36)"`
	Current  int    `json:"current" alias:"条数" valid:"Max(1000000)"`
	Size     int    `json:"size" alias:"条数" valid:"Max(1000000)"`
}

type CurrentKVByBusiness struct {
	BusinessiD string `json:"business_id" alias:"业务" valid:"MaxSize(36)"`
}

type CurrentKVByAsset struct {
	AssetId string `json:"asset_id" alias:"设备分组id" valid:"MaxSize(36)"`
}

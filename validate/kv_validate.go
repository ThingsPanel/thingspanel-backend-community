package valid

import "time"

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
	DeviceName string `json:"device_name" alias:"设备名称"`
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
	DeviceName string `json:"device_name" alias:"设备名称"`
}

// KVExport 校验
type KVExportValidate struct {
	EntityID  string `json:"entity_id" alias:"设备" valid:"MaxSize(36)"`
	Type      int64  `json:"type" alias:"类型" valid:"Required;"`
	StartTime string `json:"start_time" alias:"开始时间"`
	EndTime   string `json:"end_time" alias:"结束时间"`
}

type CurrentKV struct {
	EntityID  string   `json:"entity_id" alias:"设备" valid:"MaxSize(36)"`
	Attribute []string `json:"attribute,omitempty" alias:"属性" valid:"MaxSize(36)"`
}

type CurrentKVByDeviceId struct {
	DeviceId string `json:"device_id" alias:"设备" valid:"MaxSize(36)"`
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

type HistoryDataValidate struct {
	DeviceId  string   `json:"device_id" alias:"设备" valid:"Required;MaxSize(36)"`
	Attribute []string `json:"attribute" alias:"属性" valid:"Required;MaxSize(36)"`
	StartTs   int64    `json:"start_ts" alias:"开始时间" valid:"Required"`
	EndTs     int64    `json:"end_ts" alias:"结束时间" valid:"Required"`
	Rate      string   `json:"rate" alias:"间隔" valid:"MaxSize(36)"`
}

// GetHistoryDataByKey 校验
type HistoryDataByKeyValidate struct {
	DeviceId  string `json:"device_id" alias:"设备" valid:"Required;MaxSize(36)"`
	Key       string `json:"key" alias:"属性" valid:"Required;MaxSize(36)"`
	StartTime int64  `json:"start_time" alias:"开始时间" valid:"Required"`
	EndTime   int64  `json:"end_time" alias:"结束时间" valid:"Required"`
	Limit     int64  `json:"limit" alias:"条数" valid:"Max(1000000)"`
}

// GetStatisticDataByKey 校验
type StatisticDataValidate struct {
	DeviceId          string `json:"device_id" alias:"设备" valid:"Required;MaxSize(36)"`
	Key               string `json:"key" alias:"属性" valid:"Required;MaxSize(36)"`
	StartTime         int64  `json:"start_time" alias:"开始时间" valid:"Required"`
	EndTime           int64  `json:"end_time" alias:"结束时间" valid:"Required"`
	TimeRange         string `json:"time_range" alias:"时间范围"`
	AggregateWindow   string `json:"aggregate_window" alias:"聚合间隔"`
	AggregateFunction string `json:"aggregate_function" alias:"聚合方法"`
}

// 支持的间隔之间
var StatisticAggregateWindow = map[string]int64{
	"30s": int64(time.Second * 30 / time.Microsecond),
	"1m":  int64(time.Minute / time.Microsecond),
	"2m":  int64(time.Minute * 2 / time.Microsecond),
	"5m":  int64(time.Minute * 5 / time.Microsecond),
	"10m": int64(time.Minute * 10 / time.Microsecond),
	"30m": int64(time.Minute * 30 / time.Microsecond),
	"1h":  int64(time.Hour / time.Microsecond),
	"3h":  int64(time.Hour * 3 / time.Microsecond),
	"6h":  int64(time.Hour * 6 / time.Microsecond),
	"1d":  int64(time.Hour * 24 / time.Microsecond),
	"7d":  int64(time.Hour * 24 * 7 / time.Microsecond),
	"1mo": int64(time.Hour * 24 * 30 / time.Microsecond),
}

// 支持的统计方法
var StatisticAggregateFunction = map[string]string{
	"max":  "MAX",
	"avg":  "AVG",
	"test": "",
}

// 支持的时间段选择(暂时忽略
var StatisticTimeRangeMap = map[string]int{
	"custom":   0, // 自定义时间段
	"last_5m":  0,
	"last_15m": 0,
	"last_30m": 0,
	"last_1h":  0,
	"last_3h":  0,
	"last_1y":  0,
}

// 删除历史数据校验
type DeleteHistoryDataValidate struct {
	DeviceId  string `json:"device_id" alias:"设备" valid:"Required;MaxSize(36)"`
	Attribute string `json:"attribute" alias:"属性" valid:"Required;MaxSize(36)"`
}

package valid

type TpOtaDeviceVaildate struct {
	Id               string `json:"id" gorm:"primaryKey"`
	DeviceId         string `json:"device_id,omitempty" alias:"设备ID" valid:"Required;MaxSize(36)"`
	CurrentVersion   string `json:"current_version,omitempty" alias:"当前版本号" valid:"MaxSize(50)"`
	TargetVersion    string `json:"target_version,omitempty" alias:"目标版本号" valid:"MaxSize(50)"`
	UpgradeProgress  string `json:"upgrade_progress,omitempty" alias:"升级进度" valid:"MaxSize(10)"`
	StatusUpdateTime string `json:"status_update_time,omitempty" alias:"状态更新时间" valid:"MaxSize(36)"`
	UpgradeStatus    string `json:"upgrade_status,omitempty" alias:"状态"`
	StatusDetail     string `json:"status_detail,omitempty" alias:"状态详情" valid:"MaxSize(255)"`
	OtaTaskId        string `json:"ota_task_id,omitempty" alias:"ota任务id" valid:"MaxSize(36)"`
	Name             string `json:"name,omitempty" alias:"设备名"`
}
type AddTpOtaDeviceValidate struct {
	DeviceId         string `json:"device_id,omitempty" alias:"设备ID" valid:"Required;MaxSize(36)"`
	CurrentVersion   string `json:"current_version,omitempty" alias:"当前版本号" valid:"MaxSize(50)"`
	TargetVersion    string `json:"target_version,omitempty" alias:"目标版本号" valid:"MaxSize(50)"`
	UpgradeProgress  string `json:"upgrade_progress,omitempty" alias:"升级进度" valid:"MaxSize(10)"`
	StatusUpdateTime string `json:"status_update_time,omitempty" alias:"状态更新时间" valid:"MaxSize(36)"`
	UpgradeStatus    string `json:"upgrade_status,omitempty" alias:"状态 0-待推送 1-已推送 2-升级中 3-升级成功 4-升级失败 5-已取消"`
	StatusDetail     string `json:"status_detail,omitempty" alias:"状态详情" valid:"MaxSize(255)"`
	OtaTaskId        string `json:"ota_task_id,omitempty" alias:"ota任务id" valid:"MaxSize(36)"`
}
type TpOtaDevicePaginationValidate struct {
	CurrentPage   int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage       int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	DeviceId      string `json:"device_id"  valid:"MaxSize(36)"`
	Name          string `json:"name"  valid:"MaxSize(36)"`
	UpgradeStatus string `json:"upgrade_status,omitempty" alias:"状态 0-待推送 1-已推送 2-升级中 3-升级成功 4-升级失败 5-已取消"`
	OtaTaskId     string `json:"ota_task_id,omitempty" alias:"ota任务id" valid:"MaxSize(36)"`
}

type RspTpOtaDevicePaginationValidate struct {
	CurrentPage int                    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        map[string]interface{} `json:"data" alias:"返回数据"`
	Total       int64                  `json:"total" alias:"总数" valid:"Max(10000)"`
}
type TpOtaDeviceIdValidate struct {
	Id            string `json:"id,omitempty"   gorm:"primaryKey"  alias:"Id" valid:"MaxSize(36)"`
	OtaTaskId     string `json:"ota_task_id,omitempty" alias:"ota任务id" valid:"Required;MaxSize(36)"`
	UpgradeStatus string `json:"upgrade_status,omitempty" alias:"状态"`
}

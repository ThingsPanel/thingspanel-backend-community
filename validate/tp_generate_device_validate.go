package valid

import "ThingsPanel-Go/models"

type TpGenerateDeviceValidate struct {
	Id           string `json:"id" valid:"Required;MaxSize(99)"`
	BatchId      string `json:"batch_id,omitempty" alias:"批次id" valid:"Required;MaxSize(36)"`
	Token        string `json:"token,omitempty" valid:"MaxSize(36)"`
	Password     string `json:"password,omitempty" valid:"MaxSize(36)"`
	ActivateFlag string `json:"activate_flag,omitempty" alias:"激活状态" valid:"MaxSize(36)"`
	ActivateDate string `json:"activate_date,omitempty" alias:"激活日期" valid:"MaxSize(36)"`
	DeviceId     string `json:"device_id,omitempty" alias:"设备id" valid:"MaxSize(36)"`
	DeviceCode   string `json:"device_code,omitempty" alias:"设备编码" valid:"MaxSize(36)"`
	CreatedTime  int64  `json:"created_time,omitempty"`
	Remark       string `json:"remark,omitempty" valid:"Required;MaxSize(255)"`
}

type TpGenerateDevicePaginationValidate struct {
	CurrentPage  int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage      int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	BatchId      string `json:"batch_id,omitempty" alias:"批次id" valid:"Required;MaxSize(36)"`
	DeviceCode   string `json:"device_code,omitempty" alias:"设备编码" valid:"MaxSize(36)"`
	ActivateFlag string `json:"activate_flag,omitempty" alias:"激活状态" valid:"MaxSize(36)"`
}

type RspTpGenerateDevicePaginationValidate struct {
	CurrentPage int                       `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                       `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpGenerateDevice `json:"data" alias:"返回数据"`
	Total       int64                     `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpGenerateDeviceIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

type ActivateDeviceValidate struct {
	ActivationCode string `json:"activation_code" alias:"激活码" valid:"Required;MaxSize(36)"`
	Name           string `json:"name"  alias:"设备名称" valid:"Required;MaxSize(36)"`
	AccessId       string `json:"access_id"  alias:"设备分组" valid:"Required;MaxSize(36)"`
}

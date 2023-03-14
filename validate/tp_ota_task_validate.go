package valid

import "ThingsPanel-Go/models"

type TpOtaTaskVaildate struct {
	Id              string `json:"id" gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	TaskName        string `json:"task_name,omitempty" alias:"任务名称" valid:"Required;MaxSize(36)"`
	UpgradeTimeType string `json:"upgrade_time_type,omitempty" alias:"升级时间 0-立即升级 1-定时升级" valid:"Required;MaxSize(36)"`
	StartTime       string `json:"start_time,omitempty" alias:"升级开始时间" valid:"MaxSize(36)"`
	EndTime         string `json:"end_time,omitempty" alias:"升级结束时间" valid:"MaxSize(36)"`
	DeviceCount     int64  `json:"device_count,omitempty" alias:"设备数量"`
	TaskStatus      string `json:"task_status,omitempty" alias:"状态 0-待升级 1-升级中 2-已完成"`
	Description     string `json:"description,omitempty" alias:"说明"`
	CreatedAt       int64  `json:"created_at,omitempty" alias:"创建日期"`
	OtaId           string `json:"ota_id,omitempty" alias:"固件包id"`
}
type AddTpOtaTaskValidate struct {
	TaskName        string   `json:"task_name,omitempty" alias:"任务名称" valid:"Required;MaxSize(36)"`
	UpgradeTimeType string   `json:"upgrade_time_type,omitempty" alias:"升级时间 0-立即升级 1-定时升级" valid:"Required;MaxSize(36)"`
	StartTime       string   `json:"start_time,omitempty" alias:"升级开始时间" valid:"MaxSize(36)"`
	EndTime         string   `json:"end_time,omitempty" alias:"升级结束时间" valid:"MaxSize(36)"`
	DeviceCount     int64    `json:"device_count,omitempty" alias:"设备数量"`
	TaskStatus      string   `json:"task_status,omitempty" alias:"状态 0-待升级 1-升级中 2-已完成"`
	Description     string   `json:"description,omitempty" alias:"说明"`
	CreatedAt       int64    `json:"created_at,omitempty" alias:"创建日期"`
	OtaId           string   `json:"ota_id,omitempty" alias:"固件包id"`
	DeviceIds       []string `json:"device_ids,omitempty" alias:"设备"`
}
type TpOtaTaskPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id" alias:"升级包id" gorm:"primaryKey" valid:"MaxSize(36)"`
	OtaId       string `json:"ota_id,omitempty" alias:"升级包id" valid:"Required"`
}

type RspTpOtaTaskPaginationValidate struct {
	CurrentPage int                `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpOtaTask `json:"data" alias:"返回数据"`
	Total       int64              `json:"total" alias:"总数" valid:"Max(10000)"`
}
type TpOtaTaskIdValidate struct {
	Id string `json:"id,omitempty"   gorm:"primaryKey"  alias:"Id" valid:"MaxSize(36)"`
}

package valid

import "ThingsPanel-Go/models"

type TpAutomationConditionValidate struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	AutomationId        string `json:"automation_id,omitempty" valid:"MaxSize(36)"`
	GroupNumber         int64  `json:"group_number,omitempty"`                       // 小组编号
	ConditionType       string `json:"condition_type,omitempty"  valid:"MaxSize(2)"` // 条件类型1-设备条件 2-时间条件
	DeviceId            string `json:"device_id,omitempty" valid:"MaxSize(36)"`
	TimeConditionType   string `json:"time_condition_type,omitempty" valid:"MaxSize(2)"`   // 时间条件类型0-时间范围 1-单次 2-重复
	DeviceConditionType string `json:"device_condition_type,omitempty" valid:"MaxSize(2)"` // 设备条件类型
	V1                  string `json:"v1,omitempty" valid:"MaxSize(99)"`
	V2                  string `json:"v2,omitempty" valid:"MaxSize(99)"`
	V3                  string `json:"v3,omitempty" valid:"MaxSize(99)"`
	V4                  string `json:"v4,omitempty" valid:"MaxSize(99)"`
	V5                  string `json:"v5,omitempty" valid:"MaxSize(99)"`
	Remark              string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type AddTpAutomationConditionValidate struct {
	Id                  string `json:"id"  gorm:"primaryKey" valid:"MaxSize(36)"`
	AutomationId        string `json:"automation_id,omitempty" valid:"MaxSize(36)"`
	GroupNumber         int64  `json:"group_number,omitempty"`                       // 小组编号
	ConditionType       string `json:"condition_type,omitempty"  valid:"MaxSize(2)"` // 条件类型1-设备条件 2-时间条件
	DeviceId            string `json:"device_id,omitempty" valid:"MaxSize(36)"`
	TimeConditionType   string `json:"time_condition_type,omitempty" valid:"MaxSize(2)"`   // 时间条件类型0-时间范围 1-单次 2-重复
	DeviceConditionType string `json:"device_condition_type,omitempty" valid:"MaxSize(2)"` // 设备条件类型
	V1                  string `json:"v1,omitempty" valid:"MaxSize(99)"`
	V2                  string `json:"v2,omitempty" valid:"MaxSize(99)"`
	V3                  string `json:"v3,omitempty" valid:"MaxSize(99)"`
	V4                  string `json:"v4,omitempty" valid:"MaxSize(99)"`
	V5                  string `json:"v5,omitempty" valid:"MaxSize(99)"`
	Remark              string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type TpAutomationConditionPaginationValidate struct {
	CurrentPage   int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage       int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ConditionType string `json:"condition_type,omitempty" alias:"条件类型" valid:"MaxSize(2)"`
	Id            string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpAutomationConditionPaginationValidate struct {
	CurrentPage int                            `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                            `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpAutomationCondition `json:"data" alias:"返回数据"`
	Total       int64                          `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpAutomationConditionIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

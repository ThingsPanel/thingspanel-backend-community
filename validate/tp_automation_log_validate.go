package valid

import "ThingsPanel-Go/models"

type TpAutomationLog struct {
	Id                 string `json:"id,omitempty"  valid:"Required;MaxSize(36)"`
	AutomationId       string `json:"automation_id,omitempty"  valid:"MaxSize(36)"`
	TriggerTime        string `json:"trigger_time,omitempty" valid:"MaxSize(50)"`
	ProcessDescription string `json:"process_description,omitempty" valid:"MaxSize(255)"`
	ProcessResult      string `json:"process_result,omitempty" valid:"MaxSize(1)"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type TpAutomationLogValidate struct {
	Id                 string `json:"id,omitempty"  valid:"Required;MaxSize(36)"`
	AutomationId       string `json:"automation_id,omitempty"  valid:"MaxSize(36)"`
	TriggerTime        string `json:"trigger_time,omitempty" valid:"MaxSize(50)"`
	ProcessDescription string `json:"process_description,omitempty" valid:"MaxSize(255)"`
	ProcessResult      string `json:"process_result,omitempty" valid:"MaxSize(1)"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type AddTpAutomationLogValidate struct {
	Id                 string `json:"id,omitempty"  valid:"MaxSize(36)"`
	AutomationId       string `json:"automation_id,omitempty"  valid:"MaxSize(36)"`
	TriggerTime        string `json:"trigger_time,omitempty" valid:"MaxSize(50)"`
	ProcessDescription string `json:"process_description,omitempty" valid:"MaxSize(255)"`
	ProcessResult      string `json:"process_result,omitempty" valid:"MaxSize(1)"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type TpAutomationLogPaginationValidate struct {
	CurrentPage   int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage       int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ProcessResult string `json:"process_result,omitempty" alias:"执行状态" valid:"MaxSize(1)"`
	Id            string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
	AutomationId  string `json:"automation_id,omitempty" alias:"AutomationId" valid:"MaxSize(36)"`
}

type RspTpAutomationLogPaginationValidate struct {
	CurrentPage int                      `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                      `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpAutomationLog `json:"data" alias:"返回数据"`
	Total       int64                    `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpAutomationLogIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

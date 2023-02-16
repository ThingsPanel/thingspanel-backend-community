package valid

type TpAutomationLogDetailValidate struct {
	Id                 string `json:"id,omitempty" valid:"Required;MaxSize(36)"`
	AutomationLogId    string `json:"automation_log_id,omitempty"  valid:"MaxSize(36)"`
	ActionType         string `json:"action_type,omitempty"  valid:"MaxSize(2)"` // 动作类型 1-设备输出 2-触发告警 3-激活场景
	ProcessDescription string `json:"process_description,omitempty" valid:"MaxSize(255)"`
	ProcessResult      string `json:"process_result,omitempty" valid:"MaxSize(1)"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
	TargetId           string `json:"target_id,omitempty" valid:"MaxSize(36)"` // 设备id告警id场景id
}

type AddTpAutomationLogDetailValidate struct {
	Id                 string `json:"id,omitempty"  valid:"MaxSize(36)"`
	AutomationLogId    string `json:"automation_log_id,omitempty"  valid:"MaxSize(36)"`
	ActionType         string `json:"action_type,omitempty"  valid:"MaxSize(2)"` // 动作类型 1-设备输出 2-触发告警 3-激活场景
	ProcessDescription string `json:"process_description,omitempty" valid:"MaxSize(255)"`
	ProcessResult      string `json:"process_result,omitempty" valid:"MaxSize(1)"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty" valid:"MaxSize(255)"`
	TargetId           string `json:"target_id,omitempty" valid:"MaxSize(36)"` // 设备id告警id场景id
}

type TpAutomationLogDetailPaginationValidate struct {
	CurrentPage     int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage         int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ProcessResult   string `json:"process_result,omitempty" alias:"执行状态" valid:"MaxSize(99)"`
	Id              string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
	AutomationLogId string `json:"automation_log_id,omitempty" alias:"自动化日志id" valid:"MaxSize(36)"`
}

type RspTpAutomationLogDetailPaginationValidate struct {
	CurrentPage int                      `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                      `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []map[string]interface{} `json:"data" alias:"返回数据"`
	Total       int64                    `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpAutomationLogDetailIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

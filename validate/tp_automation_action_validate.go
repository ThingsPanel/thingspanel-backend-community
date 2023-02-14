package valid

import "ThingsPanel-Go/models"

type TpAutomationActionValidate struct {
	Id                 string                    `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
	AutomationId       string                    `json:"automation_id,omitempty" valid:"MaxSize(36)"`
	ActionType         string                    `json:"action_type,omitempty" valid:"MaxSize(2)"` //动作类型| 1-设备输出 2-触发告警 3-激活场景
	DeviceId           string                    `json:"device_id,omitempty" valid:"MaxSize(36)"`
	WarningStrategyId  string                    `json:"warning_strategy_id,omitempty" valid:"MaxSize(36)"`
	WarningStrategy    TpWarningStrategyValidate `json:"warning_strategy,omitempty"`
	ScenarioStrategyId string                    `json:"scenario_strategy_id,omitempty" valid:"MaxSize(36)"`
	AdditionalInfo     string                    `json:"additional_info,omitempty" valid:"MaxSize(10000)"` //附加信息|device_model设备模型 1-设定属性 2-调动服务 instruct指令
	Remark             string                    `json:"remark,omitempty"  valid:"MaxSize(255)"`
}

type AddTpAutomationActionValidate struct {
	Id                 string                       `json:"id"  valid:"MaxSize(36)"`
	AutomationId       string                       `json:"automation_id,omitempty" valid:"MaxSize(36)"`
	ActionType         string                       `json:"action_type,omitempty" valid:"MaxSize(2)"` //动作类型| 1-设备输出 2-触发告警 3-激活场景
	DeviceId           string                       `json:"device_id,omitempty" valid:"MaxSize(36)"`
	WarningStrategyId  string                       `json:"warning_strategy_id,omitempty" valid:"MaxSize(36)"`
	WarningStrategy    AddTpWarningStrategyValidate `json:"warning_strategy,omitempty"`
	ScenarioStrategyId string                       `json:"scenario_strategy_id,omitempty" valid:"MaxSize(36)"`
	AdditionalInfo     string                       `json:"additional_info,omitempty" valid:"MaxSize(10000)"` //附加信息|device_model设备模型 1-设定属性 2-调动服务 instruct指令
	Remark             string                       `json:"remark,omitempty"  valid:"MaxSize(255)"`
}

type TpAutomationActionPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ActionType  string `json:"action_type,omitempty" alias:"动作类型" valid:"MaxSize(1)"`
	Id          string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpAutomationActionPaginationValidate struct {
	CurrentPage int                         `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                         `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpAutomationAction `json:"data" alias:"返回数据"`
	Total       int64                       `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpAutomationActionIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

package models

type TpAutomationAction struct {
	Id                 string `json:"id"  gorm:"primaryKey"`
	AutomationId       string `json:"automation_id,omitempty"`
	ActionType         string `json:"action_type,omitempty"` //动作类型| 1-设备输出 2-触发告警 3-激活场景
	DeviceId           string `json:"device_id,omitempty"`
	WarningStrategyId  string `json:"warning_strategy_id,omitempty"`
	ScenarioStrategyId string `json:"scenario_strategy_id,omitempty"`
	AdditionalInfo     string `json:"additional_info,omitempty"` //附加信息|device_model设备模型 1-设定属性 2-调动服务 instruct指令
	Remark             string `json:"remark,omitempty"`
}

func (t *TpAutomationAction) TableName() string {
	return "tp_automation_action"
}

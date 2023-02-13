package models

type TpScenarioAction struct {
	Id                 string `json:"id" gorm:"primaryKey"`
	ScenarioStrategyId string `json:"scenario_strategy_id,omitempty"`
	ActionType         string `json:"action_type,omitempty"`
	DeviceId           string `json:"device_id,omitempty"`
	DeviceModel        string `json:"device_model,omitempty"` // 模型类型1-设定属性 2-调动服务
	Instruct           string `json:"instruct,omitempty"`     // 指令
	Remark             string `json:"remark,omitempty"`
}

func (t *TpScenarioAction) TableName() string {
	return "tp_scenario_action"
}

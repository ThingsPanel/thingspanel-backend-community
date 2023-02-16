package models

type TpScenarioLog struct {
	Id                 string `json:"id" gorm:"primaryKey"`
	ScenarioStrategyId string `json:"scenario_strategy_id,omitempty"`
	ProcessDescription string `json:"process_description,omitempty"` // 过程描述
	TriggerTime        string `json:"trigger_time,omitempty"`
	ProcessResult      string `json:"process_result,omitempty"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty"`
}

func (t *TpScenarioLog) TableName() string {
	return "tp_scenario_log"
}

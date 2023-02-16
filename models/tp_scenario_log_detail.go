package models

type TpScenarioLogDetail struct {
	Id                 string `json:"id" gorm:"primaryKey"`
	ScenarioLogId      string `json:"scenario_log_id,omitempty"`
	ActionType         string `json:"action_type,omitempty"`
	ProcessDescription string `json:"process_description,omitempty"`
	ProcessResult      string `json:"process_result,omitempty"`
	Remark             string `json:"remark,omitempty"`
	TargetId           string `json:"target_id,omitempty"` // 设备id告警id场景id
}

func (t *TpScenarioLogDetail) TableName() string {
	return "tp_scenario_log_detail"
}

package models

type TpAutomationLog struct {
	Id                 string `json:"id" gorm:"primaryKey"`
	AutomationId       string `json:"automation_id,omitempty"`
	TriggerTime        string `json:"trigger_time,omitempty"`
	ProcessDescription string `json:"process_description,omitempty"`
	ProcessResult      string `json:"process_result,omitempty"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty"`
}

func (t *TpAutomationLog) TableName() string {
	return "tp_automation_log"
}

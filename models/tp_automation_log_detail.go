package models

type TpAutomationLogDetail struct {
	Id                 string `json:"id" gorm:"primaryKey"`
	AutomationLogId    string `json:"automation_log_id,omitempty"`
	ActionType         string `json:"action_type,omitempty"` // 动作类型 1-设备输出 2-触发告警 3-激活场景
	ProcessDescription string `json:"process_description,omitempty"`
	ProcessResult      string `json:"process_result,omitempty"` // 执行状态 1-成功 2-失败
	Remark             string `json:"remark,omitempty"`
	TargetId           string `json:"target_id,omitempty"` // 设备id告警id场景id
}

func (t *TpAutomationLogDetail) TableName() string {
	return "tp_automation_log_detail"
}

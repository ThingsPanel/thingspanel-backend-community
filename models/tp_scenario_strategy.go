package models

type TpScenarioStrategy struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	TenantId            string `json:"tenant_id,omitempty"`
	ScenarioName        string `json:"scenario_name,omitempty"`        // 场景名称
	ScenarioDescription string `json:"scenario_description,omitempty"` // 场景描述
	CreatedAt           int64  `json:"created_at,omitempty"`
	CreatedBy           string `json:"created_by,omitempty"`
	UpdateTime          int64  `json:"update_time,omitempty"`
	Remark              string `json:"remark,omitempty"`
}

func (t *TpScenarioStrategy) TableName() string {
	return "tp_scenario_strategy"
}

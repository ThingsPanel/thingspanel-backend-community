package models

type TpAutomation struct {
	Id                  string `json:"id"  gorm:"primaryKey"`
	TenantId            string `json:"tenant_id,omitempty"`
	AutomationName      string `json:"automation_name,omitempty"`
	AutomationDescribed string `json:"automation_described,omitempty"`
	UpdateTime          int64  `json:"update_time,omitempty"`
	CreatedAt           int64  `json:"created_at,omitempty"`
	CreatedBy           string `json:"created_by,omitempty"`
	Priority            int64  `json:"priority,omitempty"` //优先级|1-100越小越高
	Enabled             string `json:"enabled,omitempty"`  //启用状态 |0-未开启 1-已开启
	Remark              string `json:"remark,omitempty"`
}

func (t *TpAutomation) TableName() string {
	return "tp_automation"
}

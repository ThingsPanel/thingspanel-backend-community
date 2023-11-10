package models

type TpDataTranspon struct {
	Id                string `json:"id,omitempty"`
	Name              string `json:"name"`
	Desc              string `json:"desc,omitempty"`
	Status            int    `json:"status"`
	TenantId          string `json:"tenant_id,omitempty"`
	Script            string `json:"script,omitempty"`
	CreateTime        int64  `json:"create_time,omitempty"`
	WarningStrategyId string `json:"warning_strategy_id,omitempty"`
	WarningSwitch     int    `json:"warning_switch,omitempty"`
}

func (TpDataTranspon) TableName() string {
	return "tp_data_transpond"
}

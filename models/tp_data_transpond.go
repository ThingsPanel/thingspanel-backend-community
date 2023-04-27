package models

type TpDataTranspon struct {
	Id         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Desc       string `json:"desc,omitempty"`
	Status     int    `json:"status,omitempty"`
	TenantId   string `json:"tenant_id,omitempty"`
	Script     string `json:"script,omitempty"`
	CreateTime int    `json:"create_time,omitempty"`
}

func (TpDataTranspon) TableName() string {
	return "tp_data_transpond"
}

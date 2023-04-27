package models

type TpDataTransponTarget struct {
	Id              string `json:"id"`
	DataTranspondId string `json:"data_transpond_id"`
	DataType        int    `json:"data_type"`
	Target          string `json:"target"`
}

func (TpDataTransponTarget) TableName() string {
	return "tp_data_transpond_target"
}

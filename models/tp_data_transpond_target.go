package models

type TpDataTransponTarget struct {
	Id              string `json:"id"`
	DataTranspondId string `json:"data_transpond_id"`
	DataType        int    `json:"data_type"`
	Target          string `json:"target"`
}

// 发送类型为URL
const DataTypeURL = 1

// 发送类型为MQTT
const DataTypeMQTT = 2

func (TpDataTransponTarget) TableName() string {
	return "tp_data_transpond_target"
}

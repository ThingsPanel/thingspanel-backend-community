package models

type TpDataTransponDetail struct {
	Id              string `json:"id"`
	DataTranspondId string `json:"data_transpond_id"`
	DeviceId        string `json:"device_id"`
	MessageType     int    `json:"message_type"`
}

func (TpDataTransponDetail) TableName() string {
	return "tp_data_transpond_detail"
}

package models

type DeviceCommandHistory struct {
	ID              string `json:"id" gorm:"primaryKey,size:36"`
	DeviceId        string `json:"device_id"`
	CommandIdentify string `json:"command_identify,omitempty"`
	CommandName     string `json:"command_name,omitempty"`
	Desc            string `json:"desc,omitempty"`
	Data            string `json:"data,omitempty"`
	SendTime        int64  `json:"send_time"`
	SendStatus      int64  `json:"send_status"`
}

func (DeviceCommandHistory) TableName() string {
	return "device_command_history"
}

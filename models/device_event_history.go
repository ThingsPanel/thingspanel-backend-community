package models

type DeviceEvnetHistory struct {
	ID            string `json:"id" gorm:"primaryKey,size:36"`
	DeviceId      string `json:"device_id"`
	EventIdentify string `json:"event_identify,omitempty"`
	EvnetName     string `json:"event_name,omitempty"`
	Desc          string `json:"desc,omitempty"`
	Data          string `json:"data,omitempty"`
	ReportTime    int64  `json:"report_time"`
}

func (DeviceEvnetHistory) TableName() string {
	return "device_event_history"
}

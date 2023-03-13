package models

type TpOtaDevice struct {
	Id               string `json:"id" gorm:"primaryKey"`
	DeviceId         string `json:"device_id,omitempty"`
	CurrentVersion   string `json:"current_version,omitempty"`
	TargetVersion    string `json:"target_version,omitempty"`
	UpgradeProgress  string `json:"upgrade_progress,omitempty"`
	StatusUpdateTime string `json:"status_update_time,omitempty"`
	UpgradeStatus    string `json:"upgrade_status,omitempty"`
	StatusDetail     string `json:"status_detail,omitempty"`
	OtaTaskId        string `json:"ota_task_id,omitempty"`
}

func (TpOtaDevice) TableName() string {
	return "tp_ota_device"
}

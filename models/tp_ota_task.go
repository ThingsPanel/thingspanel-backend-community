package models

type TpOtaTask struct {
	Id              string `json:"id" gorm:"primaryKey"`
	TaskName        string `json:"task_name,omitempty"`
	UpgradeTimeType string `json:"upgrade_time_type,omitempty"` //升级时间 0-立即升级 1-定时升级
	StartTime       string `json:"start_time,omitempty"`
	EndTime         string `json:"end_time,omitempty"`
	DeviceCount     int64  `json:"device_count,omitempty"`
	TaskStatus      string `json:"task_status,omitempty"`
	Description     string `json:"description,omitempty"`
	CreatedAt       int64  `json:"created_at,omitempty"`
}

func (TpOtaTask) TableName() string {
	return "tp_ota_task"
}

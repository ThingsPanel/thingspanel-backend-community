package models

type TpDataCleanup struct {
	Id                  string `json:"id"`
	CleanupType         int    `json:"cleanup_type,omitempty"`
	RetentionDays       int    `json:"retention_days"`
	LastCleanupTime     int64  `json:"last_cleanup_time,omitempty"`
	LastCleanupDataTime int64  `json:"last_cleanup_data_time,omitempty"`
	Remark              string `json:"remark"`
}

func (TpDataCleanup) TableName() string {
	return "tp_data_cleanup_config"
}

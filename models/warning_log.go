package models

type WarningLog struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	Type      string `json:"type" gorm:"size:36"`
	Describe  string `json:"describe"`
	DataID    string `json:"data_id" gorm:"size:36"`
	CreatedAt int64  `json:"created_at"`
}

func (WarningLog) TableName() string {
	return "warning_log"
}

type WarningLogView struct {
	ID         string `json:"id" gorm:"primaryKey,size:36"`
	Type       string `json:"type" gorm:"size:36"`
	Describe   string `json:"describe"`
	DataID     string `json:"data_id" gorm:"size:36"`
	CreatedAt  int64  `json:"created_at"`
	DeviceName string `json:"device_name"`
}

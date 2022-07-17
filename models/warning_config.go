package models

type WarningConfig struct {
	ID           string `json:"id" gorm:"primaryKey,size:36"`
	Wid          string `json:"wid"`                      // 业务ID
	Name         string `json:"name"`                     // 预警名称
	Describe     string `json:"describe"`                 // 预警描述
	Config       string `json:"config" gorm:"type:text"`  // 配置
	Message      string `json:"message" gorm:"type:text"` // 消息模板
	Bid          string `json:"bid"`
	Sensor       string `json:"sensor" gorm:"size:100"`
	CustomerID   string `json:"customer_id" gorm:"size:36"`
	OtherMessage string `json:"other_message" gorm:"size:255"`
}

func (WarningConfig) TableName() string {
	return "warning_config"
}

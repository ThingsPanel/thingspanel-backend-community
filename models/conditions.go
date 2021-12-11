package models

type Condition struct {
	ID         string `json:"id" gorm:"primaryKey,size:36"`
	BusinessID string `json:"business_id" gorm:"size:36"` // 业务ID
	Name       string `json:"name" gorm:"size:255"`       // 策略名称
	Describe   string `json:"describe" gorm:"size:255"`   // 策略描述
	Status     int64  `json:"status" gorm:"size:255"`     // 策略状态
	Config     string `json:"config"`                     // 配置
	Sort       int64  `json:"sort"`
	Type       int64  `json:"type"`
	Issued     string `json:"issued" gorm:"size:20"`
	CustomerID string `json:"customer_id" gorm:"size:36"`
}

func (Condition) TableName() string {
	return "conditions"
}

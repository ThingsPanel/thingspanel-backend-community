package models

type DashBoard struct {
	ID                string `json:"id" gorm:"primaryKey,size:36"`
	Configuration     string `json:"configuration" gorm:"type:longtext"` //自动json
	AssignedCustomers string `json:"assigned_customers" gorm:"type:longtext"`
	SearchText        string `json:"search_text"`
	Title             string `json:"title"`
	BusinessID        string `json:"business_id" gorm:"size:36"` // 业务id
}

func (DashBoard) TableName() string {
	return "dashboard"
}

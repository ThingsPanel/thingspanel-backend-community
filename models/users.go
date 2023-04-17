package models

type Users struct {
	ID             string `json:"id" gorm:"primaryKey,size:36"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	Enabled        string `json:"enabled" gorm:"size:5"`
	AdditionalInfo string `json:"additional_info" gorm:"type:longtext"`
	Authority      string `json:"authority"`
	CustomerID     string `json:"customer_id" gorm:"size:36"`
	Email          string `json:"email"`
	Password       string `json:"password"`
	Name           string `json:"name"`
	Mobile         string `json:"mobile" gorm:"size:20"`
	Remark         string `json:"remark" gorm:"size:100"`
	TenantID       string `json:"tenant_id" gorm:"size:36"`
}

func (Users) TableName() string {
	return "users"
}

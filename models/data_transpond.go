package models

type DataTranspond struct {
	Id          string `json:"id" gorm:"primaryKey;size:36"` // ID
	ProcessId   string `json:"process_id" gorm:"size:36"`    // 系统名称
	ProcessType string `json:"process_type" gorm:"size:36"`  // 主题
	Label       string `json:"label" gorm:"size:255"`        // 首页logo
	Disabled    string `json:"disabled" gorm:"size:10"`      // 缓冲logo
	Info        string `json:"info"  gorm:"size:255"`
	Env         string `json:"env" gorm:"size:999"`
	CustomerId  string `json:"customer_id" gorm:"size:36"`
	CreatedAt   int64  `json:"created_at"`
	RoleType    string `json:"role_type" gorm:"size:2"`
}

func (DataTranspond) TableName() string {
	return "data_transpond"
}

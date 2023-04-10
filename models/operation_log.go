package models

type OperationLog struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	Type      string `json:"type" gorm:"size:36"`
	Describe  string `json:"describe" gorm:"type:longtext"`
	DataID    string `json:"data_id" gorm:"size:36"`
	CreatedAt int64  `json:"created_at"`
	Detailed  string `json:"detailed" gorm:"type:longtext"`
	TenantId  string `json:"tenant_id,omitempty"` //租户id
}

func (OperationLog) TableName() string {
	return "operation_log"
}

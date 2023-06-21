package models

type Business struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	Name      string `json:"name" gorm:"size:255"`
	Sort      int64  `json:"sort"`
	CreatedAt int64  `json:"created_at"`
	AppType   string `json:"app_type"`   // 应用类型
	AppID     string `json:"app_id"`     // application id
	AppSecret string `json:"app_secret"` // 密钥
	TenantId  string `json:"tenant_id"`  // 租户id
}

func (Business) TableName() string {
	return "business"
}

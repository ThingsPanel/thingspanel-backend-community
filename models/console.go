package models

type Console struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdateAt  int64  `json:"update_at"`
	Data      string `json:"data"`
	Config    string `json:"config"`
	Template  string `json:"template"`
	Code      string `json:"code"`
	TenantId  string `json:"tenant_id"`
	ShareId   string `json:"share_id"`
}

func (Console) TableName() string {
	return "tp_console"
}

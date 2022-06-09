package models

type TpRole struct {
	Id       string `json:"id" gorm:"primaryKey"`     // ID
	RoleName string `json:"role_name" gorm:"size:99"` // 角色名称
	ParentId string `json:"parent_id" gorm:"size:99"` // 主题
}

func (TpRole) TableName() string {
	return "tp_role"
}

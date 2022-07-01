package models

type TpRoleMenu struct {
	RoleId string `json:"role_id" gorm:"size:36"` // 角色id
	MenuId string `json:"menu_id" gorm:"size:36"` // 菜单id
}

func (TpRoleMenu) TableName() string {
	return "tp_role_menu"
}

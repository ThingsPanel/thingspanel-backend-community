package models

type TpMenu struct {
	Id       string `json:"id" gorm:"primaryKey"`     // ID
	MenuName string `json:"menu_name" gorm:"size:99"` // 菜单名称
	ParentId string `json:"parent_id" gorm:"size:99"` // 父节点
}

func (TpMenu) TableName() string {
	return "tp_menu"
}

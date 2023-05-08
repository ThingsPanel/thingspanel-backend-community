package models

import (
	"time"
)

type Taste struct {
	Id               string       `gorm:"column:id;NOT NULL"`
	Name             string       `gorm:"column:name;NOT NULL"`
	TasteId          string       `gorm:"column:taste_id;NOT NULL"`
	CreateAt         int64        `gorm:"column:create_at;NOT NULL"`
	UpdateAt         time.Time    `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
	DeleteAt         time.Time    `gorm:"column:delete_at"`
	IsDel            bool         `gorm:"column:is_del"`
	RecipeID         string       `gorm:"column:recipe_id"`
	PotTypeId        string       `gorm:"column:pot_type_id"`
	TasteMaterialArr []*Materials `gorm:"-"`
	MaterialIdList    string       `gorm:"column:material_id_list"`
}

func (t *Taste) TableName() string {
	return "taste"
}

package models

import (
	"time"
)

type Taste struct {
	Id            string    `gorm:"column:id;NOT NULL"`
	Name          string    `gorm:"column:name;NOT NULL"`
	TasteId       string    `gorm:"column:taste_id;NOT NULL"`
	MaterialsName string    `gorm:"column:materials_name;NOT NULL"`
	Dosage        int       `gorm:"column:dosage;NOT NULL"`
	Unit          string    `gorm:"column:unit;NOT NULL"`
	CreateAt      int64     `gorm:"column:create_at;NOT NULL"`
	UpdateAt      time.Time `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
	DeleteAt      time.Time `gorm:"column:delete_at"`
	IsDel         bool      `gorm:"column:is_del"`
	WaterLine     int       `gorm:"column:water_line"`
	Station       string    `gorm:"column:station"`
	RecipeID      string    `gorm:"column:recipe_id"`
}

func (t *Taste) TableName() string {
	return "taste"
}

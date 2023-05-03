package models

import (
	"time"
)

type Recipe struct {
	Id               string    `gorm:"primaryKey;column:id;NOT NULL"`
	BottomPotId      string    `gorm:"column:bottom_pot_id"`
	BottomPot        string    `gorm:"column:bottom_pot"`
	PotTypeId        string    `gorm:"column:pot_type_id"`
	PotTypeName      string    `gorm:"column:pot_type_name"`
	Materials        string    `gorm:"column:materials"`
	Taste            string    `gorm:"column:taste"`
	BottomProperties string    `gorm:"column:bottom_properties"`
	SoupStandard     int64     `gorm:"column:soup_standard"`
	CreateAt         int64     `gorm:"column:create_at"`
	UpdateAt         time.Time `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
	DeleteAt         time.Time `gorm:"column:delete_at"`
	IsDel            bool      `gorm:"column:is_del;default:false"`
	AssetId          string    `gorm:"column:asset_id"`
	MaterialIdList   []string  `gorm:"-"`
	TasteIdList      []string  `gorm:"-"`
}

type RecipeValue struct {
	Id               string       `gorm:"primaryKey;column:id;NOT NULL"`
	BottomPotId      string       `gorm:"column:bottom_pot_id"`
	BottomPot        string       `gorm:"column:bottom_pot"`
	PotTypeId        string       `gorm:"column:pot_type_id"`
	PotTypeName      string       `gorm:"column:name"`
	Materials        string       `gorm:"column:materials"`
	MaterialArr      []*Materials `gorm:"-"`
	Taste            string       `gorm:"column:taste"`
	TasteArr         []*Taste     `gorm:"-"`
	BottomProperties string       `gorm:"column:bottom_properties"`
	SoupStandard     int64        `gorm:"column:soup_standard"`
	CreateAt         int64        `gorm:"column:create_at"`
	UpdateAt         time.Time    `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
	DeleteAt         time.Time    `gorm:"column:delete_at"`
	IsDel            bool         `gorm:"column:is_del;default:false"`
}

type EditRecipeValue struct {
	BottomPotId      string    `gorm:"column:bottom_pot_id"`
	BottomPot        string    `gorm:"column:bottom_pot"`
	PotTypeId        string    `gorm:"column:pot_type_id"`
	PotTypeName      string    `gorm:"column:pot_type_name"`
	Materials        string    `gorm:"column:materials"`
	Taste            string    `gorm:"column:taste"`
	BottomProperties string    `gorm:"column:bottom_properties"`
	SoupStandard     int64     `gorm:"column:soup_standard"`
	UpdateAt         time.Time `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
	IsDel            bool      `gorm:"column:is_del;default:false"`
}

func (r *Recipe) TableName() string {
	return "recipe"
}

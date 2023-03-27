package models

import (
	"time"
)

type PotType struct {
	Id           string    `gorm:"primaryKey;column:id;NOT NULL"`
	Name         string    `gorm:"column:name"`
	Image        string    `gorm:"column:image"`
	CreateAt     int64     `gorm:"column:create_at"`
	UpdateAt     time.Time `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
	IsDel        bool      `gorm:"column:is_del;default:false"`
	SoupStandard int       `gorm:"column:soup_standard"`
	PotTypeId    string    `gorm:"column:pot_type_id"`
}

func (p *PotType) TableName() string {
	return "pot_type"
}

package models

type PotType struct {
	Id       string `json:"id"  gorm:"primaryKey"`
	Name     string `json:"name,omitempty"`
	Image    string `json:"image,omitempty"`
	CreateAt int64  `json:"created_time,omitempty"`
	UpdateAt int64  `json:"update_at,omitempty"`
}

func (PotType) TableName() string {
	return "pot_type"
}

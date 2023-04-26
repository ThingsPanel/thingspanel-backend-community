package models

type OriginalTaste struct {
	Id         string `gorm:"column:id;NOT NULL"`
	Name       string `gorm:"column:name;NOT NULL"`
	TasteId    string `gorm:"column:taste_id;NOT NULL"`
	MaterialId string `gorm:"column:material_id;NOT NULL"`
}

func (t *OriginalTaste) TableName() string {
	return "original_taste"
}

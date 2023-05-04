package models

type Materials struct {
	Id        string `gorm:"column:id;NOT NULL"`
	Name      string `gorm:"column:name"`
	Dosage    int    `gorm:"column:dosage"`
	Unit      string `gorm:"column:unit"`
	WaterLine int    `gorm:"column:water_line"`
	Station   string `gorm:"column:station"`
	RecipeID  string `gorm:"column:recipe_id"`
	PotTypeId string `gorm:"column:pot_type_id"`
}

func (m *Materials) TableName() string {
	return "materials"
}

package models

type OriginalMaterials struct {
	Id        string `gorm:"column:id;NOT NULL"`
	Name      string `gorm:"column:name"`
	Dosage    int    `gorm:"column:dosage"`
	Unit      string `gorm:"column:unit"`
	WaterLine int    `gorm:"column:water_line"`
	Station   string `gorm:"column:station"`
	Resource  string `gorm:"column:resource"`
}

func (m *OriginalMaterials) TableName() string {
	return "original_materials"
}

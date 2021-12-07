package models

type Resources struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	CPU       string `json:"cpu" gorm:"size:36"`
	MEM       string `json:"mem" gorm:"size:36"`
	CreatedAt string `json:"created_at" gorm:"size:36"`
}

func (Resources) TableName() string {
	return "resources"
}

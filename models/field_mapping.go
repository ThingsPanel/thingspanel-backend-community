package models

type FieldMapping struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	DeviceID  string `json:"device_id" gorm:"size:36"`
	FieldFrom string `json:"field_from"`
	FieldTo   string `json:"field_to"`
	Symbol    string `json:"symbol"`
}

func (FieldMapping) TableName() string {
	return "field_mapping"
}

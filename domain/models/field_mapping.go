package models

import "database/sql"

type FieldMapping struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	DeviceID  sql.NullString `json:"device_id" gorm:"size:36"`
	FieldFrom sql.NullString `json:"field_from"`
	FieldTo   sql.NullString `json:"field_to"`
}

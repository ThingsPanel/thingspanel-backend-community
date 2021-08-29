package models

import "database/sql"

type SystemConfig struct {
	ID     string         `json:"id" gorm:"primaryKey,size:36"`
	Type   sql.NullString `json:"type"`
	Config sql.NullString `json:"config"`
}

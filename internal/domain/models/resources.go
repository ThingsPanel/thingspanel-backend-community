package models

import "database/sql"

type Resources struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	CPU       sql.NullString `json:"cpu" gorm:"size:36"`
	MEM       sql.NullString `json:"mem" gorm:"size:36"`
	CreatedAt sql.NullString `json:"created_at" gorm:"size:36"`
}

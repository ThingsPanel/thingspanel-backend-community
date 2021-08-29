package models

import "database/sql"

type Dictionary struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	Name      sql.NullString `json:"name"`
	ParentID  sql.NullString `json:"parent_id"`
	Sort      sql.NullString `json:"sort"`
	CreatedAt sql.NullInt64  `json:"created_at"`
	Code      sql.NullString `json:"code"`
}

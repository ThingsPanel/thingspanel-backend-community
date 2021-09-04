package models

import "database/sql"

type OperationLog struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	Type      sql.NullString `json:"type" gorm:"size:36"`
	Describe  sql.NullString `json:"describe" gorm:"type:longtext"`
	DataID    sql.NullString `json:"data_id" gorm:"size:36"`
	CreatedAt sql.NullInt64  `json:"created_at"`
	Detailed  sql.NullString `json:"detailed" gorm:"type:longtext"`
}

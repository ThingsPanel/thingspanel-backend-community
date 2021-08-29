package models

import "database/sql"

const (
	PRODUCTION_TYPE_PLANTING   = iota + 1 //`种植`
	PRODUCTION_TYPE_MEDICATION            // 用药
	PRODUCTION_TYPE_REWARD                // 收获

)

type Production struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	Type      *uint8         `json:"type"`  // 种植｜用药｜收获
	Name      sql.NullString `json:"name"`  // 字段名
	Value     sql.NullString `json:"value"` // 值
	CreatedAt sql.NullInt64  `json:"created_at"`
	Remark    sql.NullString `json:"remark"` // 备注
	InsertAt  sql.NullInt64  `json:"insert_at"`
}

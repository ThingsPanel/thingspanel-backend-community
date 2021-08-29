package models

import "database/sql"

type TSKV struct {
	EntityType string          `json:"entity_type" gorm:"primaryKey"`      // 类型：DEVICE
	EntryID    string          `json:"entry_id" gorm:"primaryKey,size:36"` // 设备id
	Key        string          `json:"key" gorm:"primaryKey"`              // 字段
	TS         int64           `json:"ts" gorm:"primaryKey"`               // 毫秒时间戳
	BoolV      sql.NullString  `json:"bool_v" gorm:"size:5"`
	StrV       sql.NullString  `json:"str_v" gorm:"type:longtext"`
	LongV      sql.NullInt64   `json:"long_v"`
	DoubleV    sql.NullFloat64 `json:"dbl_v"` // 数值
}

type TSKVLatest struct {
	EntityType string          `json:"entity_type" gorm:"primaryKey"`
	EntityID   string          `json:"entity_id" gorm:"entity_id,size:36"`
	Key        string          `json:"key" gorm:"primaryKey"`
	TS         int64           `json:"ts"`
	BoolV      sql.NullString  `json:"bool_v" gorm:"size:5"`
	StrV       sql.NullString  `json:"str_v" gorm:"type:longtext"`
	LongV      sql.NullInt64   `json:"long_v"`
	DoubleV    sql.NullFloat64 `json:"dbl_v"`
}

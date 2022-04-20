package models

// type TSKVLatest struct {
// 	EntityType string          `json:"entity_type" gorm:"primaryKey"`
// 	EntityID   string          `json:"entity_id" gorm:"entity_id,size:36"`
// 	Key        string          `json:"key" gorm:"primaryKey"`
// 	TS         int64           `json:"ts"`
// 	BoolV      sql.NullString  `json:"bool_v" gorm:"size:5"`
// 	StrV       sql.NullString  `json:"str_v" gorm:"type:longtext"`
// 	LongV      sql.NullInt64   `json:"long_v"`
// 	DoubleV    sql.NullFloat64 `json:"dbl_v"`
// }

type TSKVLatest struct {
	EntityType string  `json:"entity_type" gorm:"primaryKey"`
	EntityID   string  `json:"entity_id" gorm:"entity_id,size:36"`
	Key        string  `json:"key" gorm:"primaryKey"`
	TS         int64   `json:"ts"`
	BoolV      string  `json:"bool_v" gorm:"size:5"`
	StrV       string  `json:"str_v" gorm:"type:longtext"`
	LongV      int64   `json:"long_v"`
	DblV       float64 `json:"dbl_v"`
}

func (TSKVLatest) TableName() string {
	return "ts_kv_latest"
}

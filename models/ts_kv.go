package models

type TSKV struct {
	EntityType string  `json:"entity_type" gorm:"primaryKey"`       // 类型：DEVICE
	EntityID   string  `json:"entity_id" gorm:"primaryKey,size:36"` // 设备id
	Key        string  `json:"key" gorm:"primaryKey"`               // 字段
	TS         int64   `json:"ts" gorm:"primaryKey"`                // 毫秒时间戳
	BoolV      string  `json:"bool_v" gorm:"size:5"`
	StrV       string  `json:"str_v" gorm:"type:longtext"`
	LongV      int64   `json:"long_v"`
	DblV       float64 `json:"dbl_v"`
}

func (TSKV) TableName() string {
	return "ts_kv"
}

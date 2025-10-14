package storage

import (
	"encoding/json"
	"time"
)

// DataType 数据类型
type DataType string

const (
	DataTypeTelemetry DataType = "telemetry"
	DataTypeAttribute DataType = "attribute"
	DataTypeEvent     DataType = "event"
)

// Message 统一消息格式
type Message struct {
	DeviceID  string      `json:"device_id"`
	TenantID  string      `json:"tenant_id"`
	DataType  DataType    `json:"data_type"`
	Timestamp int64       `json:"timestamp"` // 毫秒时间戳
	Data      interface{} `json:"data"`
}

// TelemetryDataPoint 遥测数据点
type TelemetryDataPoint struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// AttributeDataPoint 属性数据点
type AttributeDataPoint struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// EventData 事件数据
type EventData struct {
	Identify string          `json:"identify"`
	Data     json.RawMessage `json:"data"`
}

// 数据库模型

// TelemetryData 遥测历史数据
type TelemetryData struct {
	DeviceID string   `gorm:"column:device_id;primaryKey"`
	Key      string   `gorm:"column:key;primaryKey"`
	TS       int64    `gorm:"column:ts;primaryKey"` // 毫秒时间戳
	BoolV    *bool    `gorm:"column:bool_v"`
	NumberV  *float64 `gorm:"column:number_v"`
	StringV  *string  `gorm:"column:string_v"`
	TenantID string   `gorm:"column:tenant_id"`
}

func (TelemetryData) TableName() string {
	return "telemetry_datas"
}

// TelemetryCurrentData 遥测最新值
type TelemetryCurrentData struct {
	DeviceID string    `gorm:"column:device_id;primaryKey"`
	Key      string    `gorm:"column:key;primaryKey"`
	TS       time.Time `gorm:"column:ts"`
	BoolV    *bool     `gorm:"column:bool_v"`
	NumberV  *float64  `gorm:"column:number_v"`
	StringV  *string   `gorm:"column:string_v"`
	TenantID string    `gorm:"column:tenant_id"`
}

func (TelemetryCurrentData) TableName() string {
	return "telemetry_current_datas"
}

// AttributeData 属性数据
type AttributeData struct {
	ID       string    `gorm:"column:id;primaryKey"`
	DeviceID string    `gorm:"column:device_id"`
	Key      string    `gorm:"column:key"`
	TS       time.Time `gorm:"column:ts"`
	BoolV    *bool     `gorm:"column:bool_v"`
	NumberV  *float64  `gorm:"column:number_v"`
	StringV  *string   `gorm:"column:string_v"`
	TenantID string    `gorm:"column:tenant_id"`
}

func (AttributeData) TableName() string {
	return "attribute_datas"
}

// EventDataModel 事件数据
type EventDataModel struct {
	ID       string          `gorm:"column:id;primaryKey"`
	DeviceID string          `gorm:"column:device_id"`
	Identify string          `gorm:"column:identify"`
	TS       time.Time       `gorm:"column:ts"`
	Data     json.RawMessage `gorm:"column:data;type:json"`
	TenantID string          `gorm:"column:tenant_id"`
}

func (EventDataModel) TableName() string {
	return "event_datas"
}

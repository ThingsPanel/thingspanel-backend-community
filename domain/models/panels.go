package models

import (
	"database/sql"
	"time"
)

type DashBoard struct {
	ID                string         `json:"id" gorm:"primaryKey,size:36"`
	Config            sql.NullString `json:"configuration" gorm:"type:longtext"` //自动json
	AssignedCustomers sql.NullString `json:"assigned_customers" gorm:"type:longtext"`
	SearchText        sql.NullString `json:"search_text"`
	Title             sql.NullString `json:"title"`
	BusinessID        sql.NullString `json:"business_id" gorm:"size:36"` // 业务id
}

type Widget struct {
	ID               string         `json:"id" gorm:"primaryKey,size:36"`
	DashboardID      sql.NullString `json:"dashboard_id" gorm:"size:36"`
	Config           sql.NullString `json:"config" gorm:"type:longtext"` //自动json
	Type             sql.NullString `json:"type"`
	Action           sql.NullString `json:"action"`
	UpdatedAt        *time.Time     `json:"updated_at"`
	DeviceID         sql.NullString `json:"device_id" gorm:"size:36"` // 设备id
	WidgetIdentifier sql.NullString `json:"widget_identifier"`        // 图表标识符如: environmentpanel:normal
	AssetID          sql.NullString `json:"asset_id" gorm:"size:36"`
}

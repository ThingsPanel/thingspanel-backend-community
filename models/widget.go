package models

import (
	"time"
)

type Widget struct {
	ID               string    `json:"id" gorm:"primaryKey,size:36"`
	DashboardID      string    `json:"dashboard_id" gorm:"size:36"`
	Config           string    `json:"config" gorm:"type:longtext"` //自动json
	Type             string    `json:"type"`
	Action           string    `json:"action"`
	UpdatedAt        time.Time `json:"updated_at"`
	DeviceID         string    `json:"device_id" gorm:"size:36"` // 设备id
	WidgetIdentifier string    `json:"widget_identifier"`        // 图表标识符如: environmentpanel:normal
	AssetID          string    `json:"asset_id" gorm:"size:36"`
	Extend           string    `json:"extend" gorm:"size:999"`
}

func (Widget) TableName() string {
	return "widget"
}

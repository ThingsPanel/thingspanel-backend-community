package models

import "database/sql"

type Asset struct {
	ID             string         `json:"id" gorm:"primarykey"`
	AdditionalInfo sql.NullString `json:"additional_info"`
	CustomerID     sql.NullString `json:"customer_id"`
	Name           sql.NullString `json:"name"`
	Labal          sql.NullString `json:"labal"`
	SearchText     sql.NullString `json:"search_text"`
	Type           sql.NullString `json:"type"`
	ParentID       sql.NullString `json:"parent_id"`
	Tier           int64          `json:"tier"`
	BusinessID     sql.NullString `json:"business_id"`
}
type Device struct {
	ID             string         `json:"id "gorm:"primaryKey"`
	AssetID        sql.NullString `json:"asset_id"`        // 资产id
	Token          sql.NullString `json:"token"`           // 安全key
	AdditionalInfo sql.NullString `json:"additional_info"` // 存储基本配置
	CustomerID     sql.NullString `json:"customer_id"`
	Type           sql.NullString `json:"type"` // 插件类型
	Name           sql.NullString `json:"name"` // 插件名
	Label          sql.NullString `json:"label"`
	SearchText     sql.NullString `json:"search_text"`
	Extension      sql.NullString `json:"extension"` // 插件( 目录名)
}

type Business struct {
	ID        string         `json:"id gorm:"primaryKey"`
	Name      sql.NullString `json:"name"`
	CreatedAT sql.NullString `json:"created_at"`
	AppType   string         `json:"app_type"`   // 应用类型
	AppID     string         `json:"app_id"`     // application id
	AppSecret string         `json:"app_secret"` // 密钥
}

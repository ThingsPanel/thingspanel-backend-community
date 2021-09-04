package models

import "database/sql"

type Asset struct {
	ID             string         `json:"id" gorm:"primarykey"`
	AdditionalInfo sql.NullString `json:"additional_info" gorm:"type:longtext"`
	CustomerID     sql.NullString `json:"customer_id" gorm:"size:36"` // 客户ID
	Name           sql.NullString `json:"name"`                       // 名称
	Labal          sql.NullString `json:"labal"`                      // 标签
	SearchText     sql.NullString `json:"search_text"`
	Type           sql.NullString `json:"type"`                       // 类型
	ParentID       sql.NullString `json:"parent_id" gorm:"size:36"`   // 父级ID
	Tier           int64          `json:"tier"`                       // 层级
	BusinessID     sql.NullString `json:"business_id" gorm:"size:36"` // 业务ID
}
type Device struct {
	ID             string         `json:"id" gorm:"primaryKey,size:36"`
	AssetID        sql.NullString `json:"asset_id" gorm:"size:36"`              // 资产id
	Token          sql.NullString `json:"token"`                                // 安全key
	AdditionalInfo sql.NullString `json:"additional_info" gorm:"type:longtext"` // 存储基本配置
	CustomerID     sql.NullString `json:"customer_id" gorm:"size:36"`
	Type           sql.NullString `json:"type"` // 插件类型
	Name           sql.NullString `json:"name"` // 插件名
	Label          sql.NullString `json:"label"`
	SearchText     sql.NullString `json:"search_text"`
	Extension      sql.NullString `json:"extension" gorm:"size:50"` // 插件( 目录名)
}

type Business struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	Name      sql.NullString `json:"name" gorm:"size:36"`
	CreatedAt sql.NullInt64  `json:"created_at"`
	AppType   string         `json:"app_type"`   // 应用类型
	AppID     string         `json:"app_id"`     // application id
	AppSecret string         `json:"app_secret"` // 密钥
}

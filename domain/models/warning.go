package models

import "database/sql"

type WarningLog struct {
	ID        string         `json:"id" gorm:"primaryKey,size:36"`
	Type      sql.NullString `json:"type" gorm:"size:36"`
	Describe  sql.NullString `json:"describe"`
	DataID    sql.NullString `json:"data_id" gorm:"size:36"`
	CreatedAt sql.NullInt64  `json:"created_at"`
}

type WarningConfig struct {
	ID         string         `json:"id" gorm:"primaryKey,size:36"`
	WarningID  string         `json:"wid"`                      // 业务ID
	Name       sql.NullString `json:"name"`                     // 预警名称
	Describe   sql.NullString `json:"describe"`                 // 预警描述
	Config     sql.NullString `json:"config" gorm:"type:text"`  // 配置
	Message    sql.NullString `json:"message" gorm:"type:text"` // 消息模板
	Bid        sql.NullString `json:"bid"`
	Sensor     sql.NullString `json:"sensor" gorm:"size:100"`
	CustomerID sql.NullString `json:"customer_id" gorm:"size:36"`
}

package models

import (
	"database/sql"
)

type Conditions struct {
	ID         string         `json:"id" gorm:"primaryKey"`
	BusinessID sql.NullString `json:"business_id"` // 业务ID
	Name       sql.NullString `json:"name"`        // 策略名称
	Describe   sql.NullString `json:"describe"`    // 策略描述
	Status     sql.NullString `json:"status"`      // 策略状态
	Config     sql.NullString `json:"config"`      // 配置
	Sort       sql.NullInt64  `json:"sort"`
	Type       sql.NullInt64  `json:"type"`
	Issued     sql.NullString `json:"issued"`
	CustomerID sql.NullString `json:"customer_id"`
}

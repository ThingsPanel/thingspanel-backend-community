package models

import "database/sql"

const (
	NAVIGATION_TYPE_BUSINESS      = iota + 1 // 业务
	NAVIGATION_TYPE_ACS                      // 自动化-控制策略 , automation control strategy
	NAVIGATION_TYPE_AAS                      // 自动化-告警策略 , automation alert strategy
	NAVIGATION_TYPE_VISUALIZATION            // 可视化

)

type Navigation struct {
	ID    string         `json:"id" gorm:"primaryKey,size:36"`
	Type  sql.NullInt64  `json:"type"` // 1:业务  2：自动化-控制策略 3：自动化-告警策略  4：可视化
	Name  sql.NullString `json:"name"`
	Data  sql.NullString `json:"data"`
	Count sql.NullInt64  `json:"count"` // 数量
}

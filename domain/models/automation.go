package models

import (
	"database/sql"
)

const (
	AUTOMATION_STATUS_ED    = iota // 每天执行 , execution daily
	AUTOMATION_STATUS_EE1M         // 每一分钟执行一次 , execution every minute
	AUTOMATION_STATUS_EE5M         // 每五分钟执行一次 , execution every 5 minutes
	AUTOMATION_STATUS_EE10M        //  每五分钟执行一次 , execution every 10 minutes
	AUTOMATION_STATUS_EE1H         // 每一小时执行一次 , execution every hour
	AUTOMATION_STATUS_EE3H         // 每三小时执行一次, execution every three hours
	AUTOMATION_STATUS_EE6H         // 每六小时执行一次, execution every six hours
	AUTOMATION_STATUS_EE12H        // 每十二小时执行一次, execution every 12 hours
)

type Conditions struct {
	ID         string         `json:"id" gorm:"primaryKey,size:36"`
	BusinessID sql.NullString `json:"business_id" gorm:"size:36"` // 业务ID
	Name       sql.NullString `json:"name"`                       // 策略名称
	Describe   sql.NullString `json:"describe"`                   // 策略描述
	Status     sql.NullString `json:"status"`                     // 策略状态
	Config     sql.NullString `json:"config"`                     // 配置
	Sort       sql.NullInt64  `json:"sort"`
	Type       sql.NullInt64  `json:"type"`
	Issued     sql.NullString `json:"issued" gorm:"size:20"`
	CustomerID sql.NullString `json:"customer_id" gorm:"size:36"`
}

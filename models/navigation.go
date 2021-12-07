package models

const (
	NAVIGATION_TYPE_BUSINESS      = iota + 1 // 业务
	NAVIGATION_TYPE_ACS                      // 自动化-控制策略 , automation control strategy
	NAVIGATION_TYPE_AAS                      // 自动化-告警策略 , automation alert strategy
	NAVIGATION_TYPE_VISUALIZATION            // 可视化

)

type Navigation struct {
	ID    string `json:"id" gorm:"primaryKey,size:36"`
	Type  int64  `json:"type"`
	Name  string `json:"name"`
	Data  string `json:"data"`
	Count int64  `json:"count"`
}

func (Navigation) TableName() string {
	return "navigation"
}

package models

type Chart struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	ChartType int64  `json:"chart_type,omitempty"` //图表类型1-折线 2-仪表
	ChartData string `json:"chart_data,omitempty" gorm:"type:longtext"`
	ChartName string `json:"chart_name,omitempty" gorm:"size:99"`
	CreatedAt int64  `json:"created_at,omitempty"`
	Sort      int64  `json:"sort"`
	Issued    int64  `json:"issued"`
	Remark    string `json:"remark,omitempty" gorm:"size:255"`
	Flag      int64  `json:"flag"` //是否发布0-未发布1-已发布
}

func (Chart) TableName() string {
	return "chart"
}

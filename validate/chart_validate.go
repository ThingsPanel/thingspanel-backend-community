package valid

import "ThingsPanel-Go/models"

type ChartValidate struct {
	Id        string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	ChartType string `json:"chart_type,omitempty" alias:"图表类型"`
	ChartData string `json:"chart_data,omitempty" alias:"数据"`                     // 数据
	ChartName string `json:"chart_name,omitempty" alias:"名称" valid:"MaxSize(99)"` // 名称
	Sort      int64  `json:"sort,omitempty" alias:"排序" `                          // 排序
	Issued    int64  `json:"issued,omitempty" alias:"是否发布0-未发布1-已发布"`             // 是否发布0-未发布1-已发布
	CreatedAt int64  `json:"created_at,omitempty" alias:"创建时间"`
	Remark    string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(99)"`
	Flag      int64  `json:"flag,omitempty" alias:"图表标志"`
}

type AddChartValidate struct {
	ChartType string `json:"chart_type,omitempty" alias:"图表类型"`
	ChartData string `json:"chart_data,omitempty" alias:"数据"`                     // 数据
	ChartName string `json:"chart_name,omitempty" alias:"名称" valid:"MaxSize(99)"` // 名称
	Sort      int64  `json:"sort,omitempty" alias:"排序" `                          // 排序
	Issued    int64  `json:"issued,omitempty" alias:"是否发布0-未发布1-已发布"`             // 是否发布0-未发布1-已发布
	CreatedAt int64  `json:"created_at,omitempty" alias:"创建时间"`
	Remark    string `json:"remark,omitempty" alias:"备注" valid:"MaxSize(99)"`
	Flag      int64  `json:"flag,omitempty" alias:"图表标志"`
}

type ChartPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Issued      int    `json:"issued" alias:"发布状态" valid:"Max(36)"`
	ChartType   string `json:"chart_type" alias:"图表类型" valid:"MaxSize(36)"`
	Flag        int    `json:"flag" alias:"标志" valid:"Max(36)"`
}

type RspChartPaginationValidate struct {
	CurrentPage int            `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int            `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.Chart `json:"data" alias:"返回数据"`
	Total       int64          `json:"total" alias:"总数" valid:"Max(10000)"`
}

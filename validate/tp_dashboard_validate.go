package valid

import "ThingsPanel-Go/models"

type TpDashboardValidate struct {
	Id            string `json:"id,omitempty"  valid:"MaxSize(36)"`
	RelationId    string `json:"relation_id,omitempty"  valid:"MaxSize(36)"`
	JsonData      string `json:"json_data,omitempty"`
	DashboardName string `json:"dashboard_name,omitempty"  valid:"MaxSize(99)"`
	CreateAt      int64  `json:"create_at,omitempty"`
	Sort          int64  `json:"sort,omitempty"`
	Remark        string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type AddTpDashboardValidate struct {
	RelationId    string `json:"relation_id,omitempty"  valid:"MaxSize(36)"`
	JsonData      string `json:"json_data,omitempty"`
	DashboardName string `json:"dashboard_name,omitempty"  valid:"MaxSize(99)"`
	CreateAt      int64  `json:"create_at,omitempty"`
	Sort          int64  `json:"sort,omitempty"`
	Remark        string `json:"remark,omitempty" valid:"MaxSize(255)"`
}

type TpDashboardPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	RelationId  string `json:"relation_id" alias:"发布状态" valid:"MaxSize(36)"`
}

type RspTpDashboardPaginationValidate struct {
	CurrentPage int                  `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                  `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpDashboard `json:"data" alias:"返回数据"`
	Total       int64                `json:"total" alias:"总数" valid:"Max(10000)"`
}

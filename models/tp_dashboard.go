package models

type TpDashboard struct {
	Id            string `json:"id,omitempty"`
	RelationId    string `json:"relation_id,omitempty"`
	JsonData      string `json:"json_data,omitempty"`
	DashboardName string `json:"dashboard_name,omitempty"`
	CreateAt      int64  `json:"create_at,omitempty"`
	Sort          int64  `json:"sort,omitempty"`
	Remark        string `json:"remark,omitempty"`
	TenantId      string `json:"tenant_id,omitempty"` // 租户id
	ShareId       string `json:"share_id,omitempty"`  // 分享id
}

func (TpDashboard) TableName() string {
	return "tp_dashboard"
}

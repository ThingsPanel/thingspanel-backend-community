package model

//create struct
type CreateDashboardReq struct {
	RelationId    *string `json:"relation_id" validate:"omitempty,max=36"`
	JsonData      *string `json:"json_data"  validate:"omitempty"`
	DashboardName *string `json:"dashboard_name" validate:"omitempty,max=99"`
	CreateAt      *string `json:"create_at" validate:"omitempty"`
	Sort          *int32  `json:"sort" validate:"omitempty"`
	Remark        *string `json:"remark" validate:"omitempty,max=255"`
}

//put struct
type UpdateDashboardReq struct {
	Id            string  `json:"id" validate:"required,max=36"`
	RelationId    *string `json:"relation_id" validate:"omitempty,max=36"`
	JsonData      *string `json:"json_data"  validate:"omitempty"`
	DashboardName *string `json:"dashboard_name" validate:"omitempty,max=99"`
	CreateAt      *string `json:"create_at" validate:"omitempty"`
	Sort          *int32  `json:"sort" validate:"omitempty"`
	Remark        *string `json:"remark" validate:"omitempty,max=255"`
}

//list struct
type DashboardListReq struct {
	PageReq
	RelationId *string `json:"relation_id" form:"relation_id" validate:"omitempty,max=36"`
	Id         *string `json:"id" form:"id" validate:"omitempty,max=36"`
	ShareId    *string `json:"share_id" form:"share_id" validate:"omitempty,max=36"`
}

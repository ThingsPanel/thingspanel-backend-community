package model

type CreateBoardReq struct {
	Name        string  `json:"name" validate:"required,max=255"`         // 看板名称
	Config      *string `json:"config" validate:"omitempty"`              // 看板配置
	HomeFlag    string  `json:"home_flag"  validate:"required,max=2"`     // 首页标志默认N，Y
	MenuFlag    string  `json:"menu_flag"`                                // 菜单标志默认N，Y
	Description *string `json:"description" validate:"omitempty,max=500"` // 描述
	Remark      *string `json:"remark" validate:"omitempty,max=255"`
	TenantID    string  `json:"tenant_id" validate:"omitempty,max=36"` //租户id
}

type UpdateBoardReq struct {
	Id          string  `json:"id" validate:"omitempty,max=36"`
	Name        string  `json:"name" validate:"omitempty,max=255"`        // 看板名称
	Config      *string `json:"config" validate:"omitempty"`              // 看板配置
	HomeFlag    string  `json:"home_flag"  validate:"omitempty,max=2"`    // 首页标志默认N，Y
	MenuFlag    string  `json:"menu_flag"  validate:"omitempty,max=2"`    // 菜单标志默认N，Y
	Description *string `json:"description" validate:"omitempty,max=500"` // 描述
	Remark      *string `json:"remark" validate:"omitempty,max=255"`
	TenantID    string  `json:"tenant_id" validate:"omitempty,max=36"` //租户id
}

type GetBoardListByPageReq struct {
	PageReq
	Name     *string `json:"name" form:"name" validate:"omitempty,max=255"`
	HomeFlag *string `json:"home_flag" form:"home_flag"  validate:"omitempty,max=2"`
}

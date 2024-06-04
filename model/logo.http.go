package model

type UpdateLogoReq struct {
	Id             string  `json:"id" validate:"required,max=36"`                // Id
	SystemName     string  `json:"system_name" validate:"omitempty,max=99"`      // 系统名称
	LogoCache      *string `json:"logo_cache" validate:"omitempty,max=255"`      // 缓冲logo
	LogoBackground *string `json:"logo_background" validate:"omitempty,max=255"` // 站标Logo
	LogoLoading    *string `json:"logo_loading" validate:"omitempty,max=255"`    // 加载页面Logo
	HomeBackground *string `json:"home_background" validate:"omitempty,max=255"` // 首页背景
	Remark         *string `json:"remark" validate:"omitempty,max=255"`
}

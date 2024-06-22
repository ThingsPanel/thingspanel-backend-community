package model

type CreateServicePluginReq struct {
	Name              string `json:"name" binding:"required,max=255"`
	ServiceIdentifier string `json:"service_identifier" binding:"required,max=100"`
	ServiceType       int32  `json:"service_type" binding:"required,oneof=1 2"`
	Version           string `json:"version" binding:"omitempty,max=100"`
	Description       string `json:"description" binding:"omitempty,max=255"`
	ServiceConfig     string `json:"service_config" binding:"omitempty"`
	Remark            string `json:"remark" binding:"omitempty,max=255"`
}

type GetServicePluginByPageReq struct {
	PageReq
	ServiceType int32 `json:"service_type" form:"service_type"`
}

type GetServicePluginReq struct {
	ID string `json:"id" form:"id"`
}

type UpdateServicePluginReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	Name              string `json:"name" binding:"max=255"`
	ServiceIdentifier string `json:"service_identifier" binding:"max=100"`
	ServiceType       int32  `json:"service_type" binding:"omitempty,oneof=1 2"`
	Version           string `json:"version" binding:"max=100"`
	Description       string `json:"description" binding:"max=255"`
	Remark            string `json:"remark" binding:"max=255"`

	ServiceConfig string `json:"service_config" binding:"omitempty"`
}

type DeleteServicePluginReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

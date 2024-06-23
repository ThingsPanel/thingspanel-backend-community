package model

type CreateAccessReq struct {
	Name                string `json:"name" binding:"required"`
	ServicePluginID     string `json:"service_plugin_id" binding:"required"`
	Voucher             string `json:"voucher" binding:"required"`
	Description         string `json:"description"`
	ServiceAccessConfig string `json:"service_access_config"`
	TenantID            string `json:"tenant_id" binding:"required"`
	Remark              string `json:"remark" `
}

type UpdateAccessReq struct {
	ID                  string `json:"id" binding:"required"`
	ServiceAccessConfig string `json:"service_access_config"`
}

type DeleteAccessReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

type GetServiceAccessByPageReq struct {
	PageReq
	ServicePluginID string `json:"service_plugin_id" form:"service_plugin_id"`
}

type GetServiceAccessVoucherFormReq struct {
	ServicePluginID string `json:"service_plugin_id" form:"service_plugin_id"  binding:"required"`
}

// 服务接入点设备列表 voucher page_size page
type ServiceAccessDeviceListReq struct {
	PageReq
	Voucher string `json:"voucher" form:"voucher" binding:"required"`
}

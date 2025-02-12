// internal/model/open_api_keys.http.go
package model

// OpenAPIKeyListReq 查询API密钥列表请求
type OpenAPIKeyListReq struct {
	PageReq        // 继承基础分页请求
	Status  *int16 `json:"status" form:"status" validate:"omitempty,oneof=0 1"` // 状态: 0-禁用 1-启用
}

// CreateOpenAPIKeyReq 创建API密钥请求
type CreateOpenAPIKeyReq struct {
	TenantID string `json:"tenant_id" validate:"required,max=36"` // 租户ID
	Name     string `json:"name" validate:"omitempty,max=200"`    // 名称
}

// UpdateOpenAPIKeyReq 更新API密钥请求
type UpdateOpenAPIKeyReq struct {
	ID     string  `json:"id" validate:"required,max=36"`         // 主键ID
	Status *int16  `json:"status" validate:"omitempty,oneof=0 1"` // 状态: 0-禁用 1-启用
	Name   *string `json:"name" validate:"omitempty,max=200"`     // 名称
}

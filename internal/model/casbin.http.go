package model

type FunctionsRoleValidate struct {
	RoleID       string   `json:"role_id"  valid:"Required; MaxSize(36)"`                   //角色
	FunctionsIDs []string `json:"functions_ids"  alias:"功能列表" valid:"Required;MaxSize(36)"` //功能列表
}

type RoleValidate struct {
	RoleID string `json:"role_id"  form:"role_id" valid:"Required; MaxSize(36)"` //角色
}

type RolesUserValidate struct {
	UserID   string   `json:"user_id"  valid:"Required; MaxSize(36)"`   //用户
	RolesIDs []string `json:"roles_ids"   valid:"Required;MaxSize(36)"` //角色列表
}

type UserValidate struct {
	UserID string `json:"user_id"  form:"user_id" valid:"Required; MaxSize(255)"` //用户
}

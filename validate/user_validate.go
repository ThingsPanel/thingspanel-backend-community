package valid

// PaginateUser  校验
type PaginateUser struct {
	Search    string `json:"search" alias:"查询内容" valid:"MaxSize(36)"`
	Authority string `json:"authority" alias:"权限" valid:"MaxSize(36)"`
	Limit     int    `json:"limit" alias:"条数" valid:"Max(100)"`
	Page      int    `json:"page" alias:"页面" valid:"Min(1)"`
}

// AddUser 校验
type AddUser struct {
	Name           string `json:"name" alias:"姓名" valid:"Required; MaxSize(255)"`
	Email          string `json:"email" alias:"邮箱" valid:"Required; Email; MaxSize(100)"`
	Password       string `json:"password" alias:"密码" valid:"Required; MaxSize(255)"`
	Enabled        string `json:"enabled" alias:"状态" valid:"MaxSize(5)"`
	Mobile         string `json:"mobile" alias:"手机号" valid:"Mobile;"`
	Remark         string `json:"remark" alias:"备注" valid:"MaxSize(255)"`
	Authority      string `json:"authority" alias:"权限" valid:"Required;MaxSize(255)"` // 用户权限：SYS_ADMIN-系统管理员 TENANT_ADMIN-租户管理员 TENANT_USER-租户用户
	AdditionalInfo string `json:"additional_info" alias:"附加信息" valid:"MaxSize(255)"`  // 附加信息
	TenantID       string `json:"tenant_id" alias:"租户ID" valid:"MaxSize(36)"`         // 租户ID
}

// EditUser 校验
type EditUser struct {
	ID      string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	Name    string `json:"name" alias:"姓名" valid:"Required; MaxSize(255)"`
	Email   string `json:"email" alias:"邮箱" valid:"Email; MaxSize(100)"`
	Mobile  string `json:"mobile" alias:"手机号" valid:"Mobile; Required;"`
	Remark  string `json:"remark" alias:"备注" valid:"MaxSize(255)"`
	Enabled string `json:"enabled" alias:"状态" valid:"MaxSize(5)"`
}

// DeleteUser 校验
type DeleteUser struct {
	ID string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
}

// PasswordUser 校验
type PasswordUser struct {
	ID          string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	OldPassword string `json:"old_password" alias:"原密码" valid:"Required; MaxSize(255)"`
	Password    string `json:"password" alias:"密码" valid:"Required; MaxSize(255)"`
}

type SaveTenantConfig struct {
	// Remark    string `json:"remark"`
	ModelType string `json:"model_type" alias:"ModelType" valid:"Required"`
	ApiKey    string `json:"api_key" alias:"ApiKey" valid:"Required"`
	BashUrl   string `json:"bash_url" alias:"BashUrl"`
}

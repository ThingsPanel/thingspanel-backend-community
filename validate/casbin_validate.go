package valid

type FunctionsRoleValidate struct {
	Role      string   `json:"role" alias:"角色" valid:"Required; MaxSize(255)"`
	Functions []string `json:"functions"  alias:"功能列表" valid:"Required;MaxSize(255)"`
}

type RoleValidate struct {
	Role string `json:"role" alias:"角色" valid:"Required; MaxSize(255)"`
}

type RolesUserValidate struct {
	User  string   `json:"user" alias:"用户" valid:"Required; MaxSize(255)"`
	Roles []string `json:"roles"  alias:"角色列表" valid:"Required;MaxSize(255)"`
}

type UserValidate struct {
	User string `json:"user" alias:"用户" valid:"Required; MaxSize(255)"`
}

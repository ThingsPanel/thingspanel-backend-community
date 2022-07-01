package valid

type TpRoleValidate struct {
	Id           string `json:"id"  alias:"ID" valid:"MaxSize(36)"` // ID
	RoleName     string `json:"role_name"  alias:"角色名称" valid:"MaxSize(99)"`
	RoleDescribe string `json:"role_describe"  alias:"角色描述" valid:"MaxSize(255)"`
	ParentId     string `json:"parent_id"  alias:"父id" valid:"MaxSize(36)"`
}

type TpRoleMenuValidate struct {
	RoleId  string   `json:"role_id"  alias:"角色id" valid:"Required;MaxSize(36)"` // ID
	MenuIds []string `json:"menu_ids"  alias:"菜单id" valid:"Required;MaxSize(36)"`
}

type TpRoleIdValidate struct {
	RoleId string `json:"role_id"  alias:"角色id" valid:"Required;MaxSize(36)"` // ID
}

type EmailValidate struct {
	Email string `json:"email"  alias:"用户邮箱" valid:"Required;MaxSize(36)"` // ID
}

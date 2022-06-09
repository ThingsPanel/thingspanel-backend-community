package valid

type TpRoleValidate struct {
	Id       string `json:"id"  alias:"ID" valid:"MaxSize(36)"` // ID
	RoleName string `json:"role_name"  alias:"角色名称" valid:"MaxSize(99)"`
	ParentId string `json:"parent_id"  alias:"父id" valid:"MaxSize(36)"`
}

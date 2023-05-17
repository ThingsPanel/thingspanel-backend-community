package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"

	"github.com/beego/beego/v2/core/logs"
)

type TpRoleService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取角色列表
func (*TpRoleService) GetRoleList(pageSize int, offset int, tenantId string) (int64, []models.TpRole) {
	var TpRoles []models.TpRole
	var count int64
	psql.Mydb.Model(&models.TpRole{}).Where("tenant_id = ?", tenantId).Count(&count)
	offset = pageSize * (offset - 1)
	result := psql.Mydb.Model(&models.TpRole{}).Where("tenant_id = ?", tenantId).Offset(offset).Limit(pageSize).Order("role_name asc").Find(&TpRoles)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return count, TpRoles
	}

	return count, TpRoles
}

// Add新增角色
func (*TpRoleService) AddRole(tp_role models.TpRole) (bool, models.TpRole) {
	var uuid = uuid.GetUuid()
	tp_role.Id = uuid
	result := psql.Mydb.Create(&tp_role)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false, tp_role
	}
	return true, tp_role
}

// 根据ID编辑role
func (*TpRoleService) EditRole(tp_role models.TpRole, tenantId string) bool {
	result := psql.Mydb.Model(&models.TpRole{}).Where("tenant_id = ?", tenantId).Updates(&tp_role)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// 删除角色
func (*TpRoleService) DeleteRole(tp_role models.TpRole, tenantId string) bool {
	result := psql.Mydb.Model(&models.TpRole{}).Where("tenant_id = ?", tenantId).Delete(&tp_role)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

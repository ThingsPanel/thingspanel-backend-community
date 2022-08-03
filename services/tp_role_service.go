package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"gorm.io/gorm"
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
func (*TpRoleService) GetRoleList(pageSize int, offset int) (int64, []models.TpRole) {
	var TpRoles []models.TpRole
	var count int64
	psql.Mydb.Model(&models.TpRole{}).Count(&count)
	offset = pageSize * (offset - 1)
	result := psql.Mydb.Model(&models.TpRole{}).Offset(offset).Limit(pageSize).Order("role_name asc").Find(&TpRoles)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
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
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, tp_role
	}
	return true, tp_role
}

// 根据ID编辑role
func (*TpRoleService) EditRole(tp_role models.TpRole) bool {
	result := psql.Mydb.Updates(&tp_role)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除角色
func (*TpRoleService) DeleteRole(tp_role models.TpRole) bool {
	result := psql.Mydb.Delete(&tp_role)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

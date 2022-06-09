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
func (*TpRoleService) GetRoleList() (bool, []models.TpRole) {
	var TpRoles []models.TpRole
	result := psql.Mydb.Model(&models.TpRole{}).Find(&TpRoles)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpRoles
	}
	return true, TpRoles
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
	result := psql.Mydb.Save(&tp_role)
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

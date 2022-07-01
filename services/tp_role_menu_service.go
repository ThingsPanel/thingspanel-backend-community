package services

import (
	"ThingsPanel-Go/models"
	"errors"

	"ThingsPanel-Go/initialize/psql"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpRoleMenuService struct {
}

// 通过用户名获取菜单
func (*TpRoleMenuService) GetRoleMenuListByUser(userName string) (bool, []string) {
	var menusMap []string
	//var menusMap []map[string]interface{}
	var result *gorm.DB
	if userName == "admin@thingspanel.cn" {
		result = psql.Mydb.Raw("select menu_name from tp_menu ").Scan(&menusMap)
	} else {
		var CasbinService CasbinService
		roleList, _ := CasbinService.GetRoleFromUser(userName)
		if len(roleList) == 0 {
			return false, menusMap
		}
		whereSql := ""
		for _, role := range roleList {
			if whereSql != "" {
				whereSql = whereSql + ","
			}
			whereSql = whereSql + "'" + role + "'"
		}
		result = psql.Mydb.Raw("select distinct(tm.menu_name) from tp_role_menu trm left join tp_menu tm on trm.menu_id =tm.id where trm.role_id in (" + whereSql + ")").Scan(&menusMap)
	}
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, menusMap
	}
	logs.Info(menusMap, "=============================")
	// for _, menuMap := range menusMap {
	// 	menuList = append(menuList, menuMap["menu_name"])
	// }
	return true, menusMap
}

// 获取角色的菜单
func (*TpRoleMenuService) GetRoleMenu(roleId string) []string {
	var menuIds []string
	result := psql.Mydb.Raw("select menu_id from tp_role_menu where role_id = '" + roleId + "'").Scan(&menuIds)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return menuIds
}

// 给角色添加菜单
func (*TpRoleMenuService) AddRoleMenu(roleId string, menuIds []string) bool {
	tx := psql.Mydb.Begin()
	for _, menuId := range menuIds {
		tpRoleMenu := models.TpRoleMenu{
			RoleId: roleId,
			MenuId: menuId,
		}
		err := tx.Create(&tpRoleMenu)
		if err.Error != nil {
			tx.Rollback()
			return false
		}
	}
	tx.Commit()
	return true
}

// 修改角色的菜单
func (*TpRoleMenuService) EditRoleMenu(roleId string, menuIds []string) bool {
	tx := psql.Mydb.Begin()

	ts_err := tx.Where(models.TpRoleMenu{RoleId: roleId}).Delete(models.TpRoleMenu{})
	if ts_err.Error != nil {
		tx.Rollback()
		return false
	}
	for _, menuId := range menuIds {
		tpRoleMenu := models.TpRoleMenu{
			RoleId: roleId,
			MenuId: menuId,
		}
		err := tx.Create(&tpRoleMenu)
		if err.Error != nil {
			tx.Rollback()
			return false
		}
	}
	tx.Commit()
	return true
}

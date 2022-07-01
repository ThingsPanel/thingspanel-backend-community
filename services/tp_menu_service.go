package services

import (
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"ThingsPanel-Go/initialize/psql"

	"gorm.io/gorm"
)

type TpMenuService struct {
}

// 获取菜单列表
func (*TpMenuService) GetMenuList() (bool, []models.TpMenu) {
	var TpMenus []models.TpMenu
	result := psql.Mydb.Model(&models.TpMenu{}).Find(&TpMenus)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpMenus
	}
	return true, TpMenus
}

// 获取菜单树
func (*TpMenuService) GetMenuTree() (bool, []map[string]interface{}) {
	var TpMenuTree []map[string]interface{}
	result := psql.Mydb.Model(&models.TpMenu{}).Where("parent_id = '0'").Find(&TpMenuTree)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpMenuTree
	}
	for _, TpMenu := range TpMenuTree {
		var TpMenus []map[string]interface{}
		result := psql.Mydb.Model(&models.TpMenu{}).Where("parent_id = ?", TpMenu["id"].(string)).Find(&TpMenus)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
			return false, TpMenuTree
		}
		TpMenu["child_node"] = TpMenus
	}
	return true, TpMenuTree
}

// Add新增菜单
func (*TpMenuService) AddMenu(tp_Menu models.TpMenu) (bool, models.TpMenu) {
	var uuid = uuid.GetUuid()
	tp_Menu.Id = uuid
	result := psql.Mydb.Create(&tp_Menu)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, tp_Menu
	}
	return true, tp_Menu
}

// 根据ID编辑菜单
func (*TpMenuService) EditMenu(tp_Menu models.TpMenu) bool {
	result := psql.Mydb.Save(&tp_Menu)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除菜单
func (*TpMenuService) DeleteMenu(tp_Menu models.TpMenu) bool {
	result := psql.Mydb.Delete(&tp_Menu)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

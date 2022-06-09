package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"gorm.io/gorm"
)

type TpFunctionService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取角色列表
func (*TpFunctionService) GetFunctionList() (bool, []models.TpFunction) {
	var TpFunctions []models.TpFunction
	result := psql.Mydb.Model(&models.TpFunction{}).Find(&TpFunctions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpFunctions
	}
	return true, TpFunctions
}

// Add新增角色
func (*TpFunctionService) AddFunction(tp_function models.TpFunction) (bool, models.TpFunction) {
	var uuid = uuid.GetUuid()
	tp_function.Id = uuid
	result := psql.Mydb.Create(&tp_function)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, tp_function
	}
	return true, tp_function
}

// 根据ID编辑role
func (*TpFunctionService) EditFunction(tp_function models.TpFunction) bool {
	result := psql.Mydb.Save(&tp_function)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除角色
func (*TpFunctionService) DeleteFunction(tp_function models.TpFunction) bool {
	result := psql.Mydb.Delete(&tp_function)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

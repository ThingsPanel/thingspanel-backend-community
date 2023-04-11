package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"github.com/beego/beego/v2/core/logs"
)

type DataTranspondService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*DataTranspondService) GetDataTranspondList(PaginationValidate valid.PaginationValidate) (bool, []models.DataTranspond, int64) {
	var DataTransponds []models.DataTranspond
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.DataTranspond{})
	if PaginationValidate.Disabled != "" {
		db.Where("disabled = ?", PaginationValidate.Disabled)
	}
	if PaginationValidate.ProcessType != "" {
		db.Where("process_type = ?", PaginationValidate.ProcessType)
	}
	if PaginationValidate.RoleType != "" {
		db.Where("role_type = ?", PaginationValidate.RoleType)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&DataTransponds)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, DataTransponds, 0
	}
	return true, DataTransponds, count
}

// 新增数据
func (*DataTranspondService) AddDataTranspond(data_transpond models.DataTranspond) (bool, models.DataTranspond) {
	var uuid = uuid.GetUuid()
	data_transpond.Id = uuid
	result := psql.Mydb.Create(&data_transpond)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, data_transpond
	}
	return true, data_transpond
}

// 修改数据
func (*DataTranspondService) EditDataTranspond(data_transpond models.DataTranspond) bool {
	result := psql.Mydb.Updates(&data_transpond)
	//result := psql.Mydb.Save(&data_transpond)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*DataTranspondService) DeleteDataTranspond(data_transpond models.DataTranspond) bool {
	result := psql.Mydb.Delete(&data_transpond)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

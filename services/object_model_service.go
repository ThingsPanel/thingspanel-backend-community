package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"

	"gorm.io/gorm"
)

type ObjectModelService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*ObjectModelService) GetObjectModelDetail(object_model_id string) []models.ObjectModel {
	var ObjectModel []models.ObjectModel
	psql.Mydb.First(&ObjectModel, "id = ?", object_model_id)
	return ObjectModel
}

// 获取列表
func (*ObjectModelService) GetObjectModelList(PaginationValidate valid.ObjectModelPaginationValidate) (bool, []models.ObjectModel, int64) {
	var ObjectModels []models.ObjectModel
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=?"
	var params []interface{}
	params = append(params, 1)
	if PaginationValidate.ObjectType != "" {
		sqlWhere += " and object_type = ?"
		params = append(params, PaginationValidate.ObjectType)
	}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = ?"
		params = append(params, PaginationValidate.Id)
	}
	var count int64
	psql.Mydb.Model(&models.ObjectModel{}).Where(sqlWhere, params...).Count(&count)
	result := psql.Mydb.Model(&models.ObjectModel{}).Where(sqlWhere, params...).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&ObjectModels)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ObjectModels, 0
	}
	return true, ObjectModels, count
}

// 新增数据
func (*ObjectModelService) AddObjectModel(object_model models.ObjectModel) (bool, models.ObjectModel) {
	var uuid = uuid.GetUuid()
	object_model.Id = uuid
	result := psql.Mydb.Create(&object_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, object_model
	}
	return true, object_model
}

// 修改数据
func (*ObjectModelService) EditObjectModel(object_model valid.ObjectModelValidate) bool {
	result := psql.Mydb.Model(&models.ObjectModel{}).Where("id = ?", object_model.Id).Updates(&object_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*ObjectModelService) DeleteObjectModel(object_model models.ObjectModel) bool {
	result := psql.Mydb.Delete(&object_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

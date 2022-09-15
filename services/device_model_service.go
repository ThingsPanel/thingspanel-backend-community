package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strconv"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type DeviceModelService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*DeviceModelService) GetDeviceModelList(PaginationValidate valid.DeviceModelPaginationValidate) (bool, []models.DeviceModel, int64) {
	var DeviceModels []models.DeviceModel
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	if PaginationValidate.Issued != 0 {
		sqlWhere += " and issued = " + strconv.Itoa(PaginationValidate.Issued)
	}
	if PaginationValidate.ModelType != "" {
		sqlWhere += " and model_type = '" + PaginationValidate.ModelType + "'"
	}
	if PaginationValidate.Flag != 0 {
		sqlWhere += " and flag = " + strconv.Itoa(PaginationValidate.Flag)
	}
	var count int64
	psql.Mydb.Model(&models.DeviceModel{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.DeviceModel{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&DeviceModels)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, DeviceModels, 0
	}
	return true, DeviceModels, count
}

// 新增数据
func (*DeviceModelService) AddDeviceModel(device_model models.DeviceModel) (bool, models.DeviceModel) {
	var uuid = uuid.GetUuid()
	device_model.ID = uuid
	result := psql.Mydb.Create(&device_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, device_model
	}
	return true, device_model
}

// 修改数据
func (*DeviceModelService) EditDeviceModel(device_model models.DeviceModel) bool {
	result := psql.Mydb.Updates(&device_model)
	//result := psql.Mydb.Save(&device_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*DeviceModelService) DeleteDeviceModel(device_model models.DeviceModel) bool {
	result := psql.Mydb.Delete(&device_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

type DeviceModelTree struct {
	DictValue   string               `json:"dict_value"`
	Describe    string               `json:"describe"`
	DeviceModel []models.DeviceModel `json:"device_model"`
}

// 插件树
func (*DeviceModelService) DeviceModelTree() []DeviceModelTree {
	var trees []DeviceModelTree
	var tp_dict []models.TpDict
	logs.Info("------------------------------")
	result := psql.Mydb.Where("dict_code = 'chart_type'").Find(&tp_dict)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return trees
	}
	for _, dict := range tp_dict {
		var tree DeviceModelTree
		var device_model []models.DeviceModel
		result := psql.Mydb.Where("model_type = ?", dict.DictValue).Find(&device_model)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
			return trees
		}
		tree.DictValue = dict.DictValue
		tree.Describe = dict.Describe
		tree.DeviceModel = device_model
		trees = append(trees, tree)
	}
	return trees
}

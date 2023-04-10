package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strconv"

	"github.com/beego/beego/v2/core/logs"
	"github.com/bitly/go-simplejson"
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

func (*DeviceModelService) GetDeviceModelDetail(device_model_id string) []models.DeviceModel {
	var deviceModel []models.DeviceModel
	psql.Mydb.First(&deviceModel, "id = ?", device_model_id)
	return deviceModel
}

// 获取列表
func (*DeviceModelService) GetDeviceModelList(PaginationValidate valid.DeviceModelPaginationValidate, tenantId string) (bool, []models.DeviceModel, int64) {
	var DeviceModels []models.DeviceModel
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.DeviceModel{})
	db.Where("tenant_id = ?", tenantId)
	if PaginationValidate.Issued != 0 {
		db.Where("issued = ?", strconv.Itoa(PaginationValidate.Issued))
	}
	if PaginationValidate.ModelType != "" {
		db.Where("model_type = ?", PaginationValidate.ModelType)
	}
	if PaginationValidate.Flag != 0 {
		db.Where("flag = ?", strconv.Itoa(PaginationValidate.Flag))
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&DeviceModels)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, DeviceModels, 0
	}
	return true, DeviceModels, count
}

// 新增数据
func (*DeviceModelService) AddDeviceModel(device_model models.DeviceModel) (bool, models.DeviceModel) {
	result := psql.Mydb.Create(&device_model)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, device_model
	}
	return true, device_model
}

// 修改数据
func (*DeviceModelService) EditDeviceModel(device_model valid.DeviceModelValidate, tenantId string) bool {
	result := psql.Mydb.Model(&models.DeviceModel{}).Where("id = ? and tenant_id = ?", device_model.Id, tenantId).Updates(&device_model)
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
	ModelName   string               `json:"model_name"`
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
		for i := range device_model {
			device_model[i].ChartData = ""
		}
		tree.DictValue = dict.DictValue
		tree.ModelName = dict.Describe
		tree.DeviceModel = device_model
		trees = append(trees, tree)
	}
	return trees
}

//根据设备插件id获取物模型属性
func (*DeviceModelService) GetModelByPluginId(pluginId string) ([]interface{}, error) {
	var model []interface{}
	var deviceModel models.DeviceModel
	psql.Mydb.First(&deviceModel, "id = ?", pluginId)
	chartDate, err := simplejson.NewJson([]byte(deviceModel.ChartData))
	if err != nil {
		return model, err
	} else {
		if value, err := chartDate.Get("tsl").Get("properties").Array(); err != nil {
			return model, err
		} else {
			model = value
		}
	}
	return model, nil
}

//根据设备插件id获取物模型属性的类型map
func (*DeviceModelService) GetTypeMapByPluginId(pluginId string) (map[string]interface{}, error) {
	var typeMap = make(map[string]interface{})
	var DeviceModelService DeviceModelService
	modelList, err := DeviceModelService.GetModelByPluginId(pluginId)
	if err != nil {
		return typeMap, err
	}
	for _, attribute := range modelList {
		if attributeMap, ok := attribute.(map[string]interface{}); ok {
			if name, ok := attributeMap["name"].(string); ok {
				typeMap[name] = attributeMap["dataType"].(string)
			}
		}
	}
	return typeMap, nil
}

//根据设备插件id获取设备图表单元名称
func (*DeviceModelService) GetChartNameListByPluginId(pluginId string) ([]string, error) {
	var chartNameMap []string
	var deviceModel models.DeviceModel
	psql.Mydb.First(&deviceModel, "id = ?", pluginId)
	chartDate, err := simplejson.NewJson([]byte(deviceModel.ChartData))
	if err != nil {
		return nil, err
	} else {
		if value, err := chartDate.Get("chart").Array(); err != nil {
			return nil, err
		} else {
			for _, charts := range value {
				if chartMap, ok := charts.(map[string]interface{}); ok {
					if _, ok := chartMap["name"].(string); ok {
						chartNameMap = append(chartNameMap, chartMap["name"].(string))
					}
				} else {
					logs.Error("chart属性转map失败")
				}
			}
		}
	}
	return chartNameMap, nil
}

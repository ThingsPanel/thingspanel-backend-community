package services

import (
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"ThingsPanel-Go/initialize/psql"

	"gorm.io/gorm"
)

type FieldMappingService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 根据device_id删除一条field_mapping数据
func (*FieldMappingService) DeleteByDeviceId(device_id string) bool {
	result := psql.Mydb.Where("device_id = ?", device_id).Delete(&models.FieldMapping{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// Add新增一条field_mapping数据
func (*FieldMappingService) Add(device_id string, field_from string, field_to string) (bool, string) {
	var uuid = uuid.GetUuid()
	fieldMapping := models.FieldMapping{
		ID:        uuid,
		DeviceID:  device_id,
		FieldFrom: field_from,
		FieldTo:   field_to,
	}
	result := psql.Mydb.Create(&fieldMapping)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 获取全部FieldMapping
func (*FieldMappingService) GetByDeviceid(device_id string) ([]models.FieldMapping, int64) {
	var fieldMappings []models.FieldMapping
	result := psql.Mydb.Where("device_id = ?", device_id).Find(&fieldMappings)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(fieldMappings) == 0 {
		fieldMappings = []models.FieldMapping{}
	}
	return fieldMappings, result.RowsAffected
}

// 根据ID删除一条FieldMapping数据
func (*FieldMappingService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.FieldMapping{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 转换FieldMapping
func (*FieldMappingService) TransformByDeviceid(device_id string, field_to string) string {
	var fieldMappings models.FieldMapping
	var field_from string
	result := psql.Mydb.Where("device_id = ? AND field_to = ?", device_id, field_to).Find(&fieldMappings)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if result.RowsAffected == 0 {
		field_from = ""
	} else {
		field_from = fieldMappings.FieldFrom
	}
	return field_from
}

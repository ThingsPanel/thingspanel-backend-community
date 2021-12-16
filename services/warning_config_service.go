package services

import (
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"fmt"

	"ThingsPanel-Go/initialize/psql"

	"gorm.io/gorm"
)

type WarningConfigService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// GetWarningConfigById 根据id获取一条warningConfig数据
func (*WarningConfigService) GetWarningConfigById(id string) (*models.WarningConfig, int64) {
	var warningConfig models.WarningConfig
	result := psql.Mydb.Where("id = ?", id).First(&warningConfig)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &warningConfig, result.RowsAffected
}

// Paginate 分页获取warningConfig数据
func (*WarningConfigService) Paginate(wid string, offset int, pageSize int) ([]models.WarningConfig, int64) {
	var warningConfigs []models.WarningConfig
	var count int64
	result := psql.Mydb.Model(&models.WarningConfig{}).Where("wid = ?", wid).Limit(pageSize).Offset(offset).Find(&warningConfigs)
	psql.Mydb.Model(&models.WarningConfig{}).Where("wid = ?", wid).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(warningConfigs) == 0 {
		warningConfigs = []models.WarningConfig{}
	}
	return warningConfigs, count
}

// Add新增一条warningConfig数据
func (*WarningConfigService) Add(wid string, name string, describe string, config string, message string, bid string, sensor string, customer_id string) (bool, string) {
	var uuid = uuid.GetUuid()
	warningConfig := models.WarningConfig{
		ID:         uuid,
		Wid:        wid,
		Name:       name,
		Describe:   describe,
		Config:     config,
		Message:    message,
		Bid:        bid,
		Sensor:     sensor,
		CustomerID: customer_id,
	}
	result := psql.Mydb.Create(&warningConfig)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 根据ID编辑一条warningConfig数据
func (*WarningConfigService) Edit(id string, wid string, name string, describe string, config string, message string, bid string, sensor string, customer_id string) bool {
	// updated_at
	result := psql.Mydb.Model(&models.WarningConfig{}).Where("id = ?", id).Updates(map[string]interface{}{"wid": wid, "name": name, "describe": describe, "config": config, "message": message, "bid": bid, "sensor": sensor, "customer_id": customer_id})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID删除一条warningConfig数据
func (*WarningConfigService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.WarningConfig{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// GetWarningConfigById 根据id获取一条warningConfig数据
func (*WarningConfigService) GetWarningConfigByWidAndBid(wid string, bid string) (*models.WarningConfig, int64) {
	var warningConfig models.WarningConfig
	result := psql.Mydb.Where("wid = ? AND bid = ?", wid, bid).First(&warningConfig)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &warningConfig, result.RowsAffected
}

// GetWarningConfigsByDeviceId 根据id获取多条warningConfig数据
func (*WarningConfigService) WarningConfigCheck(bid string, values map[string]interface{}) {
	var warningConfigs []models.WarningConfig
	var count int64
	result := psql.Mydb.Model(&models.WarningConfig{}).Where("bid = ?", bid).Find(&warningConfigs)
	psql.Mydb.Model(&models.WarningConfig{}).Where("bid = ?", bid).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if count > 0 {
		//var FieldMappingService FieldMappingService
		for _, wv := range warningConfigs {
			fmt.Println(wv)
		}
	}
}

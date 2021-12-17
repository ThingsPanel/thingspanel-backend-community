package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"gorm.io/gorm"
)

type DeviceService struct {
}

// Token 获取设备token
func (*DeviceService) Token(id string) (*models.Device, int64) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &device, result.RowsAffected
}

// GetDevicesByAssetID 获取设备列表
func (*DeviceService) GetDevicesByAssetID(asset_id string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&models.Device{}).Where("asset_id = ?", asset_id).Find(&devices)
	psql.Mydb.Model(&models.Device{}).Where("asset_id = ?", asset_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
}

// GetDevicesByAssetIDs 获取设备列表
func (*DeviceService) GetDevicesByAssetIDs(asset_ids []string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&models.Device{}).Where("asset_id IN ?", asset_ids).Find(&devices)
	psql.Mydb.Model(&models.Device{}).Where("asset_id IN ?", asset_ids).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
}

// GetAllDevicesByID 获取所有设备
func (*DeviceService) GetAllDeviceByID(id string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Find(&devices)
	psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
}

// GetDevicesByID 获取设备
func (*DeviceService) GetDeviceByID(id string) (*models.Device, int64) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &device, result.RowsAffected
}

// Delete 根据ID删除Device
func (*DeviceService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Device{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 获取全部Device
func (*DeviceService) All() ([]models.Device, int64) {
	var devices []models.Device
	result := psql.Mydb.Find(&devices)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, result.RowsAffected
}

// 根据ID编辑Device的Token
func (*DeviceService) Edit(id string, token string, protocol string, port string, publish string, subscribe string, username string, password string) bool {
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Updates(map[string]interface{}{
		"token":     token,
		"protocol":  protocol,
		"port":      port,
		"publish":   publish,
		"subscribe": subscribe,
		"username":  username,
		"password":  password,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

func (*DeviceService) Add(token string, protocol string, port string, publish string, subscribe string, username string, password string) (bool, string) {
	var uuid = uuid.GetUuid()
	device := models.Device{
		Token:     token,
		Protocol:  protocol,
		Port:      port,
		Publish:   publish,
		Subscribe: subscribe,
		Username:  username,
		Password:  password,
	}
	result := psql.Mydb.Create(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"log"

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

// GetDevicesByBusinessID 根据业务ID获取设备列表
// return []设备,设备数量
// 2022-04-18新增
func (*DeviceService) GetDevicesByBusinessID(business_id string) ([]models.Device, int64) {
	var devices []models.Device
	SQL := `select device.id,device.asset_id ,device.additional_info,device."type" ,device."location",device."d_id",device."name",device."label",device.protocol from device left join asset on device.asset_id  = asset.id where asset.business_id =?`
	if err := psql.Mydb.Raw(SQL, business_id).Scan(&devices).Error; err != nil {
		log.Println(err.Error())
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, int64(len(devices))
}

// GetDevicesByAssetIDs 获取设备列表
func (*DeviceService) GetDevicesByAssetIDs(asset_ids []string) (devices []models.Device, err error) {
	err = psql.Mydb.Model(&models.Device{}).Where("asset_id IN ?", asset_ids).Find(&devices).Error
	if err != nil {
		return devices, err
	}
	return devices, nil
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
	var count int64
	result := psql.Mydb.Model(&devices).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
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

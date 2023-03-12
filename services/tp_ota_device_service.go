package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpOtaDeviceService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}
type DeviceStatusCount struct {
	UpgradeStatus string `json:"upgrade_status,omitempty" alias:"状态"`
	Count         int    `json:"count" alias:"数量"`
}

func (*TpOtaDeviceService) GetTpOtaDeviceList(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []models.TpOtaDevice, int64) {
	var TpOtaDevices []models.TpOtaDevice
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	if PaginationValidate.DeviceId != "" {
		db.Where("device_id like ?", "%"+PaginationValidate.DeviceId+"%")
	}
	if PaginationValidate.UpgradeStatus != "" {
		db.Where("upgrade_status =?", PaginationValidate.UpgradeStatus)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Find(&TpOtaDevices)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpOtaDevices, 0
	}
	return true, TpOtaDevices, count
}

func (*TpOtaDeviceService) GetTpOtaDeviceStatusCount(PaginationValidate valid.TpOtaDevicePaginationValidate) (bool, []DeviceStatusCount) {
	StatusCount := make([]DeviceStatusCount, 0)
	db := psql.Mydb.Model(&models.TpOtaDevice{})
	re := db.Select("upgrade_status as upgrade_status,count(*) as count").Where("remark = ? ", "ccc").Group("upgrade_status").Scan(&StatusCount)
	if re.Error != nil {
		return false, StatusCount
	}
	return true, StatusCount

}

// 新增数据
func (*TpOtaDeviceService) AddTpOtaDevice(tp_ota_device models.TpOtaDevice) (models.TpOtaDevice, error) {
	result := psql.Mydb.Create(&tp_ota_device)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_ota_device, result.Error
	}
	return tp_ota_device, nil
}
func (*TpOtaDeviceService) DeleteTpOtaDevice(tp_ota_device models.TpOtaDevice) error {
	result := psql.Mydb.Delete(&tp_ota_device)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

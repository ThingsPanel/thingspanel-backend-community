package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"

	"github.com/beego/beego/v2/core/logs"
)

type DeviceEvnetHistory struct {
}

func (*DeviceEvnetHistory) GetDeviceEvnetHistoryListByDeviceId(
	offset int, pageSize int, deviceId string) ([]models.DeviceEvnetHistory, int64) {

	var evnetHistroy []models.DeviceEvnetHistory
	var count int64

	tx := psql.Mydb.Model(&models.DeviceEvnetHistory{})
	tx.Where("device_id = ?", deviceId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return evnetHistroy, count
	}

	err = tx.Order("report_time desc").Limit(pageSize).Offset(offset).Find(&evnetHistroy).Error
	if err != nil {
		logs.Error(err.Error())
		return evnetHistroy, count
	}
	return evnetHistroy, count
}

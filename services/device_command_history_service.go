package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"

	"github.com/beego/beego/v2/core/logs"
)

type DeviceCommandHistory struct {
}

func (*DeviceCommandHistory) GetDeviceCommandHistoryListByDeviceId(
	offset int, pageSize int, deviceId string) ([]models.DeviceCommandHistory, int64) {

	var commandHistroy []models.DeviceCommandHistory
	var count int64

	tx := psql.Mydb.Model(&models.DeviceCommandHistory{})
	tx.Where("device_id = ?", deviceId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return commandHistroy, count
	}

	err = tx.Order("send_time desc").Limit(pageSize).Offset(offset).Find(&commandHistroy).Error
	if err != nil {
		logs.Error(err.Error())
		return commandHistroy, count
	}
	return commandHistroy, count
}

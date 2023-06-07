package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"

	"github.com/beego/beego/v2/core/logs"
)

type TpNotificationService struct {
}

func SaveNotification(ng models.TpNotificationGroups, nm []models.TpNotificationMembers) bool {

	result := psql.Mydb.Save(ng)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	delres := DeleteNotificationMembers(ng.Id)

	if !delres {
		return false
	}

	result = psql.Mydb.Create(&nm)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	return true
}

func GetNotificationGroupsStatus(id string) int {
	var res models.TpNotificationGroups
	result := psql.Mydb.Where("id = ?", id).Find(&res)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return -1
	}
	return res.Status
}

func GetNotificationGroups(id string) models.TpNotificationGroups {
	var res models.TpNotificationGroups
	result := psql.Mydb.Where("id = ?", id).Find(&res)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return res
	}
	return res
}

func GetNotificationMenbers(id string) []models.TpNotificationMembers {
	var res []models.TpNotificationMembers
	tx := psql.Mydb.Model(&models.TpNotificationMembers{})
	err := tx.Where("tp_notification_groups_id = ?", id).Find(&res).Error
	if err != nil {
		logs.Error(err.Error())
		return res
	}
	return res
}

func UpdateNotificationGroupsStatus(id string, s int) bool {
	tx := psql.Mydb.Model(&models.TpNotificationGroups{})
	err := tx.Where("id = ?", id).Update("status", s).Error
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	return true
}

func DeleteNotificationGroups(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.TpNotificationGroups{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

func DeleteNotificationMembers(id string) bool {
	if len(id) > 0 {
		result := psql.Mydb.Where("tp_notification_groups_id = ?", id).Delete(&models.TpNotificationMembers{})
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return false
		}
	}
	return true
}

func GetNotificationListByTenantId(
	offset int, pageSize int, tenantId string) ([]models.TpNotificationGroups, int64) {

	var nG []models.TpNotificationGroups
	var count int64

	tx := psql.Mydb.Model(&models.TpNotificationGroups{})
	tx.Where("tenant_id = ?", tenantId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return nG, count
	}

	err = tx.Limit(pageSize).Offset(offset).Find(&nG).Error
	if err != nil {
		logs.Error(err.Error())
		return nG, count
	}
	return nG, count
}

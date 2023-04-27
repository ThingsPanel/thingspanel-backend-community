package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpDataTranspondService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 新建转发规则
func (*TpDataTranspondService) AddTpDataTranspond(
	dataTranspond models.TpDataTranspon,
	dataTranspondDetail []models.TpDataTransponDetail,
	dataTranspondTarget []models.TpDataTransponTarget,
) bool {

	err := psql.Mydb.Create(&dataTranspond)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	err = psql.Mydb.Create(&dataTranspondDetail)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	err = psql.Mydb.Create(&dataTranspondTarget)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
		return false
	}

	return true
}

func (*TpDataTranspondService) GetListByTenantId(
	offset int, pageSize int, tenantId string) ([]models.TpDataTranspon, int64) {

	var dataTranspon []models.TpDataTranspon
	var count int64

	tx := psql.Mydb.Model(&models.TpDataTranspon{})
	tx.Where("tenant_id = ?", tenantId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspon, count
	}

	err = tx.Order("create_time desc").Limit(pageSize).Offset(offset).Find(&dataTranspon).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspon, count
	}
	return dataTranspon, count
}

// 根据 dataTranspondId 查找 tp_data_transpond 表
func (*TpDataTranspondService) GetDataTranspondByDataTranspondId(dataTranspondId string) (models.TpDataTranspon, bool) {
	var dataTranspon models.TpDataTranspon
	tx := psql.Mydb.Model(&models.TpDataTranspon{})
	err := tx.Where("id = ?", dataTranspondId).Find(&dataTranspon).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspon, false
	}
	return dataTranspon, true
}

// 根据 dataTranspondId 查找 tp_data_transpond_detail 表
func (*TpDataTranspondService) GetDataTranspondDetailByDataTranspondId(dataTranspondId string) ([]models.TpDataTransponDetail, bool) {
	var dataTranspondDetail []models.TpDataTransponDetail
	tx := psql.Mydb.Model(&models.TpDataTransponDetail{})
	err := tx.Where("data_transpond_id = ?", dataTranspondId).Omit("id").Find(&dataTranspondDetail).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspondDetail, false
	}
	return dataTranspondDetail, true
}

// 根据 dataTranspondId 查找 tp_data_transpond_target 表
func (*TpDataTranspondService) GetDataTranspondTargetByDataTranspondId(dataTranspondId string) ([]models.TpDataTransponTarget, bool) {
	var dataTranspondTarget []models.TpDataTransponTarget
	tx := psql.Mydb.Model(&models.TpDataTransponTarget{})
	err := tx.Where("data_transpond_id = ?", dataTranspondId).Omit("id").Find(&dataTranspondTarget).Error
	if err != nil {
		logs.Error(err.Error())
		return dataTranspondTarget, false
	}
	return dataTranspondTarget, true
}

func (*TpDataTranspondService) UpdateDataTranspondStatusByDataTranspondId(dataTranspondId string, swtich int) bool {
	tx := psql.Mydb.Model(&models.TpDataTranspon{})
	err := tx.Where("id = ?", dataTranspondId).Update("status", swtich).Error
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	return true
}

func (*TpDataTranspondService) DeletaByDataTranspondId(dataTranspondId string) bool {

	result := psql.Mydb.Where("id = ?", dataTranspondId).Delete(&models.TpDataTranspon{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	result = psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Delete(&models.TpDataTransponDetail{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	result = psql.Mydb.Where("data_transpond_id = ?", dataTranspondId).Delete(&models.TpDataTransponTarget{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	return true
}

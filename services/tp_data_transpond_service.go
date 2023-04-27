package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"

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

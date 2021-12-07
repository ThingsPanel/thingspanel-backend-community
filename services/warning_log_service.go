package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"

	"gorm.io/gorm"
)

type WarningLogService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// Paginate 分页获取WarningLog数据
func (*WarningLogService) Paginate(name string, offset int, pageSize int) ([]models.WarningLog, int64) {
	var warningLogs []models.WarningLog
	result := psql.Mydb.Limit(pageSize).Offset(offset).Find(&warningLogs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return warningLogs, result.RowsAffected
}

// 根据id获取100条WarningLog数据
func (*WarningLogService) GetList(offset int, pageSize int) ([]models.WarningLog, int64) {
	var warningLogs []models.WarningLog
	result := psql.Mydb.Limit(pageSize).Offset(offset).Find(&warningLogs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return warningLogs, result.RowsAffected
}

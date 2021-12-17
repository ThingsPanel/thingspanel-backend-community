package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"time"

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
	if len(warningLogs) == 0 {
		warningLogs = []models.WarningLog{}
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
	if len(warningLogs) == 0 {
		warningLogs = []models.WarningLog{}
	}
	return warningLogs, result.RowsAffected
}

// Add新增一条WarningLogService数据
func (*WarningLogService) Add(t string, describe string, data_id string) (bool, string) {
	var uuid = uuid.GetUuid()
	warningLog := models.WarningLog{
		ID:        uuid,
		Type:      t,
		Describe:  describe,
		DataID:    data_id,
		CreatedAt: time.Now().Unix(),
	}
	result := psql.Mydb.Create(&warningLog)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"

	"gorm.io/gorm"
)

type OperationLogService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// Paginate 分页获取OperationLog数据
func (*OperationLogService) Paginate(offset int, pageSize int, ip string, path string) ([]models.OperationLog, int64) {
	var operationLogs []models.OperationLog
	sqlWhere := "1=1"
	if path != "" {
		sqlWhere += " and (detailed ::json->>'path' like '%" + path + "%')"
	}
	if ip != "" {
		sqlWhere += " and (detailed ::json->>'ip' like '%" + ip + "%')"
	}
	var count int64
	countResult := psql.Mydb.Model(&operationLogs).Where(sqlWhere).Count(&count)
	if countResult.Error != nil {
		errors.Is(countResult.Error, gorm.ErrRecordNotFound)
	}
	offset = pageSize * (offset - 1)
	result := psql.Mydb.Where(sqlWhere).Order("created_at desc").Limit(pageSize).Offset(offset).Find(&operationLogs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(operationLogs) == 0 {
		operationLogs = []models.OperationLog{}
	}
	return operationLogs, count
}

// 根据id获取100条OperationLog数据
func (*OperationLogService) List(offset int, pageSize int) ([]models.OperationLog, int64) {
	var operationLogs []models.OperationLog
	result := psql.Mydb.Order("created_at desc").Limit(pageSize).Offset(offset).Find(&operationLogs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(operationLogs) == 0 {
		operationLogs = []models.OperationLog{}
	}
	return operationLogs, result.RowsAffected
}

package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpAutomationLogService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*TpAutomationLogService) GetTpAutomationLogList(PaginationValidate valid.TpAutomationLogPaginationValidate) ([]models.TpAutomationLog, int64, error) {
	var TpAutomationLogs []models.TpAutomationLog
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	var paramList []interface{}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = ?"
		paramList = append(paramList, PaginationValidate.Id)
	}
	if PaginationValidate.ProcessResult != "" {
		sqlWhere += " and process_result = ?"
		paramList = append(paramList, PaginationValidate.ProcessResult)
	}
	var count int64
	psql.Mydb.Model(&models.TpAutomationLog{}).Where(sqlWhere, paramList...).Count(&count)
	result := psql.Mydb.Model(&models.TpAutomationLog{}).Where(sqlWhere, paramList...).Limit(PaginationValidate.PerPage).Offset(offset).Order("trigger_time desc").Find(&TpAutomationLogs)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return TpAutomationLogs, 0, result.Error
	}
	return TpAutomationLogs, count, nil
}

// 新增数据
func (*TpAutomationLogService) AddTpAutomationLog(tp_warning_information models.TpAutomationLog) (models.TpAutomationLog, error) {
	var uuid = uuid.GetUuid()
	tp_warning_information.Id = uuid
	result := psql.Mydb.Create(&tp_warning_information)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_warning_information, result.Error
	}
	return tp_warning_information, nil
}

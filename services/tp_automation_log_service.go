package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
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
	if PaginationValidate.AutomationId != "" {
		sqlWhere += " and automation_id = ?"
		paramList = append(paramList, PaginationValidate.AutomationId)
	}
	var count int64
	psql.Mydb.Model(&models.TpAutomationLog{}).Where(sqlWhere, paramList...).Count(&count)
	result := psql.Mydb.Model(&models.TpAutomationLog{}).Where(sqlWhere, paramList...).Limit(PaginationValidate.PerPage).Offset(offset).Order("trigger_time desc").Find(&TpAutomationLogs)
	if result.Error != nil {
		logs.Error(result.Error)
		return TpAutomationLogs, 0, result.Error
	}
	return TpAutomationLogs, count, nil
}

// 新增数据
func (*TpAutomationLogService) AddTpAutomationLog(automationLog models.TpAutomationLog) (models.TpAutomationLog, error) {
	var uuid = uuid.GetUuid()
	automationLog.Id = uuid
	result := psql.Mydb.Create(&automationLog)
	if result.Error != nil {
		logs.Error(result.Error)
		return automationLog, result.Error
	}
	return automationLog, nil
}

// 更新数据
func (*TpAutomationLogService) UpdateTpAutomationLog(automationLogMap map[string]interface{}) error {
	logs.Error(automationLogMap)
	result := psql.Mydb.Model(&models.TpAutomationLog{}).Where("id = ?", automationLogMap["Id"]).Updates(&automationLogMap)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

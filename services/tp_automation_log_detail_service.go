package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpAutomationLogDetailService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*TpAutomationLogDetailService) GetTpAutomationLogDetailList(PaginationValidate valid.TpAutomationLogDetailPaginationValidate) ([]map[string]interface{}, int64, error) {
	var TpAutomationLogDetails []map[string]interface{}
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
	if PaginationValidate.AutomationLogId != "" {
		sqlWhere += " and automation_log_id = ?"
		paramList = append(paramList, PaginationValidate.AutomationLogId)
	}
	var count int64
	psql.Mydb.Model(&models.TpAutomationLogDetail{}).Where(sqlWhere, paramList...).Count(&count)
	result := psql.Mydb.Model(&models.TpAutomationLogDetail{}).Where(sqlWhere, paramList...).Limit(PaginationValidate.PerPage).Offset(offset).Find(&TpAutomationLogDetails)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return TpAutomationLogDetails, 0, result.Error
	}
	for i, automationLogDetail := range TpAutomationLogDetails {
		if automationLogDetail["action_type"] == "1" {
			var device models.Device
			result := psql.Mydb.Model(&models.Device{}).Where("id = ?", automationLogDetail["target_id"]).First(&device)
			if result.Error == nil {
				TpAutomationLogDetails[i]["target_name"] = device.Name
			}
		} else if automationLogDetail["action_type"] == "2" {
			var warningStrategy models.TpWarningStrategy
			result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", automationLogDetail["target_id"]).First(&warningStrategy)
			if result.Error == nil {
				TpAutomationLogDetails[i]["target_name"] = warningStrategy.WarningStrategyName
			}
		} else if automationLogDetail["action_type"] == "3" {
			var scenarioStrategy models.TpScenarioStrategy
			result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", automationLogDetail["target_id"]).First(&scenarioStrategy)
			if result.Error == nil {
				TpAutomationLogDetails[i]["target_name"] = scenarioStrategy.ScenarioName
			}
		}
	}
	return TpAutomationLogDetails, count, nil
}

// 新增数据
func (*TpAutomationLogDetailService) AddTpAutomationLogDetail(automationLogDetail models.TpAutomationLogDetail) (models.TpAutomationLogDetail, error) {
	var uuid = uuid.GetUuid()
	automationLogDetail.Id = uuid
	result := psql.Mydb.Create(&automationLogDetail)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return automationLogDetail, result.Error
	}
	return automationLogDetail, nil
}

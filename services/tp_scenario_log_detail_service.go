package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpScenarioLogDetailService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*TpScenarioLogDetailService) GetTpScenarioLogDetailList(PaginationValidate valid.TpScenarioLogDetailPaginationValidate) ([]map[string]interface{}, int64, error) {
	var TpScenarioLogDetails []map[string]interface{}
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
	if PaginationValidate.ScenarioLogId != "" {
		sqlWhere += " and scenario_log_id = ?"
		paramList = append(paramList, PaginationValidate.ScenarioLogId)
	}
	var count int64
	psql.Mydb.Model(&models.TpScenarioLogDetail{}).Where(sqlWhere, paramList...).Count(&count)
	result := psql.Mydb.Model(&models.TpScenarioLogDetail{}).Where(sqlWhere, paramList...).Limit(PaginationValidate.PerPage).Offset(offset).Find(&TpScenarioLogDetails)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return TpScenarioLogDetails, 0, result.Error
	}
	for i, scenarioLogDetail := range TpScenarioLogDetails {
		if scenarioLogDetail["action_type"] == "1" {
			var device models.Device
			result := psql.Mydb.Model(&models.Device{}).Where("id = ?", scenarioLogDetail["target_id"]).First(&device)
			if result.Error == nil {
				TpScenarioLogDetails[i]["target_name"] = device.Name
			}
		} else if scenarioLogDetail["action_type"] == "2" {
			var warningStrategy models.TpWarningStrategy
			result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", scenarioLogDetail["target_id"]).First(&warningStrategy)
			if result.Error == nil {
				TpScenarioLogDetails[i]["target_name"] = warningStrategy.WarningStrategyName
			}
		} else if scenarioLogDetail["action_type"] == "3" {
			var scenarioStrategy models.TpScenarioStrategy
			result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", scenarioLogDetail["target_id"]).First(&scenarioStrategy)
			if result.Error == nil {
				TpScenarioLogDetails[i]["target_name"] = scenarioStrategy.ScenarioName
			}
		}
	}
	return TpScenarioLogDetails, count, nil
}

// 新增数据
func (*TpScenarioLogDetailService) AddTpScenarioLogDetail(tp_warning_information models.TpScenarioLogDetail) (models.TpScenarioLogDetail, error) {
	var uuid = uuid.GetUuid()
	tp_warning_information.Id = uuid
	result := psql.Mydb.Create(&tp_warning_information)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_warning_information, result.Error
	}
	return tp_warning_information, nil
}

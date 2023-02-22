package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpScenarioActionService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpScenarioActionService) GetTpScenarioActionDetail(tp_automation_action_id string) (models.TpScenarioAction, error) {
	var tp_automation_action models.TpScenarioAction
	result := psql.Mydb.First(&tp_automation_action, "id = ?", tp_automation_action_id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return tp_automation_action, nil
		} else {
			return tp_automation_action, result.Error
		}

	}
	return tp_automation_action, nil
}

// 获取列表
func (*TpScenarioActionService) GetTpScenarioActionList(PaginationValidate valid.TpScenarioActionPaginationValidate) (bool, []models.TpScenarioAction, int64) {
	var TpScenarioActions []models.TpScenarioAction
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"

	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}

	var count int64
	psql.Mydb.Model(&models.TpScenarioAction{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpScenarioAction{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpScenarioActions)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpScenarioActions, 0
	}
	return true, TpScenarioActions, count
}

// 新增数据
func (*TpScenarioActionService) AddTpScenarioAction(tp_automation_action models.TpScenarioAction) (models.TpScenarioAction, error) {
	var uuid = uuid.GetUuid()
	tp_automation_action.Id = uuid
	result := psql.Mydb.Create(&tp_automation_action)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_automation_action, result.Error
	}
	return tp_automation_action, nil
}

// 修改数据
func (*TpScenarioActionService) EditTpScenarioAction(tp_automation_action valid.EditTpScenarioActionValidate) bool {
	result := psql.Mydb.Model(&models.TpScenarioAction{}).Where("id = ?", tp_automation_action.Id).Updates(&tp_automation_action)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpScenarioActionService) DeleteTpScenarioAction(tp_automation_action models.TpScenarioAction) error {
	result := psql.Mydb.Delete(&tp_automation_action)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

// 触发场景动作
func (*TpScenarioActionService) ExecuteScenarioAction(scenarioStrategyId string) error {
	var scenarioActions []models.TpScenarioAction
	result := psql.Mydb.Model(&models.TpScenarioAction{}).Where("scenario_strategy_id = ?", scenarioStrategyId).Find(&scenarioActions)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	var scenarioLog models.TpScenarioLog
	scenarioLog.ScenarioStrategyId = scenarioStrategyId
	scenarioLog.TriggerTime = time.Now().Format("2006-01-02 15:04:05")
	scenarioLog.ProcessResult = "2"
	var scenarioLogService TpScenarioLogService
	scenarioLog, err := scenarioLogService.AddTpScenarioLog(scenarioLog)
	if err != nil {
		logs.Error(result.Error)
		return result.Error
	}
	scenarioLog.ProcessDescription = "执行成功"
	scenarioLog.ProcessResult = "1"
	for _, scenarioAction := range scenarioActions {
		var scenarioLogDetail models.TpScenarioLogDetail
		if scenarioAction.ActionType == "1" {
			scenarioLogDetail.TargetId = scenarioAction.DeviceId
			//设备输出
			if scenarioAction.DeviceModel == "1" {
				//属性
				instructMap := make(map[string]interface{})
				err := json.Unmarshal([]byte(scenarioAction.Instruct), &instructMap)
				if err != nil {
					scenarioLogDetail.ProcessDescription = "instruct:" + err.Error()
					scenarioLogDetail.ProcessResult = "2"
				} else {
					for k, v := range instructMap {
						var deviceService DeviceService
						var conditionsLog models.ConditionsLog
						err := deviceService.OperatingDevice(scenarioAction.DeviceId, k, v)
						if err == nil {
							conditionsLog.SendResult = "1"
							scenarioLogDetail.ProcessResult = "1"
							scenarioLogDetail.ProcessDescription = "指令为:" + scenarioAction.Instruct
						} else {
							conditionsLog.SendResult = "2"
							scenarioLogDetail.ProcessResult = "2"
							scenarioLogDetail.ProcessDescription = err.Error()
						}
						//记录发送指令日志
						var conditionsLogService ConditionsLogService
						conditionsLog.DeviceId = scenarioAction.DeviceId
						conditionsLog.OperationType = "3"
						conditionsLog.ProtocolType = "mqtt"
						conditionsLog.Instruct = scenarioAction.Instruct
						conditionsLogService.Insert(&conditionsLog)
					}
				}

			} else if scenarioAction.ActionType == "2" {
				scenarioLogDetail.ProcessDescription = "暂不支持调动服务;"
				scenarioLogDetail.ProcessResult = "2"
			} else {
				scenarioLogDetail.ProcessDescription = "deviceModel错误;"
				scenarioLogDetail.ProcessResult = "2"
			}
			//记录日志
			scenarioLogDetail.ScenarioLogId = scenarioLog.Id
			var scenarioLogDetailService TpScenarioLogDetailService
			_, err := scenarioLogDetailService.AddTpScenarioLogDetail(scenarioLogDetail)
			if err != nil {
				logs.Error(result.Error)
			}
			if scenarioLogDetail.ProcessResult == "2" {
				scenarioLog.ProcessDescription = "执行中有失败，请查看日志详情"
				scenarioLog.ProcessResult = "2"
			}
		}
	}
	_, err = scenarioLogService.UpdateTpScenarioLog(scenarioLog)
	if err != nil {
		logs.Error(result.Error)
		return err
	}
	if scenarioLog.ProcessResult == "2" {
		return errors.New(scenarioLog.ProcessDescription)
	}
	return nil
}

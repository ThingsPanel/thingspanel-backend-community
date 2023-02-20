package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/fatih/structs"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cast"
	"gorm.io/gorm"

	tp_cron "ThingsPanel-Go/initialize/cron"
)

type TpAutomationService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpAutomationService) GetTpAutomationDetail(tp_automation_id string) (map[string]interface{}, error) {
	var tp_automation = make(map[string]interface{})
	result := psql.Mydb.Model(&models.TpAutomation{}).Where("id = ?", tp_automation_id).First(&tp_automation)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return tp_automation, nil
		} else {
			return tp_automation, result.Error
		}

	}
	// 自动化条件
	var tp_automation_conditions []map[string]interface{}
	result = psql.Mydb.Table("tp_automation_condition").
		Select("tp_automation_condition.*,device.asset_id,asset.business_id").
		Joins("left join device on tp_automation_condition.device_id = device.id").
		Joins("left join asset on device.asset_id = asset.id where tp_automation_condition.automation_id = ?", tp_automation_id).
		Scan(&tp_automation_conditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
	}
	tp_automation["automation_conditions"] = tp_automation_conditions
	//自动化动作
	var tp_automation_actions []map[string]interface{}
	result = psql.Mydb.Table("tp_automation_action").
		Select("tp_automation_action.*,device.asset_id,asset.business_id").
		Joins("left join device on tp_automation_action.device_id = device.id").
		Joins("left join asset on device.asset_id = asset.id where tp_automation_action.automation_id = ?", tp_automation_id).
		Scan(&tp_automation_actions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
	}
	//判断是否有告警信息
	for i, tp_automation_action := range tp_automation_actions {

		if value, ok := tp_automation_action["action_type"].(string); ok {
			if value == "2" {
				if id, ok := tp_automation_action["warning_strategy_id"].(string); ok {
					var tp_warning_strategy = make(map[string]interface{})
					result := psql.Mydb.Model(&models.TpWarningStrategy{Id: id}).First(&tp_warning_strategy)
					if result.Error != nil {
						if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
							return tp_automation, result.Error
						}
					}
					tp_automation_actions[i]["warning_strategy"] = tp_warning_strategy
				}
			}
		}
	}
	tp_automation["automation_actions"] = tp_automation_actions

	return tp_automation, nil
}

// 获取列表
func (*TpAutomationService) GetTpAutomationList(PaginationValidate valid.TpAutomationPaginationValidate) ([]models.TpAutomation, int64, error) {
	var TpAutomations []models.TpAutomation
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"

	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}

	var count int64
	psql.Mydb.Model(&models.TpAutomation{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpAutomation{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpAutomations)
	if result.Error != nil {
		logs.Error(result.Error)
		return TpAutomations, 0, result.Error
	}
	return TpAutomations, count, nil
}

// 新增数据
//新增自动化：添加自动化得到id-》添加自动化条件-》添加自动化动作（判断有无告警信息，有则先添加告警策略）；-》以上动作失败回滚
func (*TpAutomationService) AddTpAutomation(tp_automation valid.AddTpAutomationValidate) (valid.AddTpAutomationValidate, error) {
	tx := psql.Mydb.Begin()
	// 添加自动化
	tp_automation.Id = utils.GetUuid()
	tp_automation.CreatedAt = time.Now().Unix()
	tp_automation.UpdateTime = time.Now().Unix()
	automationMap := structs.Map(&tp_automation)
	delete(automationMap, "AutomationConditions")
	delete(automationMap, "AutomationActions")
	result := tx.Model(&models.TpAutomation{}).Create(automationMap)
	if result.Error != nil {
		tx.Rollback()
		return tp_automation, result.Error
	}
	// 添加自动化条件
	for _, tp_automation_conditions := range tp_automation.AutomationConditions {
		tp_automation_conditions.Id = utils.GetUuid()
		tp_automation_conditions.AutomationId = tp_automation.Id
		// DeviceId外键可以为null，需要用map处理
		automationConditionsMap := structs.Map(&tp_automation_conditions)
		if tp_automation_conditions.DeviceId == "" {
			delete(automationConditionsMap, "DeviceId")
		}
		result := tx.Model(&models.TpAutomationCondition{}).Create(automationConditionsMap)
		if result.Error != nil {
			tx.Rollback()
			logs.Error(result.Error.Error())
			return tp_automation, result.Error
		} else {
			// 定时任务需要添加cron
			if tp_automation_conditions.ConditionType == "2" && tp_automation_conditions.TimeConditionType == "2" {
				var automationCondition models.TpAutomationCondition
				result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ?", tp_automation_conditions.Id).First(&automationCondition)
				if result.Error != nil {
					err := AutomationCron(automationCondition)
					if err != nil {
						logs.Error(err.Error())
					}
				}

			}
		}
	}
	// 添加自动化动作
	for _, tp_automation_actions := range tp_automation.AutomationActions {
		tp_automation_actions.Id = utils.GetUuid()
		if tp_automation_actions.ActionType == "2" {
			//有告警触发
			tp_automation_actions.WarningStrategy.Id = utils.GetUuid()
			result := tx.Model(&models.TpWarningStrategy{}).Create(tp_automation_actions.WarningStrategy)
			if result.Error != nil {
				tx.Rollback()
				logs.Error(result.Error.Error())
				return tp_automation, result.Error
			}
			tp_automation_actions.WarningStrategyId = tp_automation_actions.WarningStrategy.Id
		}
		tp_automation_actions.AutomationId = tp_automation.Id
		// 外键可以为null，需要用map处理
		automationActionsMap := structs.Map(&tp_automation_actions)
		if tp_automation_actions.DeviceId == "" {
			delete(automationActionsMap, "DeviceId")
		}
		if tp_automation_actions.WarningStrategyId == "" {
			delete(automationActionsMap, "WarningStrategyId")
		}
		if tp_automation_actions.ScenarioStrategyId == "" {
			delete(automationActionsMap, "ScenarioStrategyId")
		}
		delete(automationActionsMap, "WarningStrategy")
		result := tx.Model(&models.TpAutomationAction{}).Create(automationActionsMap)
		if result.Error != nil {
			tx.Rollback()
			logs.Error(result.Error.Error())
			return tp_automation, result.Error
		}
	}
	tx.Commit()
	return tp_automation, nil
}

// 修改数据
func (*TpAutomationService) EditTpAutomation(tp_automation valid.TpAutomationValidate) (valid.TpAutomationValidate, error) {
	// 首先查询原定时任务
	var automationConditions []models.TpAutomationCondition
	result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ? and condition_type = '2' and time_condition_type ='2'", tp_automation.Id).Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return tp_automation, result.Error
	}
	if result.RowsAffected > int64(0) {
		for _, automationCondition := range automationConditions {
			cronId := cast.ToInt(automationCondition.V2)
			C := tp_cron.C
			C.Remove(cron.EntryID(cronId))
		}
	}
	// 开启事务 删除自动化条件
	tx := psql.Mydb.Begin()
	result = tx.Delete(&models.TpAutomationCondition{}, "automation_id = ?", tp_automation.Id)
	//result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy.Id).Updates(&tp_scenario_strategy)
	if result.Error != nil {
		tx.Rollback()
		logs.Error(result.Error.Error())
		return tp_automation, result.Error
	}
	//重新添加condition
	for _, tp_automation_conditions := range tp_automation.AutomationConditions {
		tp_automation_conditions.Id = utils.GetUuid()
		tp_automation_conditions.AutomationId = tp_automation.Id
		// DeviceId外键可以为null，需要用map处理
		automationConditionsMap := structs.Map(&tp_automation_conditions)
		if tp_automation_conditions.DeviceId == "" {
			delete(automationConditionsMap, "DeviceId")
		}
		result := tx.Model(&models.TpAutomationCondition{}).Create(automationConditionsMap)
		if result.Error != nil {
			tx.Rollback()
			logs.Error(result.Error.Error())
			return tp_automation, result.Error
		} else {
			// 定时任务需要添加cron
			if tp_automation_conditions.ConditionType == "2" && tp_automation_conditions.TimeConditionType == "2" {
				var automationCondition models.TpAutomationCondition
				result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ?", tp_automation_conditions.Id).First(&automationCondition)
				if result.Error != nil {
					err := AutomationCron(automationCondition)
					if err != nil {
						logs.Error(err.Error())
					}
				}

			}
		}
	}
	// 如果旧记录有告警信息-新记录没有则删除，新记录有则修改
	// 如果旧记录没有告警信息-新纪录有则新增
	var oldWarningStrategyFlag int64
	var newWarningStrategyFlag int64
	var automationActions []models.TpAutomationAction
	result = psql.Mydb.Model(&models.TpAutomationAction{}).Where("automation_id = ? and action_type = '2'", tp_automation.Id).Find(&automationActions)
	if result.Error != nil {
		tx.Rollback()
		return tp_automation, result.Error
	}
	if result.RowsAffected > int64(0) {
		oldWarningStrategyFlag = 1
	}
	// 删除除告警信息外的其他action
	result = tx.Delete(&models.TpAutomationAction{}, "automation_id = ? and action_type != '2'", tp_automation.Id)
	//result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy.Id).Updates(&tp_scenario_strategy)
	if result.Error != nil {
		tx.Rollback()
		return tp_automation, result.Error
	}
	for _, tp_automation_actions := range tp_automation.AutomationActions {
		automationActionsMap := structs.Map(&tp_automation_actions)
		if tp_automation_actions.DeviceId == "" {
			delete(automationActionsMap, "DeviceId")
		}
		if tp_automation_actions.WarningStrategyId == "" {
			delete(automationActionsMap, "WarningStrategyId")
		}
		if tp_automation_actions.ScenarioStrategyId == "" {
			delete(automationActionsMap, "ScenarioStrategyId")
		}
		delete(automationActionsMap, "WarningStrategy")
		if tp_automation_actions.ActionType != "2" || oldWarningStrategyFlag == int64(0) {
			if tp_automation_actions.ActionType == "2" {
				newWarningStrategyFlag = 1
			}
			tp_automation_actions.Id = utils.GetUuid()
			automationActionsMap["Id"] = tp_automation_actions.Id
			result := tx.Model(&models.TpAutomationAction{}).Create(automationActionsMap)
			if result.Error != nil {
				tx.Rollback()
				logs.Error(result.Error.Error())
				return tp_automation, result.Error
			}
		} else if tp_automation_actions.ActionType == "2" {
			//更新告警信息
			newWarningStrategyFlag = 1
			result := tx.Model(&models.TpWarningStrategy{}).Where("id = ?", tp_automation_actions.WarningStrategy.Id).Updates(&tp_automation_actions.WarningStrategy)
			if result.Error != nil {
				tx.Rollback()
				logs.Error(result.Error.Error())
				return tp_automation, result.Error
			}
		}
	}
	//删除告警策略
	if oldWarningStrategyFlag == int64(1) && newWarningStrategyFlag == int64(0) {
		result = tx.Delete(&models.TpWarningStrategy{}, "id = ?", automationActions[0].WarningStrategyId)
		//result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy.Id).Updates(&tp_scenario_strategy)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			tx.Rollback()
			return tp_automation, result.Error
		}
	}
	tp_automation.UpdateTime = time.Now().Unix()
	automationMap := structs.Map(&tp_automation)
	delete(automationMap, "AutomationConditions")
	delete(automationMap, "AutomationActions")
	result = tx.Model(&models.TpAutomation{}).Where("id = ?", tp_automation.Id).Updates(&automationMap)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		tx.Rollback()
		return tp_automation, result.Error
	}
	tx.Commit()
	return tp_automation, nil
}

// 删除数据
func (*TpAutomationService) DeleteTpAutomation(tp_automation models.TpAutomation) error {
	result := psql.Mydb.Delete(&tp_automation)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

// 开启和关闭
func (*TpAutomationService) EnabledAutomation(automationId string, enabled string) error {
	result := psql.Mydb.Model(&models.TpAutomation{}).Where("id = ?", automationId).
		Updates(map[string]interface{}{"UpdateTime": time.Now().Unix(), "enabled": enabled})
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

//添加自动化的定时任务
func AutomationCron(automationCondition models.TpAutomationCondition) error {
	C := tp_cron.C
	var logMessage string
	var cronString string
	if automationCondition.V1 == "0" {
		//几分钟
		number := cast.ToInt(automationCondition.V3)
		if number > 0 {
			cronString = "0/" + automationCondition.V3 + " * * * *"
			logMessage += "触发" + automationCondition.V3 + "分钟执行一次的任务；"
		} else {
			logs.Error("cron按分钟不能为空或0")
			return errors.New("cron按分钟不能为空或0")
		}
	} else if automationCondition.V1 == "1" {
		// 每小时的几分
		number := cast.ToInt(automationCondition.V3)
		cronString = cast.ToString(number) + " 0/1 * * * *"
		logMessage += "触发每小时的" + automationCondition.V3 + "执行一次的任务；"
	} else if automationCondition.V1 == "2" {
		// 每天的几点几分
		timeList := strings.Split(automationCondition.V3, ":")
		cronString = timeList[1] + " " + timeList[0] + " ? * * *"
		logMessage += "触发每天的" + automationCondition.V3 + "执行一次的任务；"
	} else if automationCondition.V1 == "3" {
		// 星期几的几点几分
		timeList := strings.Split(automationCondition.V3, ":")
		cronString = timeList[2] + " " + timeList[1] + " ? " + timeList[0] + " * *"
		logMessage += "触发每周的" + automationCondition.V3 + "执行一次的任务；"
	} else if automationCondition.V1 == "4" {
		// 每月的哪一天的几点几分
		timeList := strings.Split(automationCondition.V3, ":")
		cronString = timeList[2] + " " + timeList[1] + " " + timeList[0] + " * ? *"
		logMessage += "触发每月的" + automationCondition.V3 + "执行一次的任务；"
	} else if automationCondition.V1 == "5" {
		cronString = automationCondition.V1
	}
	execute := func() {
		// 触发，记录日志
		var automationLogMap = make(map[string]interface{})
		var sutomationLogService TpAutomationLogService
		var automationLog models.TpAutomationLog
		automationLog.AutomationId = automationCondition.AutomationId
		automationLog.ProcessDescription = logMessage
		automationLog.TriggerTime = time.Now().Format("2006/01/02 15:04:05")
		automationLog.ProcessResult = "2"
		automationLog, err := sutomationLogService.AddTpAutomationLog(automationLog)
		if err != nil {
			logs.Error(err.Error())
		} else {
			var conditionsService ConditionsService
			msg, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId, automationLog.Id)
			if err != nil {
				//执行失败，记录日志
				logs.Error(err.Error())
				automationLogMap["process_description"] = logMessage + err.Error()
			} else {
				//执行成功，记录日志
				logs.Info(logMessage)
				automationLogMap["process_description"] = logMessage + msg
				automationLogMap["process_result"] = '1'
			}
			err = sutomationLogService.UpdateTpAutomationLog(automationLogMap)
			if err != nil {
				logs.Error(err.Error())
			}
		}
	}
	cronId, _ := C.AddFunc(cronString, execute)
	// 将cronId更新到数据库
	result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("id = ?", automationCondition.AutomationId).Update("V2", cast.ToString(cronId))
	if result.Error != nil {
		C.Remove(cronId)
		logs.Error(result.Error.Error())
	}
	return nil
}

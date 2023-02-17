package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/fatih/structs"
	"gorm.io/gorm"
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
	tx := psql.Mydb.Begin()
	result := tx.Delete(&models.TpAutomationCondition{}, "automation_id = ?", tp_automation.Id)
	//result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy.Id).Updates(&tp_scenario_strategy)
	if result.Error != nil {
		tx.Rollback()
		logs.Error(result.Error.Error())
		return tp_automation, result.Error
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

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

type TpScenarioStrategyService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpScenarioStrategyService) GetTpScenarioStrategyDetail(tp_scenario_strategy_id string) (map[string]interface{}, error) {
	var tp_scenario_strategy = make(map[string]interface{})
	result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy_id).First(&tp_scenario_strategy)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return tp_scenario_strategy, nil
		} else {
			return tp_scenario_strategy, result.Error
		}
	}
	var tp_scenario_action []map[string]interface{}
	result = psql.Mydb.Table("tp_scenario_action").
		Select("tp_scenario_action.*,device.asset_id,asset.business_id").
		Joins("left join device on tp_scenario_action.device_id = device.id").
		Joins("left join asset on device.asset_id = asset.id where scenario_strategy_id = ?", tp_scenario_strategy_id).
		Scan(&tp_scenario_action)
	//result = psql.Mydb.Model(&models.TpScenarioAction{}).Where("scenario_strategy_id = ?", tp_scenario_strategy_id).Find(&tp_scenario_action)
	if result.Error != nil {
		return tp_scenario_strategy, result.Error
	}
	tp_scenario_strategy["scenario_actions"] = tp_scenario_action
	return tp_scenario_strategy, nil
}

// 获取列表
func (*TpScenarioStrategyService) GetTpScenarioStrategyList(PaginationValidate valid.TpScenarioStrategyPaginationValidate) ([]models.TpScenarioStrategy, int64, error) {
	var TpScenarioStrategys []models.TpScenarioStrategy
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}
	var count int64
	psql.Mydb.Model(&models.TpScenarioStrategy{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpScenarioStrategys)
	if result.Error != nil {
		return TpScenarioStrategys, 0, result.Error
	}
	return TpScenarioStrategys, count, nil
}

// 新增数据
func (*TpScenarioStrategyService) AddTpScenarioStrategy(tp_scenario_strategy valid.AddTpScenarioStrategyValidate) (valid.AddTpScenarioStrategyValidate, error) {
	tx := psql.Mydb.Begin()
	tp_scenario_strategy.Id = utils.GetUuid()
	tp_scenario_strategy.CreatedAt = time.Now().Unix()
	tp_scenario_strategy.UpdateTime = time.Now().Unix()
	result := tx.Model(&models.TpScenarioStrategy{}).Create(tp_scenario_strategy)
	if result.Error != nil {
		tx.Rollback()
		return tp_scenario_strategy, result.Error
	}
	for _, scenarioAction := range tp_scenario_strategy.AddTpScenarioActions {
		scenarioAction.Id = utils.GetUuid()
		scenarioAction.ScenarioStrategyId = tp_scenario_strategy.Id
		// DeviceId外键可以为null，需要用map处理
		scenarioActionMap := structs.Map(&scenarioAction)
		if scenarioAction.DeviceId == "" {
			delete(scenarioActionMap, "DeviceId")
		}
		result := tx.Model(&models.TpScenarioAction{}).Create(scenarioActionMap)
		if result.Error != nil {
			tx.Rollback()
			return tp_scenario_strategy, result.Error
		}
	}
	tx.Commit()
	return tp_scenario_strategy, nil
}

// 修改数据
func (*TpScenarioStrategyService) EditTpScenarioStrategy(tp_scenario_strategy valid.EditTpScenarioStrategyValidate) (valid.EditTpScenarioStrategyValidate, error) {
	tx := psql.Mydb.Begin()
	// 删除所有action
	result := tx.Where("scenario_strategy_id = ?", tp_scenario_strategy.Id).Delete(&models.TpScenarioAction{})
	//result := psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy.Id).Updates(&tp_scenario_strategy)
	if result.Error != nil {
		tx.Rollback()
		return tp_scenario_strategy, result.Error
	}
	// 重新添加action
	for i, scenarioAction := range tp_scenario_strategy.AddTpScenarioActions {

		scenarioAction.Id = utils.GetUuid()
		scenarioAction.ScenarioStrategyId = tp_scenario_strategy.Id
		// DeviceId外键可以为null，需要用map处理
		scenarioActionMap := structs.Map(&scenarioAction)
		if scenarioAction.DeviceId == "" {
			delete(scenarioActionMap, "DeviceId")
		}
		result := tx.Model(&models.TpScenarioAction{}).Create(scenarioActionMap)
		if result.Error != nil {
			tx.Rollback()
			return tp_scenario_strategy, result.Error
		}
		tp_scenario_strategy.AddTpScenarioActions[i].Id = scenarioAction.Id

	}
	//修改ScenarioStrategy
	tp_scenario_strategy.UpdateTime = time.Now().Unix()
	scenarioStrategyMap := structs.Map(&tp_scenario_strategy)
	delete(scenarioStrategyMap, "Id")
	delete(scenarioStrategyMap, "AddTpScenarioActions")
	result = psql.Mydb.Model(&models.TpScenarioStrategy{}).Where("id = ?", tp_scenario_strategy.Id).Updates(&scenarioStrategyMap)
	if result.Error != nil {
		tx.Rollback()
		return tp_scenario_strategy, result.Error
	}
	tx.Commit()
	//修改后的回显不是必要的，所以没必要再去查询
	return tp_scenario_strategy, nil
}

// 删除数据
func (*TpScenarioStrategyService) DeleteTpScenarioStrategy(tp_scenario_strategy models.TpScenarioStrategy) error {
	result := psql.Mydb.Delete(&tp_scenario_strategy)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}

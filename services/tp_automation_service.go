package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"

	"github.com/beego/beego/v2/core/logs"
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

func (*TpAutomationService) GetTpAutomationDetail(tp_automation_id string) (models.TpAutomation, error) {
	var tp_automation models.TpAutomation
	result := psql.Mydb.First(&tp_automation, "id = ?", tp_automation_id)
	if result != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return tp_automation, nil
		} else {
			return tp_automation, result.Error
		}

	}
	return tp_automation, nil
}

// 获取列表
func (*TpAutomationService) GetTpAutomationList(PaginationValidate valid.TpAutomationPaginationValidate) (bool, []models.TpAutomation, int64) {
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
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpAutomations, 0
	}
	return true, TpAutomations, count
}

// 新增数据
//新增自动化：添加自动化条件-》添加自动化动作（判断有无告警信息，有则先添加告警策略）；-》添加自动化-》以上动作失败回滚
func (*TpAutomationService) AddTpAutomation(tp_automation valid.AddTpAutomationValidate) (valid.AddTpAutomationValidate, error) {
	tx := psql.Mydb.Begin()
	// 添加自动化条件
	for _, tp_automation_conditions := range tp_automation.AutomationConditions {
		tp_automation_conditions.Id = utils.GetUuid()
		result := tx.Model(&models.TpAutomationCondition{}).Create(tp_automation_conditions)
		if result.Error != nil {
			tx.Rollback()
			return tp_automation, result.Error
		}
	}
	// 添加自动化动作
	for _, tp_automation_actions := range tp_automation.AutomationActions {
		if tp_automation_actions.ActionType == "2" {
			//有告警触发
			tp_automation_actions.WarningStrategy.Id = utils.GetUuid()
			result := tx.Model(&models.TpAutomationCondition{}).Create(tp_automation_actions.WarningStrategy)
			if result.Error != nil {
				tx.Rollback()
				return tp_automation, result.Error
			}
			tp_automation_actions.WarningStrategyId = tp_automation_actions.WarningStrategy.Id
		}
		result := tx.Model(&models.TpAutomationAction{}).Create(tp_automation_actions)
		if result.Error != nil {
			tx.Rollback()
			return tp_automation, result.Error
		}
	}
	// 添加自动化
	tp_automation.Id = uuid.GetUuid()
	result := tx.Model(&models.TpAutomation{}).Create(tp_automation)
	if result.Error != nil {
		tx.Rollback()
		return tp_automation, result.Error
	}
	return tp_automation, nil
}

// 修改数据
func (*TpAutomationService) EditTpAutomation(tp_automation valid.TpAutomationValidate) bool {
	result := psql.Mydb.Model(&models.TpAutomation{}).Where("id = ?", tp_automation.Id).Updates(&tp_automation)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpAutomationService) DeleteTpAutomation(tp_automation models.TpAutomation) error {
	result := psql.Mydb.Delete(&tp_automation)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

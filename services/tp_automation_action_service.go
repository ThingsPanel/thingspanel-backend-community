package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpAutomationActionService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpAutomationActionService) GetTpAutomationActionDetail(tp_automation_action_id string) (models.TpAutomationAction, error) {
	var tp_automation_action models.TpAutomationAction
	result := psql.Mydb.First(&tp_automation_action, "id = ?", tp_automation_action_id)
	if result != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return tp_automation_action, nil
		} else {
			return tp_automation_action, result.Error
		}

	}
	return tp_automation_action, nil
}

// 获取列表
func (*TpAutomationActionService) GetTpAutomationActionList(PaginationValidate valid.TpAutomationActionPaginationValidate) (bool, []models.TpAutomationAction, int64) {
	var TpAutomationActions []models.TpAutomationAction
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"

	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}

	var count int64
	psql.Mydb.Model(&models.TpAutomationAction{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpAutomationAction{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpAutomationActions)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpAutomationActions, 0
	}
	return true, TpAutomationActions, count
}

// 新增数据
func (*TpAutomationActionService) AddTpAutomationAction(tp_automation_action models.TpAutomationAction) (models.TpAutomationAction, error) {
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
func (*TpAutomationActionService) EditTpAutomationAction(tp_automation_action valid.TpAutomationActionValidate) bool {
	result := psql.Mydb.Model(&models.TpAutomationAction{}).Where("id = ?", tp_automation_action.Id).Updates(&tp_automation_action)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpAutomationActionService) DeleteTpAutomationAction(tp_automation_action models.TpAutomationAction) error {
	result := psql.Mydb.Delete(&tp_automation_action)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

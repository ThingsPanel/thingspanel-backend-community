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

type TpWarningStrategyService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpWarningStrategyService) GetTpWarningStrategyDetail(tp_warning_strategy_id string) (models.TpWarningStrategy, error) {
	var tp_warning_strategy models.TpWarningStrategy
	result := psql.Mydb.First(&tp_warning_strategy, "id = ?", tp_warning_strategy_id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return tp_warning_strategy, nil
		} else {
			return tp_warning_strategy, result.Error
		}

	}
	return tp_warning_strategy, nil
}

// 获取列表
func (*TpWarningStrategyService) GetTpWarningStrategyList(PaginationValidate valid.TpWarningStrategyPaginationValidate) (bool, []models.TpWarningStrategy, int64) {
	var TpWarningStrategys []models.TpWarningStrategy
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}
	var count int64
	psql.Mydb.Model(&models.TpWarningStrategy{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpWarningStrategys)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpWarningStrategys, 0
	}
	return true, TpWarningStrategys, count
}

// 新增数据
func (*TpWarningStrategyService) AddTpWarningStrategy(tp_warning_strategy models.TpWarningStrategy) (models.TpWarningStrategy, error) {
	var uuid = uuid.GetUuid()
	tp_warning_strategy.Id = uuid
	result := psql.Mydb.Create(&tp_warning_strategy)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_warning_strategy, result.Error
	}
	return tp_warning_strategy, nil
}

// 修改数据
func (*TpWarningStrategyService) EditTpWarningStrategy(tp_warning_strategy valid.TpWarningStrategyValidate) bool {
	result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", tp_warning_strategy.Id).Updates(&tp_warning_strategy)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpWarningStrategyService) DeleteTpWarningStrategy(tp_warning_strategy models.TpWarningStrategy) error {
	result := psql.Mydb.Delete(&tp_warning_strategy)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	return nil
}

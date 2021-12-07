package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"

	"gorm.io/gorm"
)

type ConditionsService struct {
}

// 获取全部策略
func (*ConditionsService) All() ([]models.Condition, int64) {
	var conditions []models.Condition
	result := psql.Mydb.Find(&conditions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return conditions, result.RowsAffected
}

// 获取策略
func (*ConditionsService) GetConditionByID(id string) (*models.Condition, int64) {
	var condition models.Condition
	result := psql.Mydb.Where("id = ?", id).First(&condition)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &condition, result.RowsAffected
}

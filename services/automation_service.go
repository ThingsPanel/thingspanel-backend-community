package services

import (
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"

	"ThingsPanel-Go/initialize/psql"

	"gorm.io/gorm"
)

type AutomationService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// GetAutomationById 根据id获取一条Automation数据
func (*AutomationService) GetAutomationById(id string) (*models.Condition, int64) {
	var condition models.Condition
	result := psql.Mydb.Where("id = ?", id).First(&condition)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &condition, result.RowsAffected
}

// Paginate 分页获取Automation数据
func (*AutomationService) Paginate(business_id string, offset int, pageSize int) ([]models.Condition, int64) {
	var conditions []models.Condition
	result := psql.Mydb.Where("business_id = ?", business_id).Limit(offset).Offset(pageSize - 1*offset).Find(&conditions)
	var count int64

	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(conditions) == 0 {
		conditions = []models.Condition{}
	} else {
		psql.Mydb.Where("business_id = ?", business_id).Count(&count)
	}
	return conditions, count
}

// Add新增一条Automation数据
func (*AutomationService) Add(business_id string, name string, describe string, status string, config string, sort int64, t int64, issued string, customer_id string) (bool, string) {
	var uuid = uuid.GetUuid()
	condition := models.Condition{
		ID:         uuid,
		BusinessID: business_id,
		Name:       name,
		Describe:   describe,
		Status:     status,
		Config:     config,
		Sort:       sort,
		Type:       t,
		Issued:     issued,
		CustomerID: customer_id,
	}
	result := psql.Mydb.Create(&condition)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 根据ID编辑一条Automationg数据
func (*AutomationService) Edit(id string, business_id string, name string, describe string, status string, config string, sort int64, t int64, issued string, customer_id string) bool {
	// updated_at
	result := psql.Mydb.Model(&models.Condition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"business_id": business_id,
		"name":        name,
		"describe":    describe,
		"status":      status,
		"config":      config,
		"sort":        sort,
		"type":        t,
		"issued":      issued,
		"customer_id": customer_id,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID删除一条Automationg数据
func (*AutomationService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Condition{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

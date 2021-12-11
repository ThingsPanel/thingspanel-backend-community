package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"time"

	"gorm.io/gorm"
)

type WidgetService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

type DashboardConfig struct {
	SliceId   int64  `json:"slice_id"`
	X         int64  `json:"x"`
	Y         int64  `json:"y"`
	W         int64  `json:"w"`
	H         int64  `json:"h"`
	Width     int64  `json:"width"`
	Height    int64  `json:"height"`
	I         string `json:"i"`
	ChartType string `json:"chart_type"`
	Title     string `json:"title"`
}

// Paginate 分页获取widget数据
func (*WidgetService) Paginate(name string, offset int, pageSize int) ([]models.Widget, int64) {
	var widgets []models.Widget
	result := psql.Mydb.Limit(pageSize).Offset(offset).Find(&widgets)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(widgets) == 0 {
		widgets = []models.Widget{}
	}
	return widgets, result.RowsAffected
}

// 根据id获取一条Widget数据
func (*WidgetService) GetWidgetById(id string) (*models.Widget, int64) {
	var widget models.Widget
	result := psql.Mydb.Where("id = ?", id).First(&widget)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &widget, result.RowsAffected
}

// 查重
func (*WidgetService) GetRepeat(dashboard_id string, asset_id string, device_id string, widget_identifier string) bool {
	var widget models.Widget
	result := psql.Mydb.Where("dashboard_id = ? AND asset_id = ? AND device_id = ? AND widget_identifier = ?", dashboard_id, asset_id, device_id, widget_identifier).First(&widget)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if result.RowsAffected == 0 {
		return false
	} else {
		return true
	}
}

// Add新增一条widget数据
func (*WidgetService) Add(dashboard_id string, asset_id string, device_id string, widget_identifier string, config string) (bool, string) {
	var uuid = uuid.GetUuid()
	widget := models.Widget{
		ID:               uuid,
		DashboardID:      dashboard_id,
		Config:           config,
		AssetID:          asset_id,
		DeviceID:         device_id,
		WidgetIdentifier: widget_identifier,
		UpdatedAt:        time.Now(),
	}
	result := psql.Mydb.Create(&widget)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

func (*WidgetService) ForAddEdit(asset_id string, device_id string, dashboard_id string, widget_identifier string, config string) bool {
	result := psql.Mydb.Model(models.Widget{}).Where("asset_id = ? AND device_id= ? AND dashboard_id= ?", asset_id, device_id, dashboard_id).Updates(models.Widget{
		WidgetIdentifier: widget_identifier,
		Config:           config,
		UpdatedAt:        time.Now(),
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID编辑一条widget数据
func (*WidgetService) Edit(id string, dashboard_id string, asset_id string, device_id string, widget_identifier string, config string) bool {
	result := psql.Mydb.Model(&models.Widget{}).Where("id = ?", id).Updates(map[string]interface{}{
		"dashboard_id":      dashboard_id,
		"asset_id":          asset_id,
		"device_id":         device_id,
		"widget_identifier": widget_identifier,
		"config":            config,
		"updated_at":        time.Now(),
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID删除一条widget数据
func (*WidgetService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Widget{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据dashboard_id获取一条Widget数据
func (*WidgetService) GetWidgetDashboardId(dashboard_id string) ([]models.Widget, int64) {
	var widgets []models.Widget
	result := psql.Mydb.Where("dashboard_id = ?", dashboard_id).Find(&widgets)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(widgets) == 0 {
		widgets = []models.Widget{}
	}
	return widgets, result.RowsAffected
}

// 跟新 config
func (*WidgetService) UpdateConfigByWidgetId(id string, config string) bool {
	result := psql.Mydb.Model(&models.Widget{}).Where("id = ?", id).Updates(map[string]interface{}{
		"config":     config,
		"updated_at": time.Now(),
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

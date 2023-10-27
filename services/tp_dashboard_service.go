package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"

	"gorm.io/gorm"
)

type TpDashboardService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpDashboardService) GetTpDashboardDetail(tp_dashboard_id string) []models.TpDashboard {
	var tp_dashboard []models.TpDashboard
	psql.Mydb.First(&tp_dashboard, "id = ?", tp_dashboard_id)
	return tp_dashboard
}

// 获取列表
func (*TpDashboardService) GetTpDashboardList(PaginationValidate valid.TpDashboardPaginationValidate, tenantId string) (bool, []models.TpDashboard, int64) {
	var TpDashboards []models.TpDashboard
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpDashboard{})
	if PaginationValidate.ShareId != "" {
		db.Where("share_id = ?", PaginationValidate.ShareId)
	} else {
		db.Where("tenant_id = ? ", tenantId)
	}
	if PaginationValidate.RelationId != "" {
		db.Where("relation_id = ?", PaginationValidate.RelationId)
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("create_at desc").Find(&TpDashboards)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, TpDashboards, 0
	}
	return true, TpDashboards, count
}

// 新增数据
func (*TpDashboardService) AddTpDashboard(tp_dashboard models.TpDashboard) (bool, models.TpDashboard) {
	result := psql.Mydb.Create(&tp_dashboard)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, tp_dashboard
	}
	return true, tp_dashboard
}

// 修改数据
func (*TpDashboardService) EditTpDashboard(tp_dashboard valid.TpDashboardValidate, tenantId string) bool {
	result := psql.Mydb.Model(&models.TpDashboard{}).Where("id = ? and tenant_id = ?", tp_dashboard.Id, tenantId).Updates(&tp_dashboard)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpDashboardService) DeleteTpDashboard(tp_dashboard models.TpDashboard) bool {
	result := psql.Mydb.Delete(&tp_dashboard)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据id保存对应的分享id
func (*TpDashboardService) UpdateShareId(tp_dashboard_id string, shareId string) bool {
	result := psql.Mydb.Model(&models.TpDashboard{}).Where("id = ?", tp_dashboard_id).Update("share_id", shareId)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 查询可视化对应的设备列表
func (*TpDashboardService) GetDeviceListByVisualizationID(tp_dashboard_id string) ([]string, error) {
	var dashboard models.TpDashboard
	
	// 查询可视化对应的设备列表
	result := psql.Mydb.Where("id = ?", tp_dashboard_id).First(&dashboard)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.New("visualization not found")
		}
		return nil, result.Error
	}

	deviceList, err := utils.GetDeviceListByVisualizationData(dashboard.JsonData)
	if err != nil {
		return nil, err
	}
	return deviceList, nil
}

// 查询可视化对应的设备列表
func (*TpDashboardService) GetDeviceListByShareID(share_id string) (models.TpDashboard, error) {
	var dashboard models.TpDashboard
	
	// 查询可视化对应的设备列表
	result := psql.Mydb.Where("share_id = ?", share_id).First(&dashboard)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return models.TpDashboard{}, errors.New("visualization not found")
		}
		return models.TpDashboard{}, result.Error
	}
	return dashboard, nil
}

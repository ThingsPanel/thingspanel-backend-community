package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"errors"
	"strings"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type SharedVisualizationService struct {
}


// 根据分享id获取分享信息
func (*SharedVisualizationService) GetShareInfo(shareId string) (*models.SharedVisualization, error) {
	var sharedVisualization models.SharedVisualization
	result := psql.Mydb.Model(&models.SharedVisualization{}).Where("share_id = ?", shareId).Find(&sharedVisualization)
	
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return nil, result.Error
	}
	return &sharedVisualization, nil
}

// 更新设备列表
func (*SharedVisualizationService) UpdateDeviceList(dashboardId string, deviceList string) bool {
	result := psql.Mydb.Model(&models.SharedVisualization{}).Where("dashboard_id = ?", dashboardId).Update("device_list", deviceList)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}


// 新建可视化分享
func (*SharedVisualizationService) CreateSharedVisualization(sharedVisualization models.SharedVisualization) (bool, models.SharedVisualization) {
	result := psql.Mydb.Create(&sharedVisualization)
	if result.Error != nil {
		return false, sharedVisualization
	}
	return true, sharedVisualization
}


// 根据可视化id和设备id判断是否有权限
func (*SharedVisualizationService) HasPermissionByDeviceID(share_id string, dashboard_id string, device_id string) bool {
	var sharedVisualization models.SharedVisualization
	// 查询可视化
	result := psql.Mydb.Where("share_id = ?", share_id).First(&sharedVisualization)
	if result.Error != nil {
		logs.Error(result.Error)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return false
		}
		return false
	}
	if dashboard_id != "" && sharedVisualization.DashboardID != dashboard_id {
		return false
	}

	// 不传入设备id时，可视化id和分享id匹配即有权限
	if dashboard_id != "" && sharedVisualization.DashboardID == dashboard_id && device_id == "" {
		return true
	}

	// 判断设备列表中是否包含该设备
	if strings.Contains(sharedVisualization.DeviceList, device_id) {
		return true
	}

	return false
}

// 根据设备id判断是否有权限
func (*SharedVisualizationService) isDeviceIDShared(share_id string, device_id string) bool {
	var sharedVisualization models.SharedVisualization
	// 查询可视化
	result := psql.Mydb.Where("share_id = ?", share_id).First(&sharedVisualization)
	if result.Error != nil {
		logs.Error(result.Error)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return false
		}
		return false
	}

	// 判断设备列表中是否包含该设备
	if strings.Contains(sharedVisualization.DeviceList, device_id) {
		return true
	}

	return false
}


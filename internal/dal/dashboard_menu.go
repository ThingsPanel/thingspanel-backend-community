package dal

import (
	"errors"

	model "project/internal/model"
	global "project/pkg/global"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetTenantDashboardMenu(tenantID string, dashboardID string) (*model.TenantDashboardMenu, error) {
	var menu model.TenantDashboardMenu
	err := global.DB.Where("tenant_id = ? AND dashboard_id = ?", tenantID, dashboardID).First(&menu).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		logrus.Error(err)
		return nil, err
	}
	return &menu, nil
}

func UpsertTenantDashboardMenu(menu *model.TenantDashboardMenu) error {
	err := global.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "dashboard_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"dashboard_name",
			"menu_name",
			"parent_code",
			"sort",
			"enabled",
			"updated_at",
		}),
	}).Create(menu).Error
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func DeleteTenantDashboardMenu(tenantID string, dashboardID string) error {
	err := global.DB.Where("tenant_id = ? AND dashboard_id = ?", tenantID, dashboardID).
		Delete(&model.TenantDashboardMenu{}).Error
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func ListTenantDashboardMenus(tenantID string, parentCode string) ([]model.TenantDashboardMenu, error) {
	var menus []model.TenantDashboardMenu
	err := global.DB.
		Where("tenant_id = ? AND parent_code = ? AND enabled = ?", tenantID, parentCode, true).
		Order(`sort asc, created_at asc`).
		Find(&menus).Error
	if err != nil {
		logrus.Error(err)
	}
	return menus, err
}

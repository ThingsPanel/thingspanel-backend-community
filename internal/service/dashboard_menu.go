package service

import (
	"strings"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type DashboardMenu struct{}

func validateDashboardTenantAccess(tenantID string, dashboardID string) error {
	normalizedTenantID := strings.TrimSpace(tenantID)
	normalizedDashboardID := strings.TrimSpace(dashboardID)

	if normalizedTenantID == "" {
		return errcode.NewWithMessage(errcode.CodeNoPermission, "tenant dashboard menu is only available for tenant users")
	}

	if normalizedDashboardID == "" {
		return errcode.NewWithMessage(errcode.CodeParamError, "dashboard_id is required")
	}

	dashboard, err := dal.GetVisDashboardByID(normalizedDashboardID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation":    "get_vis_dashboard",
			"dashboard_id": normalizedDashboardID,
			"error":        err.Error(),
		})
	}

	if dashboard == nil {
		return errcode.NewWithMessage(errcode.CodeNotFound, "dashboard not found")
	}

	if dashboard.TenantID == nil || strings.TrimSpace(*dashboard.TenantID) != normalizedTenantID {
		return errcode.NewWithMessage(errcode.CodeNoPermission, "dashboard not owned by current tenant")
	}

	return nil
}

func (*DashboardMenu) GetTenantDashboardMenu(tenantID string, dashboardID string) (*model.TenantDashboardMenuRsp, error) {
	menu, err := dal.GetTenantDashboardMenu(tenantID, dashboardID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "get_dashboard_menu",
			"error":     err.Error(),
		})
	}

	if menu == nil {
		return nil, nil
	}

	return menu.ToRsp(), nil
}

func (*DashboardMenu) UpsertTenantDashboardMenu(claims *utils.UserClaims, dashboardID string, req *model.UpsertTenantDashboardMenuReq) (*model.TenantDashboardMenuRsp, error) {
	if err := validateDashboardTenantAccess(claims.TenantID, dashboardID); err != nil {
		return nil, err
	}

	sortValue := int16(1)
	if req.Sort != nil {
		sortValue = *req.Sort
	}

	enabledValue := true
	if req.Enabled != nil {
		enabledValue = *req.Enabled
	}

	dashboardName := req.MenuName
	if req.DashboardName != nil && *req.DashboardName != "" {
		dashboardName = *req.DashboardName
	}

	existing, err := dal.GetTenantDashboardMenu(claims.TenantID, dashboardID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "get_dashboard_menu_before_upsert",
			"error":     err.Error(),
		})
	}

	now := time.Now().UTC()
	menu := model.TenantDashboardMenu{
		ID:            uuid.New(),
		TenantID:      claims.TenantID,
		DashboardID:   dashboardID,
		DashboardName: dashboardName,
		MenuName:      req.MenuName,
		ParentCode:    "home",
		Sort:          sortValue,
		Enabled:       enabledValue,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if existing != nil {
		menu.ID = existing.ID
		menu.CreatedAt = existing.CreatedAt
	}

	err = dal.UpsertTenantDashboardMenu(&menu)
	if err != nil {
		logrus.Error(err)
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "upsert_dashboard_menu",
			"error":     err.Error(),
		})
	}

	return menu.ToRsp(), nil
}

func (*DashboardMenu) DeleteTenantDashboardMenu(tenantID string, dashboardID string) error {
	err := dal.DeleteTenantDashboardMenu(tenantID, dashboardID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "delete_dashboard_menu",
			"error":     err.Error(),
		})
	}
	return nil
}

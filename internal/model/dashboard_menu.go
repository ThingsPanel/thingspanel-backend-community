package model

import "time"

const TableNameTenantDashboardMenu = "tenant_dashboard_menus"

type TenantDashboardMenu struct {
	ID            string    `gorm:"column:id;primaryKey" json:"id"`
	TenantID      string    `gorm:"column:tenant_id;not null;index:idx_tenant_dashboard_menu_unique,unique" json:"tenant_id"`
	DashboardID   string    `gorm:"column:dashboard_id;not null;index:idx_tenant_dashboard_menu_unique,unique" json:"dashboard_id"`
	DashboardName string    `gorm:"column:dashboard_name;not null" json:"dashboard_name"`
	MenuName      string    `gorm:"column:menu_name;not null" json:"menu_name"`
	ParentCode    string    `gorm:"column:parent_code;not null;default:home" json:"parent_code"`
	Sort          int16     `gorm:"column:sort;not null;default:1" json:"sort"`
	Enabled       bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt     time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (*TenantDashboardMenu) TableName() string {
	return TableNameTenantDashboardMenu
}

type UpsertTenantDashboardMenuReq struct {
	MenuName      string  `json:"menu_name" validate:"required,max=99"`
	DashboardName *string `json:"dashboard_name" validate:"omitempty,max=99"`
	Sort          *int16  `json:"sort" validate:"omitempty,max=10000"`
	Enabled       *bool   `json:"enabled"`
}

type TenantDashboardMenuRsp struct {
	DashboardID   string `json:"dashboard_id"`
	DashboardName string `json:"dashboard_name"`
	MenuName      string `json:"menu_name"`
	ParentCode    string `json:"parent_code"`
	Sort          int16  `json:"sort"`
	Enabled       bool   `json:"enabled"`
}

func (m *TenantDashboardMenu) ToRsp() *TenantDashboardMenuRsp {
	return &TenantDashboardMenuRsp{
		DashboardID:   m.DashboardID,
		DashboardName: m.DashboardName,
		MenuName:      m.MenuName,
		ParentCode:    m.ParentCode,
		Sort:          m.Sort,
		Enabled:       m.Enabled,
	}
}

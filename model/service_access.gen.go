// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.

package model

import (
	"time"
)

const TableNameServiceAccess = "service_access"

// ServiceAccess mapped from table <service_access>
type ServiceAccess struct {
	ID                  string    `gorm:"column:id;primaryKey" json:"id"`
	Name                string    `gorm:"column:name;not null" json:"name"`
	ServicePluginID     string    `gorm:"column:service_plugin_id;not null" json:"service_plugin_id"`
	Voucher             string    `gorm:"column:voucher;not null" json:"voucher"`
	Description         *string   `gorm:"column:description" json:"description"`
	ServiceAccessConfig *string   `gorm:"column:service_access_config" json:"service_access_config"`
	Remark              *string   `gorm:"column:remark" json:"remark"`
	CreateAt            time.Time `gorm:"column:create_at;not null" json:"create_at"`
	UpdateAt            time.Time `gorm:"column:update_at;not null" json:"update_at"`
	TenantID            string    `gorm:"column:tenant_id;not null" json:"tenant_id"`
}

// TableName ServiceAccess's table name
func (*ServiceAccess) TableName() string {
	return TableNameServiceAccess
}

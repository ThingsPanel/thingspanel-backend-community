package model

import "time"

// UserDevicePermission stores the tenant-local device scope of a user.
// AccessLevel is read or manage; manage always includes read access.
type UserDevicePermission struct {
	UserID      string    `gorm:"column:user_id;not null" json:"user_id"`
	DeviceID    string    `gorm:"column:device_id;not null" json:"device_id"`
	TenantID    string    `gorm:"column:tenant_id;not null" json:"tenant_id"`
	AccessLevel string    `gorm:"column:access_level;not null" json:"access_level"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (*UserDevicePermission) TableName() string { return "user_device_permissions" }

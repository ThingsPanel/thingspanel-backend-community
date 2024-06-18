// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.

package model

import (
	"time"
)

const TableNameOtaUpgradePackage = "ota_upgrade_packages"

// OtaUpgradePackage mapped from table <ota_upgrade_packages>
type OtaUpgradePackage struct {
	ID             string     `gorm:"column:id;primaryKey;comment:Id" json:"id"`                               // Id
	Name           string     `gorm:"column:name;not null;comment:升级包名称" json:"name"`                          // 升级包名称
	Version        string     `gorm:"column:version;not null;comment:升级包版本号" json:"version"`                   // 升级包版本号
	TargetVersion  *string    `gorm:"column:target_version;comment:待升级版本号" json:"target_version"`              // 待升级版本号
	DeviceConfigID string     `gorm:"column:device_config_id;not null;comment:设备配置id" json:"device_config_id"` // 设备配置id
	Module         *string    `gorm:"column:module;comment:模块名称" json:"module"`                                // 模块名称
	PackageType    int16      `gorm:"column:package_type;not null;comment:升级包类型1-差分 2-整包" json:"package_type"` // 升级包类型1-差分 2-整包
	SignatureType  *string    `gorm:"column:signature_type;comment:签名算法MD5 SHA256" json:"signature_type"`      // 签名算法MD5 SHA256
	AdditionalInfo *string    `gorm:"column:additional_info;default:{};comment:附加信息" json:"additional_info"`   // 附加信息
	Description    *string    `gorm:"column:description;comment:描述" json:"description"`                        // 描述
	PackageURL     *string    `gorm:"column:package_url;comment:包下载路径" json:"package_url"`                     // 包下载路径
	CreatedAt      time.Time  `gorm:"column:created_at;not null;comment:创建时间" json:"created_at"`               // 创建时间
	UpdatedAt      *time.Time `gorm:"column:updated_at;comment:修改时间" json:"updated_at"`                        // 修改时间
	Remark         *string    `gorm:"column:remark;comment:备注" json:"remark"`                                  // 备注
	Signature      *string    `gorm:"column:signature;comment:升级包签名" json:"signature"`                         // 升级包签名
	TenantID       *string    `gorm:"column:tenant_id" json:"tenant_id"`
}

// TableName OtaUpgradePackage's table name
func (*OtaUpgradePackage) TableName() string {
	return TableNameOtaUpgradePackage
}
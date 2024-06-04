package model

type CreateOTAUpgradePackageReq struct {
	Name           string  `json:"name" validate:"required,max=200"`                     // 升级包名称
	Version        string  `json:"version"  validate:"required,max=36"`                  // 版本号
	TargetVersion  *string `json:"target_version" validate:"omitempty,max=36"`           // 目标版本号
	DeviceConfigID string  `json:"device_config_id" validate:"required,max=36"`          // 设备配置ID
	Module         *string `json:"module" validate:"omitempty,max=36"`                   // 模块名称
	PackageType    *int16  `json:"package_type" validate:"required,oneof=1 2"`           // 升级包类型升级包类型1-差分 2-整包
	SignatureType  *string `json:"signature_type" validate:"omitempty,oneof=MD5 SHA256"` // 签名算法 MD5 SHA256
	AdditionalInfo *string `json:"additional_info" validate:"omitempty" example:"{}"`    // 附加信息,json格式
	Description    *string `json:"description" validate:"omitempty,max=500"`             // 描述
	PackageUrl     *string `json:"package_url" validate:"omitempty,max=500"`             // 升级包地址
	Remark         *string `json:"remark" validate:"omitempty,max=255"`
}

type UpdateOTAUpgradePackageReq struct {
	Id             string  `json:"id" validate:"required,max=36"`                        // 升级包ID
	Name           string  `json:"name" validate:"omitempty,max=200"`                    // 升级包名称
	Version        string  `json:"version"  validate:"omitempty,max=36"`                 // 版本号
	TargetVersion  *string `json:"target_version" validate:"omitempty,max=36"`           // 目标版本号
	DeviceConfigID string  `json:"device_config_id" validate:"omitempty,max=36"`         // 设备配置ID
	Module         *string `json:"module" validate:"omitempty,max=36"`                   // 模块名称
	PackageType    *int16  `json:"package_type" validate:"omitempty,oneof=1 2"`          // 升级包类型
	SignatureType  *string `json:"signature_type" validate:"omitempty,oneof=MD5 SHA256"` // 签名算法 MD5 SHA256
	AdditionalInfo *string `json:"additional_info" validate:"omitempty"`                 // 附加信息,json格式
	Description    *string `json:"description" validate:"omitempty,max=500"`             // 描述
	PackageUrl     *string `json:"package_url" validate:"omitempty,max=500"`             // 升级包地址
	Remark         *string `json:"remark" validate:"omitempty,max=255"`                  // 备注
}

type GetOTAUpgradePackageLisyByPageReq struct {
	PageReq
	DeviceConfigID string `json:"device_configs_id" form:"device_config_id" validate:"omitempty,max=36" example:"uuid"` // 设备配置ID
	Name           string `json:"name" form:"name" validate:"omitempty,max=200"`                                        //  升级包名称
}

type GetOTAUpgradeTaskListByPageRsp struct {
	OtaUpgradePackage
	DeviceConfigName string `json:"device_config_name" validate:"omitempty,max=200"` // 设备配置名称
}

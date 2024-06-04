package model

type CreateOTAUpgradeTaskReq struct {
	Name                string   `json:"name" validate:"required,max=200"`                   // 任务名称
	OTAUpgradePackageId string   `json:"ota_upgrade_package_id"  validate:"required,max=36"` // 升级包ID
	Description         *string  `json:"description" validate:"omitempty,max=500"`           // 描述
	Remark              *string  `json:"remark" validate:"omitempty,max=255"`                // 备注
	DeviceIdList        []string `json:"device_id_list" validate:"required"`                 // 设备列表
}

// type CreateOTAUpgradeTaskDeviceListReq struct {
// 	DeviceId string  `json:"device_id" validate:"required,max=200"` // 设备ID
// 	Remark   *string `json:"remark" validate:"omitempty,max=255"`   // 备注
// }

type GetOTAUpgradeTaskDetailReq struct {
	PageReq
	DeviceName       *string `json:"deivce_name" form:"device_name" validate:"omitempty,max=200"`               // 设备名称
	TaskStatus       *int16  `json:"task_status" form:"task_status" validate:"omitempty,max=10"`                // 任务状态 1-待推送2-已推送3-升级中4-升级成功-5-升级失败-6已取消
	OtaUpgradeTaskId string  `json:"ota_upgrade_task_id" form:"ota_upgrade_task_id" validate:"required,max=36"` // 任务ID
}

type GetOTAUpgradeTaskListByPageReq struct {
	PageReq
	OTAUpgradePackageId string `json:"ota_upgrade_package_id" form:"ota_upgrade_package_id" validate:"required,max=36"` // 升级包ID
}

type UpdateOTAUpgradeTaskStatusReq struct {
	Id     string `json:"id" validate:"required,max=36"`        // 任务详情ID
	Action int16  `json:"action" validate:"required,oneof=1 6"` // 任务状态 取消升级传6 重新升级传1
}

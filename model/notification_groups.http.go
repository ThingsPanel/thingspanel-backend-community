package model

// NotificationGroup defines
// type NotificationGroup struct {
// 	ID                 string    `gorm:"column:id;primaryKey" json:"id"`
// 	Name               string    `gorm:"column:name;not null" json:"name"`
// 	NotificationType   string    `gorm:"column:notification_type;not null" json:"notification_type"`
// 	Status             string    `gorm:"column:status;not null" json:"status"`
// 	NotificationConfig *string   `gorm:"column:notification_config" json:"notification_config"`
// 	Description        *string   `gorm:"column:description" json:"description"`
// 	TenantID           string    `gorm:"column:tenant_id;not null" json:"tenant_id"`
// 	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"created_at"`
// 	UpdatedAt          time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
// 	Remark             *string   `gorm:"column:remark" json:"remark"`
// }

type CreateNotificationGroupReq struct {
	Name               string  `json:"name" validate:"required"`                                          // 通知组名称
	NotificationType   string  `json:"notification_type" validate:"required" example:"MEMBER"`            // 通知类型
	Status             string  `json:"status" validate:"required"`                                        // 通知组状态
	NotificationConfig *string `json:"notification_config" validate:"omitempty" example:"{\"data\":123}"` // 通知配置
	Description        *string `json:"description" validate:"omitempty"`                                  // 通知组描述
	Remark             *string `json:"remark" validate:"omitempty"`                                       // 备注
}

type UpdateNotificationGroupReq struct {
	Name               *string `json:"name" validate:"omitempty"`                                         // 通知组名称
	NotificationType   *string `json:"notification_type" validate:"omitempty" example:"MEMBER"`           // 通知类型
	Status             *string `json:"status" validate:"omitempty"`                                       // 通知组状态
	NotificationConfig *string `json:"notification_config" validate:"omitempty" example:"{\"data\":123}"` // 通知配置
	Description        *string `json:"description" validate:"omitempty"`                                  // 通知组描述
	Remark             *string `json:"remark" validate:"omitempty"`                                       // 备注
}

type GetNotificationGroupListByPageReq struct {
	PageReq
	Name             *string `json:"name" form:"name" validate:"omitempty"`
	NotificationType *string `json:"notification_type" form:"notification_type" validate:"omitempty" example:"MEMBER"` // 通知类型
	Status           *string `json:"status" form:"status" validate:"omitempty"`
}

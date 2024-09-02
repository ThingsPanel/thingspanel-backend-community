package model

type CreateAlarmConfigReq struct {
	Name                string  `json:"name" validate:"required"`
	Description         *string `json:"description" validate:"omitempty"`
	AlarmLevel          string  `json:"alarm_level" validate:"required"`
	NotificationGroupID string  `json:"notification_group_id" validate:"omitempty"`
	CreatedAt           *string `json:"created_at" validate:"omitempty"`
	UpdatedAt           *string `json:"updated_at" validate:"omitempty"`
	TenantID            string  `json:"tenant_id" validate:"omitempty"`
	Remark              *string `json:"remark" validate:"omitempty"`
	Enabled             string  `json:"enabled" validate:"omitempty"`
}

type UpdateAlarmConfigReq struct {
	ID                  string  `json:"id" validate:"required,max=36"`
	Name                *string `json:"name" validate:"omitempty"`
	Description         *string `json:"description" validate:"omitempty"`
	AlarmLevel          *string `json:"alarm_level" validate:"omitempty"`
	NotificationGroupID *string `json:"notification_group_id" validate:"omitempty"`
	CreatedAt           *string `json:"created_at" validate:"omitempty"`
	UpdatedAt           *string `json:"updated_at" validate:"omitempty"`
	TenantID            *string `json:"tenant_id" validate:"omitempty"`
	Remark              *string `json:"remark" validate:"omitempty"`
	Enabled             *string `json:"enabled" validate:"omitempty"`
}

type GetAlarmConfigListByPageReq struct {
	PageReq
	Name       *string `json:"name" form:"name" validate:"omitempty"`
	AlarmLevel *string `json:"alarm_level" form:"alarm_level" validate:"omitempty"`
	Enabled    string  `json:"enabled" form:"enabled" validate:"omitempty"`
	TenantID   string  `json:"tenant_id" validate:"omitempty"`
}

package model

import "time"

type CreateRoleReq struct {
	Name        string  `json:"name" validate:"required,max=255"`         //角色名称
	Description *string `json:"description" validate:"omitempty,max=500"` //角色描述
}

type UpdateRoleReq struct {
	Id          string     `json:"id" validate:"required,max=36"`
	Name        string     `json:"name" validate:"required,max=255"`         //角色名称
	Description *string    `json:"description" validate:"omitempty,max=500"` //角色描述
	UpdatedAt   *time.Time `json:"updated_at" validate:"omitempty"`          //修改时间，前端不用传
	Authority   *string    `json:"authority" validate:"omitempty"`           //权限
}

type GetRoleListByPageReq struct {
	PageReq
	Name *string `json:"name" form:"name" validate:"omitempty,max=255"`
}

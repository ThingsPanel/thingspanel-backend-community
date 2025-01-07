package model

import (
	"encoding/json"
	"time"
)

type CreateUserReq struct {
	AdditionalInfo *json.RawMessage `json:"additional_info" validate:"omitempty,max=10000"` // 附加信息
	Email          string           `json:"email"  validate:"required,email"`               // 邮箱
	Password       string           `json:"password" validate:"required,min=6,max=255"`     // 密码
	Name           *string          `json:"name" validate:"omitempty,min=2,max=50"`         // 姓名
	PhoneNumber    string           `json:"phone_number" validate:"required,max=50"`        // 手机号
	RoleIDs        []string         `json:"userRoles" validate:"omitempty"`                 // 角色ID
	Remark         *string          `json:"remark" validate:"omitempty,max=255"`            // 备注
}

type LoginReq struct {
	Email    string `json:"email" validate:"required"`                  // 邮箱或手机号
	Password string `json:"password" validate:"required,min=6,max=512"` // 密码
	Salt     string `json:"salt"`                                       // 随机盐
}

type LoginRsp struct {
	Token     *string `gorm:"column:token" json:"token"`
	ExpiresIn int64   `json:"expires_in"`
}

type UserListReq struct {
	PageReq
	Email       *string `json:"email" form:"email" validate:"omitempty"`                      // 邮箱
	PhoneNumber *string `json:"phone_number" form:"phone_number" validate:"omitempty,max=50"` // 手机号
	Name        *string `json:"name" form:"name" validate:"omitempty,max=50"`                 // 姓名
	Status      *string `json:"status" form:"status" validate:"omitempty,oneof=N F"`          // 用户状态 F-冻结 N-正常
}

type UpdateUserReq struct {
	ID             string     `json:"id" validate:"required,uuid"`                    // 主键ID
	AdditionalInfo *string    `json:"additional_info" validate:"omitempty,max=10000"` // 附加信息
	Email          *string    `json:"email"  validate:"omitempty,email"`              // 邮箱
	Name           *string    `json:"name" validate:"omitempty,min=2,max=50"`         // 姓名
	PhoneNumber    *string    `json:"phone_number" validate:"omitempty,max=50"`       // 手机号
	Remark         *string    `json:"remark" validate:"omitempty,max=255"`            // 备注
	Status         *string    `json:"status" validate:"omitempty,oneof=N F"`          // 用户状态 F-冻结 N-正常
	Password       *string    `json:"password" validate:"omitempty,max=255"`          // 密码
	UpdatedAt      *time.Time `json:"updated_at" validate:"omitempty"`                // 更新时间
	RoleIDs        []string   `json:"userRoles" validate:"omitempty"`                 // 角色ID
}

type UpdateUserInfoReq struct {
	ID        string     `json:"id" validate:"required"`                      // 主键ID
	Name      *string    `json:"name" validate:"omitempty,min=2,max=50"`      // 姓名
	Remark    *string    `json:"remark" validate:"omitempty,max=255"`         // 备注
	Password  *string    `json:"password" validate:"omitempty,min=6,max=255"` // 密码
	UpdatedAt *time.Time `json:"updated_at" validate:"omitempty"`             // 更新时间
	Salt      string     `json:"salt"`
}

type TransformUserReq struct {
	BecomeUserID string `json:"become_user_id" validate:"required,uuid"` // 用户ID
}

type ResetPasswordReq struct {
	Email      string `json:"email" validate:"required,email"`            // 邮箱
	VerifyCode string `json:"verify_code" validate:"required"`            // 验证码
	Password   string `json:"password" validate:"required,min=6,max=255"` // 新密码
}

type EmailRegisterReq struct {
	Email           string  `json:"email" validate:"required,email"`            // 邮箱
	VerifyCode      string  `json:"verify_code" validate:"required"`            // 验证码
	Password        string  `json:"password" validate:"required,min=6,max=255"` // 新密码
	ConfirmPassword string  `json:"confirm_password" validate:"required,min=6,max=255"`
	PhoneNumber     string  `json:"phone_number" validate:"required"` //手机号码
	PhonePrefix     string  `json:"phone_prefix" validate:"required"` //手机前缀
	Salt            *string `json:"salt" validate:"omitempty"`        // 随机盐
}

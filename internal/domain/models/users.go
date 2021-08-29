package models

import "database/sql"

type User struct {
	ID              string         `json:"id" gorm:"primaryKey,size:36"`
	CreatedAt       int64          `json:"created_at"`
	UpdatedAt       int64          `json:"updated_at"`
	Enabled         sql.NullString `json:"enabled" gorm:"size:5"`
	AdditionalInfo  sql.NullString `json:"additional_info" gorm:"type:longtext"`
	Authority       sql.NullString `json:"authority"`
	CustomerID      sql.NullString `json:"customer_id" gorm:"size:36"`
	Email           sql.NullString `json:"email"`
	Password        sql.NullString `json:"password"`
	Name            sql.NullString `json:"name"`
	FirstName       sql.NullString `json:"first_name"`
	LastName        sql.NullString `json:"last_name"`
	SearchText      sql.NullString `json:"search_text"`
	EmailVerifiedAt sql.NullInt64  `json:"email_verified_at"`
	Mobile          sql.NullString `json:"mobile" gorm:"size:20"`
	Remark          sql.NullString `json:"remark" gorm:"size:100"`
	IsAdmin         sql.NullInt64  `json:"is_admin"`
	BusinessID      sql.NullString `json:"business_id" gorm:"size:36"` // 业务id
	WXOpenID        sql.NullString `json:"wx_openid" gorm:"size:50"`   // 微信openid
	WXUnionID       sql.NullString `json:"wx_unionid" gorm:"size:50"`  // 微信unionid
}

// todo - what does $table->rememberToken() map to? https://github.com/ThingsPanel/core/blob/94cbd2a917e35bdd44a65f46a52e4f7558a1dcb8/database/migrations/2021_08_15_230802_create_users_table.php#L32

package models

type Users struct {
	ID              string `json:"id" gorm:"primaryKey,size:36"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	Enabled         string `json:"enabled" gorm:"size:5"`
	AdditionalInfo  string `json:"additional_info" gorm:"type:longtext"`
	Authority       string `json:"authority"`
	CustomerID      string `json:"customer_id" gorm:"size:36"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	Name            string `json:"name"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	SearchText      string `json:"search_text"`
	EmailVerifiedAt int64  `json:"email_verified_at"`
	RememberToken   string `json:"remember_token" gorm:"size:100"`
	Mobile          string `json:"mobile" gorm:"size:20"`
	Remark          string `json:"remark" gorm:"size:100"`
	IsAdmin         int64  `json:"is_admin"`
	BusinessId      string `json:"business_id" gorm:"size:36"` // 业务id
	WXOpenid        string `json:"wx_openid" gorm:"size:50"`   // 微信openid
	WXUnionid       string `json:"wx_unionid" gorm:"size:50"`  // 微信unionid
}

func (Users) TableName() string {
	return "users"
}

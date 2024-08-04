package model

type UsersRes struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PhoneNum   string `json:"phone_num"`
	Email      string `json:"email"`
	Authority  string `json:"authority"`
	TenantID   string `json:"tenant_id"`
	Remark     string `json:"remark"`
	CreateTime string `json:"create_time"`
}

type UsersUpdateReq struct {
	Name           string  `json:"name"`
	AdditionalInfo *string `json:"additional_info"`
}

type UsersUpdatePasswordReq struct {
	OldPassword string `json:"old_password" gorm:"old_password" validate:"required"`
	Password    string `json:"password"  gorm:"password" validate:"required"`
	Salt        string `json:"salt" gorm:"salt"`
}

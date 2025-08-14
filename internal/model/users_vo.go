package model

type UsersRes struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	PhoneNum       string `json:"phone_num"`
	Email          string `json:"email"`
	Authority      string `json:"authority"`
	TenantID       string `json:"tenant_id"`
	Remark         string `json:"remark"`
	CreateTime     string `json:"create_time"`
	AdditionalInfo string `json:"additional_info"`
	AvatarURL      string `json:"avatar_url"`
}

type UsersUpdateReq struct {
	Name            string                `json:"name"`
	AdditionalInfo  *string               `json:"additional_info"`
	PhoneNumber     *string               `json:"phone_number"`
	PhonePrefix     *string               `json:"phone_prefix"`
	Organization    *string               `json:"organization" validate:"omitempty,max=200"`
	Timezone        *string               `json:"timezone" validate:"omitempty,max=50"`
	DefaultLanguage *string               `json:"default_language" validate:"omitempty,max=10"`
	Address         *UpdateUserAddressReq `json:"address" validate:"omitempty"`
	AvatarURL       *string               `json:"avatar_url" validate:"omitempty,max=500"`
}

type UsersUpdatePasswordReq struct {
	OldPassword string `json:"old_password" gorm:"old_password" validate:"required"`
	Password    string `json:"password"  gorm:"password" validate:"required"`
	Salt        string `json:"salt" gorm:"salt"`
}

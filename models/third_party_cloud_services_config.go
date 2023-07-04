package models

const (
	NotificationConfigType_Message = 1
	NotificationConfigType_Email   = 2
)

const (
	NotificationCloudType_Ali = 1
)

type ThirdPartyCloudServicesConfig struct {
	Id         string `json:"id" gorm:"primaryKey"`
	NoticeType int    `json:"notice_type"`
	Config     string `json:"config"`
	Status     int    `json:"status"`
}

func (t *ThirdPartyCloudServicesConfig) TableName() string {
	return "third_party_cloud_services_config"
}

type CloudServicesConfig_Ali struct {
	CloudType       int    `json:"cloud_type" valid:"Required"`
	AccessKeyId     string `json:"access_key_id" valid:"Required"`
	AccessKeySecret string `json:"access_key_secret" valid:"Required"`
	Endpoint        string `json:"endpoint" valid:"Required"`
	SignName        string `json:"sign_name" valid:"Required"`
	TemplateCode    string `json:"template_code" valid:"Required"`
}

type CloudServicesConfig_Email struct {
	Host         string `json:"host" valid:"Required"`
	Port         int    `json:"port" valid:"Required"`
	FromPassword string `json:"from_password" valid:"Required"`
	FromEmail    string `json:"from_email" valid:"Required"`
	SSL          bool   `json:"ssl" valid:"Required"`
}

type CloudServicesConfig_Tencent struct {
}

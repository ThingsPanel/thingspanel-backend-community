package models

import (
	"ThingsPanel-Go/initialize/psql"
)

const (
	NotificationConfigType_Message          = 1 // 短信告警信息
	NotificationConfigType_Email            = 2
	NotificationConfigType_VerificationCode = 3 // 短信验证码
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

func NotificationConfigByNoticeTypeAndStatus(noticeType, status int) (c ThirdPartyCloudServicesConfig, err error) {

	err = psql.Mydb.
		Model(&ThirdPartyCloudServicesConfig{}).
		Where("notice_type = ? AND status = ? ", noticeType, status).
		First(&c).Error

	return c, err
}

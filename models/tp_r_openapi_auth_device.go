package models

type TpROpenapiAuthDevice struct {
	ID              string `json:"id" gorm:"primarykey"`
	TpOpenapiAuthId string `json:"tp_openapi_auth_id" gorm:"size:36"` //名称
	TpDeviceId      string `json:"tp_device_id" gorm:"size:36"`
}

func (t *TpROpenapiAuthDevice) TableName() string {
	return "tp_r_openapi_auth_device"
}

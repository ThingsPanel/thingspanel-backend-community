package models

type TpROpenapiAuthApi struct {
	ID              string `json:"id" gorm:"primarykey"`
	TpOpenapiAuthId string `json:"tp_openapi_auth_id" gorm:"size:36"` //名称
	TpApiId         string `json:"tp_api_id" gorm:"size:36"`
}

func (t *TpROpenapiAuthApi) TableName() string {
	return "tp_r_openapi_auth_api"
}

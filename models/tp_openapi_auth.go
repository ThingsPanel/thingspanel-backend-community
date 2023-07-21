package models

type TpOpenapiAuth struct {
	ID                string `json:"id" gorm:"primarykey"`
	TenantId          string `json:"tenant_id" gorm:"size:36"`          //租户id
	Name              string `json:"name" gorm:"size:50"`               // 名称
	AppKey            string `json:"app_key" gorm:"size:500"`           //key
	SecretKey         string `json:"secret_key" gorm:"size:500"`        //密钥
	SignatureMode     string `json:"signature_mode" gorm:"size:50"`     //签名方式 MD5 SHA256
	IpWhitelist       string `json:"ip_whitelist" gorm:"size:20"`       //ip白名单
	DeviceAccessScope string `json:"device_access_scope" gorm:"size:2"` //设备访问范围 1-全部 2-部分
	ApiAccessScope    string `json:"api_access_scope" gorm:"size:2"`    //接口访问范围 1-全部 2-部分
	Description       string `json:"description" gorm:"size:500"`       //描述
	CreatedAt         int64  `json:"created_at"`                        //描述
	Remark            string `json:"remark" gorm:"size:500"`            //备注

}

func (t *TpOpenapiAuth) TableName() string {
	return "tp_openapi_auth"
}

package models

type TpDataServicesConfig struct {
	Id            string `json:"id" gorm:"primaryKey"`
	Name          string `json:"name,omitempty"`           //名称
	AppKey        string `json:"app_key,omitempty"`        //appkey
	SecretKey     string `json:"secret_key,omitempty"`     //密钥
	SignatureMode string `json:"signature_mode,omitempty"` //签名方式
	IpWhitelist   string `json:"ip_whitelist,omitempty"`   //ip白名单
	DataSql       string `json:"data_sql,omitempty"`       //数据sql
	ApiFlag       string `json:"api_flag,omitempty"`       //api标识
	TimeInterval  int64  `json:"time_interval,omitempty"`  //时间间隔
	EnableFlag    string `json:"enable_flag,omitempty"`    //启用标识
	Description   string `json:"description,omitempty"`    //描述
	CreatedAt     int64  `json:"created_at,omitempty"`
	Remark        string `json:"remark,omitempty" gorm:"size:36"`
}

func (TpDataServicesConfig) TableName() string {
	return "tp_data_services_config"
}

const (
	Appkey_Length    int = 10
	SecretKey_Length int = 12
)

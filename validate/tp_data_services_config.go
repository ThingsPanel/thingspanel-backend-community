package valid

import "ThingsPanel-Go/models"

type TpDataServicesConfigPaginationValidate struct {
	CurrentPage   int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage       int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Name          string `json:"name"  alias:"名称" `           //名称
	AppKey        string `json:"app_key"  alias:"appkey"`     //appkey
	SecretKey     string `json:"secret_key" alias:"密钥"`       //密钥
	SignatureMode string `json:"signature_mode" alias:"签名方式"` //签名方式
	IpWhitelist   string `json:"ip_whitelist" alias:"ip白名单"`  //ip白名单
	DataSql       string `json:"data_sql" alias:"数据sql"`      //数据sql
	ApiFlag       string `json:"api_flag" alias:"api标识"`      //api标识
	TimeInterval  int64  `json:"time_interval" alias:"时间间隔"`  //时间间隔
	EnableFlag    string `json:"enable_flag" alias:"启用标识"`    //启用标识
	Description   string `json:"description" alias:"描述"`      //描述
}

type RspTpDataServicesConfigPaginationValidate struct {
	CurrentPage int                           `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                           `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpDataServicesConfig `json:"data" alias:"返回数据"`
	Total       int64                         `json:"total" alias:"总数" valid:"Max(10000)"`
}

type GetDataPaginationValidate struct {
	CurrentPage int `json:"current_page"  alias:"当前页" valid:"Min(1)"`
	PerPage     int `json:"per_page"  alias:"每页页数" valid:"Max(10000)"`
}
type RspGetDataPaginationValidate struct {
	CurrentPage int                      `json:"current_page"  alias:"当前页" valid:"Min(1)"`
	PerPage     int                      `json:"per_page"  alias:"每页页数" valid:"Max(10000)"`
	Data        []map[string]interface{} `json:"data" alias:"返回数据"`
	Total       int64                    `json:"total" alias:"总数"`
}

type AddTpDataServicesConfigValidate struct {
	Name          string `json:"name"  alias:"名称" valid:"Required"`            //名称
	SignatureMode string `json:"signature_mode" alias:"签名方式" valid:"Required"` //签名方式
	IpWhitelist   string `json:"ip_whitelist" alias:"ip白名单"`                   //ip白名单
	DataSql       string `json:"data_sql" alias:"数据sql"`                       //数据sql
	ApiFlag       string `json:"api_flag" alias:"api标识" valid:"Required"`      //api标识
	TimeInterval  int64  `json:"time_interval" alias:"时间间隔"`                   //时间间隔
	EnableFlag    string `json:"enable_flag" alias:"启用标识"`                     //启用标识
	Description   string `json:"description" alias:"描述"`                       //描述
	CreatedAt     int64  `json:"created_at"`
	Remark        string `json:"remark"`
}

type EditTpDataServicesConfigValidate struct {
	Id            string `json:"id" alias:"id" valid:"Required"`
	Name          string `json:"name"  alias:"名称"`             //名称
	SignatureMode string `json:"signature_mode" alias:"签名方式"`  //签名方式
	IpWhitelist   string `json:"ip_whitelist" alias:"ip白名单"`   //ip白名单
	DataSql       string `json:"data_sql" alias:"数据sql"`       //数据sql
	ApiFlag       string `json:"api_flag" alias:"api标识" valid` //api标识
	TimeInterval  int64  `json:"time_interval" alias:"时间间隔"`   //时间间隔
	EnableFlag    string `json:"enable_flag" alias:"启用标识"`     //启用标识
	Description   string `json:"description" alias:"描述"`       //描述
	Remark        string `json:"remark"`
}

type TpDataServicesConfigIdValidate struct {
	Id string `json:"id" gorm:"primaryKey" alias:"id" valid:"Required"`
}

type TpDataServicesConfigQuizeValidate struct {
	DataSql string `json:"data_sql"  alias:"Sql" valid:"Required"`
}

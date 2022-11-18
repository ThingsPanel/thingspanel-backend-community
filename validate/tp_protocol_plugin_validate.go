package valid

import "ThingsPanel-Go/models"

type TpProtocolPluginValidate struct {
	Id             string `json:"id"  gorm:"primaryKey"`
	Name           string `json:"name,omitempty" alias:"协议插件名称"`
	ProtocolType   string `json:"protocol_type,omitempty" alias:"协议插件类型"`
	AccessAddress  string `json:"access_address,omitempty" alias:"接入地址"`
	HttpAddress    string `json:"http_address,omitempty" alias:"http接口地址"`
	SubTopicPrefix string `json:"sub_topic_prefix,omitempty" alias:"订阅主题前缀"`
	CreatedAt      int64  `json:"created_at,omitempty" alias:"创建时间"`
	Description    string `json:"description,omitempty" alias:"描述"`
}

type AddTpProtocolPluginValidate struct {
	Name           string `json:"name,omitempty" alias:"协议插件名称"  valid:"Required;MaxSize(36)"`
	ProtocolType   string `json:"protocol_type,omitempty" alias:"协议插件类型"  valid:"Required;MaxSize(36)"`
	AccessAddress  string `json:"access_address,omitempty" alias:"接入地址"  valid:"Required;MaxSize(99)"`
	HttpAddress    string `json:"http_address,omitempty" alias:"http接口地址" valid:"MaxSize(99)"`
	SubTopicPrefix string `json:"sub_topic_prefix,omitempty" alias:"订阅主题前缀"  valid:"MaxSize(99)"`
	CreatedAt      int64  `json:"created_at,omitempty" alias:"创建时间"`
	Description    string `json:"description,omitempty" alias:"描述"  valid:"MaxSize(255)"`
}

type TpProtocolPluginPaginationValidate struct {
	CurrentPage  int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage      int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	ProtocolType string `json:"protocol_type,omitempty" alias:"协议类型" valid:"MaxSize(99)"`
	Id           string `json:"id,omitempty" alias:"Id" valid:"MaxSize(36)"`
}

type RspTpProtocolPluginPaginationValidate struct {
	CurrentPage int                       `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                       `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpProtocolPlugin `json:"data" alias:"返回数据"`
	Total       int64                     `json:"total" alias:"总数" valid:"Max(10000)"`
}

type TpProtocolPluginIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}

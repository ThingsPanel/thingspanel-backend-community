package valid

import "ThingsPanel-Go/models"

type TpLocalVisPluginPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id" alias:"id" `
}

type RspTpLocalVisPluginPaginationValidate struct {
	CurrentPage int                       `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                       `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpLocalVisPlugin `json:"data" alias:"返回数据"`
	Total       int64                     `json:"total" alias:"总数" valid:"Max(10000)"`
}

type AddTpLocalVisPluginValidate struct {
	PluginUrl string `json:"plugin_url" alias:"插件地址" valid:"Required"`
	Id        string `json:"id" alias:"id" valid:"Required"`
	Remark    string `json:"remark" alias:"备注" `
}

type EditTpLocalVisPluginValidate struct {
	Id        string `json:"id" alias:"id" valid:"Required"`
	PluginUrl string `json:"plugin_url" alias:"插件地址" valid:"Required"`
}

type TpLocalVisPluginIdValidate struct {
	Id string `json:"id" gorm:"primaryKey" alias:"id" valid:"Required"`
}

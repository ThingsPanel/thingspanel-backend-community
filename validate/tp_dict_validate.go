package valid

import "ThingsPanel-Go/models"

type TpDictValidate struct {
	ID        string `json:"id" alias:"ID" valid:"MaxSize(36)"`
	DictCode  string `json:"dict_code,omitempty" valid:"MaxSize(36)"`
	DictValue string `json:"dict_value,omitempty" valid:"MaxSize(99)"`
	Describe  string `json:"describe,omitempty" valid:"MaxSize(99)"`
	CreatedAt int64  `json:"created_at,omitempty" alias:"创建时间" `
}

type AddTpDictValidate struct {
	DictCode  string `json:"dict_code,omitempty" valid:"MaxSize(36)"`
	DictValue string `json:"dict_value,omitempty" valid:"MaxSize(99)"`
	Describe  string `json:"describe,omitempty" valid:"MaxSize(99)"`
	CreatedAt int64  `json:"created_at,omitempty" alias:"创建时间" `
}

type TpDictPaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	DictCode    string `json:"dict_code" alias:"编码" valid:"MaxSize(36)"`
}

type RspTpDictPaginationValidate struct {
	CurrentPage int             `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int             `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.TpDict `json:"data" alias:"返回数据"`
	Total       int64           `json:"total" alias:"总数" valid:"Max(10000)"`
}

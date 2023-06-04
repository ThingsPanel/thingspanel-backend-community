package valid

type TpVisPluginPaginationValidate struct {
	CurrentPage int `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
}

type RspTpVisPluginPaginationValidate struct {
	CurrentPage int                      `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                      `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []map[string]interface{} `json:"data" alias:"返回数据"`
	Total       int64                    `json:"total" alias:"总数" valid:"Max(10000)"`
}

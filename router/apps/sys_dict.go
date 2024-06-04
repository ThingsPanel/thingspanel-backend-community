package apps

import (
	"project/api"

	"github.com/gin-gonic/gin"
)

type Dict struct {
}

func (d *Dict) InitDict(Router *gin.RouterGroup) {
	dictapi := Router.Group("dict")
	{
		// 新增字典列
		dictapi.POST("column", api.Controllers.DictApi.CreateDictColumn)

		// 新增字典多语言
		dictapi.POST("language", api.Controllers.CreateDictLanguage)

		// 枚举查询接口
		dictapi.GET("enum", api.Controllers.DictApi.GetDict)

		// 字典列表分页查询
		dictapi.GET("", api.Controllers.DictApi.GetDictLisyByPage)

		// 字典多语言列表查询
		dictapi.GET("language/:id", api.Controllers.GetDictLanguage)

		// 删除字典
		dictapi.DELETE("column/:id", api.Controllers.DictApi.DeleteDictColumn)

		// 删除字典多语言
		dictapi.DELETE("language/:id", api.Controllers.DictApi.DeleteDictLanguage)

		// 获取协议服务下拉菜单
		dictapi.GET("protocol/service", api.Controllers.DictApi.GetProtocolAndService)
	}
}

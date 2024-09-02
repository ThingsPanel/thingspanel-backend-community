package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type AttributeData struct{}

func (t *AttributeData) InitAttributeData(Router *gin.RouterGroup) {
	attributedataapi := Router.Group("attribute/datas")
	{
		// 设备属性列表查询
		attributedataapi.GET(":id", api.Controllers.AttributeDataApi.GetDataList)

		// 获取属性下发记录（分页）
		attributedataapi.GET("set/logs", api.Controllers.AttributeDataApi.GetAttributeSetLogsDataListByPage)

		// 删除
		attributedataapi.DELETE(":id", api.Controllers.AttributeDataApi.DeleteData)

		// 下发属性
		attributedataapi.POST("pub", api.Controllers.AttributeDataApi.AttributePutMessage)

		// 向设备请求属性
		attributedataapi.GET("get", api.Controllers.AttributeDataApi.AttributeGetMessage)
	}
}

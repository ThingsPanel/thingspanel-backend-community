package apps

import (
	"project/internal/api"

	"github.com/gin-gonic/gin"
)

type User struct {
}

func (*User) InitUser(Router *gin.RouterGroup) {
	userapi := Router.Group("user")
	{
		// 个人信息管理
		userapi.GET("detail", api.Controllers.UserApi.GetUserDetail)
		userapi.PUT("update", api.Controllers.UserApi.UpdateUsers)
		userapi.GET("logout", api.Controllers.UserApi.Logout)
		userapi.GET("refresh", api.Controllers.UserApi.RefreshToken)

		// 用户管理
		userapi.GET("", api.Controllers.UserApi.GetUserListByPage)
		userapi.POST("", api.Controllers.UserApi.CreateUser)
		userapi.PUT("", api.Controllers.UserApi.UpdateUser)
		userapi.DELETE(":id", api.Controllers.UserApi.DeleteUser)
		userapi.GET(":id", api.Controllers.UserApi.GetUser)
		userapi.POST("transform", api.Controllers.UserApi.TransformUser)

	}
}

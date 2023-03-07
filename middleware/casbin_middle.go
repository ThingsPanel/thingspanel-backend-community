package middleware

import (
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	"strings"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
	"github.com/beego/beego/v2/core/logs"
	context2 "github.com/beego/beego/v2/server/web/context"
)

// 采用casbin，如果资源在表中，就需要校验，不在表中不做校验
// RBAC：用户-角色-功能-资源-动作
func CasbinMiddle() {
	logs.Info("casbin:进入中间件")
	var CasbinService services.CasbinService
	//需要验证的url
	var filterUrl = func(ctx *context.Context) {
		url := strings.TrimLeft(ctx.Input.URL(), "/")
		logs.Info("casbin:url--", utils.ReplaceUserInput(url))
		// 判断接口是否需要校验
		isVerify := CasbinService.GetUrl(url)
		if isVerify {
			logs.Info("casbin:需要校验")
			authorization := ctx.Request.Header["Authorization"][0]
			userToken := authorization[7:]
			userClaims, err := response.ParseCliamsToken(userToken)
			if err == nil {
				name := userClaims.Name
				logs.Info("casbin:username--", name)
				isSuccess := CasbinService.Verify(name, url)
				if !isSuccess {
					response.SuccessWithMessage(999, "You don't have access.", (*context2.Context)(ctx))
				}
			}
		}

	}
	adapter.InsertFilter("/api/*", adapter.BeforeRouter, filterUrl)
}

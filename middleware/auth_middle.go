package middleware

import (
	response "ThingsPanel-Go/utils"
	"fmt"
	"strings"

	cache "ThingsPanel-Go/initialize/cache"
	jwt "ThingsPanel-Go/utils"
	c "context"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
	context2 "github.com/beego/beego/v2/server/web/context"
)

// AuthMiddle 中间件
func AuthMiddle() {
	//不需要验证的url
	noLogin := map[string]interface{}{
		"/api/system/logo/index": 0,
		"api/open/data":          0,
		"api/auth/login":         0,
		"api/auth/refresh":       0,
		"api/auth/register":      1,
		"/ws":                    2,
	}
	var filterLogin = func(ctx *context.Context) {
		url := strings.TrimLeft(ctx.Input.URL(), "/")
		if !isAuthExceptUrl(strings.ToLower(url), noLogin) {
			//获取TOKEN
			if len(ctx.Request.Header["Authorization"]) == 0 {
				response.SuccessWithMessage(401, "Unauthorized", (*context2.Context)(ctx))
				return
			}
			authorization := ctx.Request.Header["Authorization"][0]
			userToken := authorization[7:len(authorization)]
			_, err := jwt.ParseCliamsToken(userToken)
			if err != nil {
				// 异常
				response.SuccessWithMessage(401, "Unauthorized", (*context2.Context)(ctx))
				return
			}
			s, _ := cache.Bm.IsExist(c.TODO(), userToken)
			if !s {
				response.SuccessWithMessage(401, "Unauthorized", (*context2.Context)(ctx))
				return
			}
		}
	}
	adapter.InsertFilter("/api/*", adapter.BeforeRouter, filterLogin)
}

func isAuthExceptUrl(url string, m map[string]interface{}) bool {
	urlArr := strings.Split(url, "/")
	if len(urlArr) > 3 {
		url = fmt.Sprintf("%s/%s/%s", urlArr[0], urlArr[1], urlArr[2])
	}
	_, ok := m[url]
	if ok {
		return true
	}
	return false
}

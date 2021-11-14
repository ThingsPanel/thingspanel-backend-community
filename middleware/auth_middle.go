package middleware

import (
	"strconv"

	"github.com/ThingsPanel/ThingsPanel-Go/domain/models"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/services"
	"github.com/ThingsPanel/ThingsPanel-Go/global"

	"fmt"
	"strings"

	beego "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
)

// AuthMiddle 中间件
func AuthMiddle() {
	//不需要验证的url
	authExcept := map[string]interface{}{
		"api/auth/login": 0,
	}
	//登录认证中间件过滤器
	var filterLogin = func(ctx *context.Context) {
		url := strings.TrimLeft(ctx.Input.URL(), "/")
		//需要进行登录验证
		if !isAuthExceptUrl(strings.ToLower(url), authExcept) {
			//验证是否登录
			loginUser, isLogin := isLogin(ctx)

			if !isLogin {
				response.ErrorWithMessageAndUrl("未登录", "/api/auth/login", (*context2.Context)(ctx))
				return
			}

			//验证，是否有权限访问
			var adminUserService services.AdminUserService
			if loginUser.Id != 1 && !adminUserService.AuthCheck(url, authExcept, loginUser) {
				errorBackURL := global.URL_CURRENT
				if ctx.Request.Method == "GET" {
					errorBackURL = ""
				}
				response.ErrorWithMessageAndUrl("无权限", errorBackURL, (*context2.Context)(ctx))
				return
			}
		}

		checkAuth, _ := strconv.Atoi(ctx.Request.PostForm.Get("check_auth"))

		if checkAuth == 1 {
			response.Success((*context2.Context)(ctx))
			return
		}

	}

	beego.InsertFilter("/api/*", beego.BeforeRouter, filterLogin)
}

//判断是否是不需要验证登录的url,只针对admin模块路由的判断
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

//是否登录
func isLogin(ctx *context.Context) (*models.User, bool) {
	loginUser, ok := ctx.Input.Session(global.LOGIN_USER).(models.User)
	if !ok {
		loginUserIDStr := ctx.GetCookie(global.LOGIN_USER_ID)
		loginUserToken := ctx.GetCookie(global.LOGIN_USER_TOKEN)
		if loginUserIDStr != "" && loginUserToken != "" {
			loginUserID, _ := strconv.Atoi(loginUserIDStr)
			var userService services.userService
			loginUserPointer := userService.GetUserById(loginUserID)
			if loginUserPointer != nil && loginUserPointer.GetSignStrByAdminUser((*context2.Context)(ctx)) == loginUserIDSign {
				ctx.Output.Session(global.LOGIN_USER, *loginUserPointer)
				return loginUserPointer, true
			}
		}
		return nil, false
	}

	return &loginUser, true
}

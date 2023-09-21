package middleware

import (
	utils "ThingsPanel-Go/utils"
	"errors"
	"fmt"
	"strings"

	"ThingsPanel-Go/initialize/redis"

	"ThingsPanel-Go/services"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
	context2 "github.com/beego/beego/v2/server/web/context"
)

const ErrUnauthorized = "Unauthorized"

// AuthMiddle 中间件
func AuthMiddle() {
	// 不 需要验证的url
	noLogin := map[string]interface{}{
		"api/plugin/device/sub-device-detail": 0,
		"api/plugin/register":                 0,
		"api/plugin/device/config":            0,
		"api/plugin/all_device/config":        0,
		"api/system/logo/index":               0,
		"api/open/data":                       0,
		"api/auth/login":                      0,
		"api/auth/refresh":                    0,
		"api/auth/register":                   1,
		"api/auth/tenant/register":            0,
		"/ws":                                 2,
		"api/ota/download":                    0,
	}
	var filterLogin = func(ctx *context.Context) {
		url := strings.TrimLeft(ctx.Input.URL(), "/")
		if !isAuthExceptUrl(strings.ToLower(url), noLogin) {
			//获取TOKEN
			userToken, err := GetToken(ctx)
			if err != nil {
				utils.SuccessWithMessage(401, err.Error(), (*context2.Context)(ctx))
				return
			}
			// 解析token
			userMsg, err := utils.ParseCliamsToken(userToken)
			if err != nil {
				utils.SuccessWithMessage(401, ErrUnauthorized, (*context2.Context)(ctx))
				return
			}
			// 判断token是否存在
			if redis.GetStr(userToken) != "1" {
				utils.SuccessWithMessage(401, ErrUnauthorized, (*context2.Context)(ctx))
				return
			}
			// 设置用户ID
			ctx.Input.SetData("user_id", userMsg.ID)
			// 设置用户权限
			if err := SetUserAuth(ctx, userMsg); err != nil {
				utils.SuccessWithMessage(401, err.Error(), (*context2.Context)(ctx))
				return
			}
		}
	}
	adapter.InsertFilter("/api/*", adapter.BeforeRouter, filterLogin)
	adapter.InsertFilter("/ws/*", adapter.BeforeRouter, filterLogin)
}

// 不需要授权的url返回true
func isAuthExceptUrl(url string, m map[string]interface{}) bool {
	urlArr := strings.Split(url, "/")
	// url大于4个长度只判断前四个是否在不需授权map中
	if len(urlArr) > 4 {
		url = fmt.Sprintf("%s/%s/%s/%s", urlArr[0], urlArr[1], urlArr[2], urlArr[3])
	}
	_, ok := m[url]
	return ok
}

//获取token
func GetToken(ctx *context.Context) (string, error) {
	authorization := ctx.Input.Header("Authorization")
	if len(authorization) == 0 {
		return "", errors.New(ErrUnauthorized)
	}
	if !strings.HasPrefix(authorization, "Bearer ") {
		return "", errors.New(ErrUnauthorized)
	}
	userToken := authorization[7:]
	return userToken, nil
}

// 设置用户权限和租户id
func SetUserAuth(ctx *context.Context, userClaims *utils.UserClaims) (err error) {
	// 通过用户id获取用户权限
	var userService *services.UserService
	authority, tenant_id, err := userService.GetUserAuthorityById(userClaims.ID)
	if err != nil {
		return err
	} else if authority == "" {
		return errors.New("用户权限为空")
	}
	ctx.Input.SetData("tenant_id", tenant_id)
	ctx.Input.SetData("authority", authority)
	return nil
}

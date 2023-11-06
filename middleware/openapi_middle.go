package middleware

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
	"github.com/beego/beego/v2/core/logs"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/spf13/viper"
	"github.com/thinkeridea/go-extend/exnet"
)

// OpenapiMiddle 中间件
func OpenapiMiddle() {
	adapter.InsertFilter("/openapi/*", adapter.BeforeRouter, openapiFilter)
}

var openapisercie services.OpenApiService
var tpDataServicesConfig services.TpDataServicesConfig
var dataServicesApiList []string = []string{
	"/openapi/v1/data/services/http/share",
}

// openapi 访问过滤
func openapiFilter(ctx *context.Context) {
	fmt.Println("请求接口：", ctx.Request.URL.Path)

	signFlag := viper.GetBool("openapi.sign")
	// 判断 请求头中是否携带X-OpenAPI-Timestamp
	if timestamp := ctx.Request.Header.Get("X-OpenAPI-Timestamp"); timestamp == "" {
		utils.SuccessWithMessage(401, "时间戳不存在", (*context2.Context)(ctx))
		return
	} else {

		if signFlag {
			//校验X-OpenAPI-Timestamp时间
			curTimestamp := time.Now().Unix()
			reqTimestamp, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				logs.Error("时间戳格式错误", err.Error())
			}
			// 过期时间
			expirTime := viper.GetInt64("openapi.timestamp")
			if (curTimestamp - reqTimestamp) > (expirTime * 60) {
				utils.SuccessWithMessage(401, "访问权限超时", (*context2.Context)(ctx))
				return
			}
		}
		AppKey := ctx.Request.Header.Get("X-OpenAPI-AppKey")
		if AppKey == "" {
			utils.SuccessWithMessage(401, "AppKey不存在", (*context2.Context)(ctx))
			return
		}
		var isDataServiceApi bool = false
		// 判断是否为数据服务接口
		for _, api := range dataServicesApiList {
			if strings.Contains(ctx.Request.URL.Path, api) {
				// 判断是否为数据服务接口
				isDataServiceApi = true
				break
			}
		}
		if isDataServiceApi {
			fmt.Println("数据服务接口")
			// 获取数据服务配置
			dataServicesConfig, err := tpDataServicesConfig.GetTpDataServicesConfigByAppKey(AppKey)
			if err != nil {
				utils.SuccessWithMessage(401, "未授权AppKey,非法访问", (*context2.Context)(ctx))
				return
			}
			if len(dataServicesConfig.IpWhitelist) > 0 {
				clientIp := RemoteIp(ctx.Request)
				ipWhitelistStr := dataServicesConfig.IpWhitelist
				ipWhitelist := strings.Split(ipWhitelistStr, "|")
				ipExist := false
				for _, ip := range ipWhitelist {
					if clientIp == ip {
						ipExist = true
					}
				}
				if !ipExist {
					utils.SuccessWithMessage(401, "未授权IP,非法访问", (*context2.Context)(ctx))
					return
				}
			}
			if signFlag {
				//验签X-OpenAPI-Signature
				Signature := ctx.Request.Header.Get("X-OpenAPI-Signature")
				verifySignature := openapisercie.GenerateAppSecretSignatureHash(dataServicesConfig.SecretKey, dataServicesConfig.SignatureMode, timestamp)
				if Signature != verifySignature {
					utils.SuccessWithMessage(401, "非法签名", (*context2.Context)(ctx))
					return
				}
			}
		} else {
			// 判断ip是否在ip白名单
			openapiInfo := openapisercie.GetOpenApiAuth(AppKey)
			if len(openapiInfo.IpWhitelist) > 0 {
				clientIp := RemoteIp(ctx.Request)
				ipWhitelistStr := openapiInfo.IpWhitelist
				ipWhitelist := strings.Split(ipWhitelistStr, "|")
				ipExist := false
				for _, ip := range ipWhitelist {
					if clientIp == ip {
						ipExist = true
					}
				}
				if !ipExist {
					utils.SuccessWithMessage(401, "未授权IP,非法访问", (*context2.Context)(ctx))
					return
				}
			}
			if signFlag {
				//验签X-OpenAPI-Signature
				Signature := ctx.Request.Header.Get("X-OpenAPI-Signature")
				verifySignature := openapisercie.GenerateAppSecretSignatureHash(openapiInfo.SecretKey, openapiInfo.SignatureMode, timestamp)
				if Signature != verifySignature {
					utils.SuccessWithMessage(401, "非法签名", (*context2.Context)(ctx))
					return
				}
			}
			//接口权限验证
			if !isAuthUrl(ctx, openapiInfo) {
				utils.SuccessWithMessage(401, "无接口权限", (*context2.Context)(ctx))
				return
			}

			// 设置 tenant_id
			ctx.Input.SetData("tenant_id", openapiInfo.TenantId)
			ctx.Input.SetData("app_key", openapiInfo.AppKey)
			ctx.Input.SetData("device_access_scope", openapiInfo.DeviceAccessScope)

		}
	}
}

// 判断是否为openapi 授权接口
func isAuthUrl(ctx *context.Context, openapiInfo models.TpOpenapiAuth) bool {
	//如果范围为全部 则跳过
	if openapiInfo.ApiAccessScope == "1" {
		return true
	}
	authApis := openapisercie.GetApiListByAuthId(openapiInfo.ID)
	reqUrl := ctx.Request.URL.RawPath
	for _, authapi := range authApis {
		if authapi.Url == reqUrl {
			return true
		}
	}
	return false
}

// RemoteIp 获取客户IP
func RemoteIp(req *http.Request) string {
	remoteAddr := req.RemoteAddr
	if ip := exnet.ClientPublicIP(req); ip != "" {
		remoteAddr = ip
	} else if ip := exnet.ClientIP(req); ip != "" {
		remoteAddr = ip
	} else if ip := req.Header.Get("X-Real-IP"); ip != "" {
		remoteAddr = ip
	} else if ip = req.Header.Get("X-Forwarded-For"); ip != "" {
		remoteAddr = ip
	} else {
		remoteAddr, _, _ = net.SplitHostPort(remoteAddr)
	}

	if remoteAddr == "::1" {
		remoteAddr = "127.0.0.1"
	}

	return remoteAddr
}

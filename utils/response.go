package utils

import (
	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/server/web/context"
)

// Response 响应参数结构体
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// Result 返回结果辅助函数
func Result(code int, msg string, data interface{}, header map[string]string, ctx *context.Context) {
	if ctx.Input.IsPost() {
		result := Response{
			Code:    code,
			Message: msg,
			Data:    data,
		}
		if len(header) > 0 {
			for k, v := range header {
				ctx.Output.Header(k, v)
			}
		}
		ctx.Output.JSON(result, false, false)
		panic(adapter.ErrAbort)
	}
}

// Success 成功、普通返回
func Success(code int, ctx *context.Context) {
	Result(code, "操作成功", []string{}, map[string]string{}, ctx)
}

// SuccessWithMessage 成功、返回自定义信息
func SuccessWithMessage(code int, msg string, ctx *context.Context) {
	Result(code, msg, []string{}, map[string]string{}, ctx)
}

// SuccessWithDetailed 成功、返回所有自定义信息
func SuccessWithDetailed(code int, msg string, data interface{}, header map[string]string, ctx *context.Context) {
	Result(code, msg, data, header, ctx)
}

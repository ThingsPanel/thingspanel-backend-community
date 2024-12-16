// internal/middleware/response/response.go
package response

import (
	"fmt"
	"net/http"
	"project/pkg/errcode"
	"strings"

	"github.com/gin-gonic/gin"
)

// Response 统一的API响应结构
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Handler 响应处理器
type Handler struct {
	ErrManager *errcode.ErrorManager
}

// NewHandler 创建响应处理器
func NewHandler(configPath string) (*Handler, error) {
	errManager := errcode.NewErrorManager(configPath)
	if err := errManager.LoadMessages(); err != nil {
		return nil, err
	}
	return &Handler{ErrManager: errManager}, nil
}

// Middleware 创建响应处理中间件
func (h *Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 处理panic
		defer func() {
			if err := recover(); err != nil {
				// 对于panic，创建一个系统错误
				sysErr := errcode.NewWithMessage(errcode.CodeSystemError, fmt.Sprint(err))
				h.handleError(c, sysErr)
				c.Abort()
			}
		}()

		c.Next()

		// 2. 如果已经写入响应，则返回
		if c.Writer.Written() {
			return
		}

		// 3. 处理错误响应
		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err
			switch e := err.(type) {
			case *errcode.Error:
				h.handleError(c, e)
			default:
				// 未知错误转换为系统错误
				sysErr := errcode.NewWithMessage(errcode.CodeSystemError, err.Error())
				h.handleError(c, sysErr)
			}
			return
		}

		// 4. 处理成功响应
		if data, exists := c.Get("data"); exists {
			h.responseSuccess(c, data)
		}
	}
}

// responseSuccess 处理成功响应
func (h *Handler) responseSuccess(c *gin.Context, data interface{}) {
	lang := c.GetHeader("Accept-Language")
	c.JSON(http.StatusOK, &Response{
		Code:    errcode.CodeSuccess,
		Message: h.ErrManager.GetMessage(errcode.CodeSuccess, lang),
		Data:    data,
	})
}

// pkg/response/response.go
func (h *Handler) handleError(c *gin.Context, err *errcode.Error) {
	var msg string
	if err.UseCustomMsg {
		msg = err.CustomMsg // 使用自定义消息
	} else {
		lang := c.GetHeader("Accept-Language")
		msg = h.ErrManager.GetMessage(err.Code, lang) // 使用配置的国际化消息

		// 处理格式化参数
		if err.Args != nil {
			msg = fmt.Sprintf(msg, err.Args...)
		}

		// 处理变量替换
		if err.Variables != nil {
			for k, v := range err.Variables {
				msg = strings.ReplaceAll(msg, "${"+k+"}", fmt.Sprint(v))
			}
		}
	}

	resp := &Response{
		Code:    err.Code,
		Message: msg,
	}
	if err.Data != nil {
		resp.Data = err.Data
	}

	c.JSON(http.StatusOK, resp)
}

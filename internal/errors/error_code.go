// pkg/errors/error_code.go

package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode 定义错误码结构
type ErrorCode struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

// Error 实现 error 接口
func (e *ErrorCode) Error() string {
	return fmt.Sprintf("Error Code: %d, Message: %s", e.Code, e.Message)
}

// 错误码规则：
// 10000-19999: 系统级错误
// 20000-29999: 认证和授权错误
// 30000-39999: 输入验证错误
// 40000-49999: 业务逻辑错误
// 50000-59999: 设备相关错误
// 60000-69999: 文件和数据处理错误

const (
	// 系统级错误 (10000-19999)
	ErrSystemInternal = 10001 // 系统内部错误
	ErrDatabaseError  = 10002 // 数据库操作错误
	ErrNetworkError   = 10003 // 网络通信错误

	ErrConfigError = 10004 // 系统配置错误
	ErrCacheError  = 10005 // 缓存操作错误

	// 认证和授权错误 (20000-29999)
	ErrAuthFailed            = 20001 // 认证失败
	ErrTokenExpired          = 20002 // 令牌已过期
	ErrInvalidToken          = 20003 // 无效的令牌
	ErrInsufficientPrivilege = 20004 // 权限不足
	ErrAccountLocked         = 20005 // 账户已锁定
	ErrTooManyAttempts       = 20006 // 尝试次数过多
	ErrPasswordExpired       = 20007 // 密码已过期
	ErrUserNotFound          = 20008 // 用户不存在

	// 输入验证错误 (30000-39999)
	ErrInvalidInput     = 30001 // 无效的输入
	ErrMissingParameter = 30002 // 缺少必要参数
	ErrInvalidFormat    = 30003 // 格式错误

	// 业务逻辑错误 (40000-49999)
	ErrResourceNotFound = 40001 // 资源未找到
	ErrDuplicateEntry   = 40002 // 重复条目
	ErrOperationFailed  = 40003 // 操作失败

	// 设备相关错误 (50000-59999)
	ErrDeviceNotFound     = 50001 // 设备未找到
	ErrDeviceOffline      = 50002 // 设备离线
	ErrDeviceUnauthorized = 50003 // 设备未授权

	// 文件和数据处理错误 (60000-69999)
	ErrFileUpload      = 60001 // 文件上传错误
	ErrDataProcessing  = 60002 // 数据处理错误
	ErrFileTooLarge    = 60003 // 文件过大
	ErrInvalidFileType = 60004 // 无效的文件类型
)

// errorCodeMap 定义错误码映射
var errorCodeMap = map[int]ErrorCode{
	ErrSystemInternal: {
		Code:       ErrSystemInternal,
		Message:    "内部系统错误",
		HTTPStatus: http.StatusInternalServerError,
	},
	ErrDatabaseError: {
		Code:       ErrDatabaseError,
		Message:    "数据库操作错误",
		HTTPStatus: http.StatusInternalServerError,
	},
	ErrNetworkError: {
		Code:       ErrNetworkError,
		Message:    "网络通信错误",
		HTTPStatus: http.StatusBadGateway,
	},
	ErrConfigError: {
		Code:       ErrConfigError,
		Message:    "系统配置错误",
		HTTPStatus: http.StatusInternalServerError,
	},
	ErrCacheError: {
		Code:       ErrCacheError,
		Message:    "缓存操作错误",
		HTTPStatus: http.StatusInternalServerError,
	},
	ErrAuthFailed: {
		Code:       ErrAuthFailed,
		Message:    "认证失败",
		HTTPStatus: http.StatusUnauthorized,
	},
	ErrTokenExpired: {
		Code:       ErrTokenExpired,
		Message:    "令牌已过期",
		HTTPStatus: http.StatusUnauthorized,
	},
	ErrInvalidToken: {
		Code:       ErrInvalidToken,
		Message:    "无效的令牌",
		HTTPStatus: http.StatusUnauthorized,
	},
	ErrInsufficientPrivilege: {
		Code:       ErrInsufficientPrivilege,
		Message:    "权限不足",
		HTTPStatus: http.StatusForbidden,
	},
	ErrAccountLocked: {
		Code:       ErrAccountLocked,
		Message:    "账户已锁定",
		HTTPStatus: http.StatusForbidden,
	},
	ErrTooManyAttempts: {
		Code:       ErrTooManyAttempts,
		Message:    "尝试次数过多",
		HTTPStatus: http.StatusTooManyRequests,
	},
	ErrPasswordExpired: {
		Code:       ErrPasswordExpired,
		Message:    "密码已过期",
		HTTPStatus: http.StatusForbidden,
	},
	ErrUserNotFound: {
		Code:       ErrUserNotFound,
		Message:    "用户不存在",
		HTTPStatus: http.StatusNotFound,
	},
	ErrInvalidInput: {
		Code:       ErrInvalidInput,
		Message:    "无效的输入",
		HTTPStatus: http.StatusBadRequest,
	},
	ErrMissingParameter: {
		Code:       ErrMissingParameter,
		Message:    "缺少必要参数",
		HTTPStatus: http.StatusBadRequest,
	},
	ErrInvalidFormat: {
		Code:       ErrInvalidFormat,
		Message:    "格式错误",
		HTTPStatus: http.StatusBadRequest,
	},
	ErrDeviceNotFound: {
		Code:       ErrDeviceNotFound,
		Message:    "设备未找到",
		HTTPStatus: http.StatusNotFound,
	},
	ErrDeviceOffline: {
		Code:       ErrDeviceOffline,
		Message:    "设备离线",
		HTTPStatus: http.StatusServiceUnavailable,
	},
	ErrFileUpload: {
		Code:       ErrFileUpload,
		Message:    "文件上传错误",
		HTTPStatus: http.StatusBadRequest,
	},
	ErrDataProcessing: {
		Code:       ErrDataProcessing,
		Message:    "数据处理错误",
		HTTPStatus: http.StatusInternalServerError,
	},
	// ... 其他错误码映射
}

// NewError 创建一个新的错误
func NewError(code int) *ErrorCode {
	if err, ok := errorCodeMap[code]; ok {
		return &err
	}
	return &ErrorCode{
		Code:       code,
		Message:    "未知错误",
		HTTPStatus: http.StatusInternalServerError,
	}
}

// WithDetails 添加错误详情
func (e *ErrorCode) WithDetails(details string) *ErrorCode {
	return &ErrorCode{
		Code:       e.Code,
		Message:    e.Message + ": " + details,
		HTTPStatus: e.HTTPStatus,
	}
}

// 封装错误信息
func Wrap(err error, code int) *ErrorCode {
	if err == nil {
		return nil
	}
	errorCode := NewError(code)
	return errorCode.WithDetails(err.Error())
}

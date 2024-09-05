package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode 定义错误码结构
type ErrorCode struct {
	Code       int
	Message    string
	HTTPStatus int
}

// 定义错误码
const (
	// 系统级错误 (10000-19999)
	ErrSystemInternal = 10001 // 系统内部错误
	ErrDatabaseError  = 10002 // 数据库操作错误
	ErrNetworkError   = 10003 // 网络通信错误
	ErrConfigError    = 10004 // 系统配置错误
	ErrCacheError     = 10005 // 缓存操作错误

	// 认证和授权错误 (20000-29999)
	ErrAuthFailed            = 20001 // 认证失败
	ErrTokenExpired          = 20002 // 令牌已过期
	ErrInvalidToken          = 20003 // 无效的令牌
	ErrInsufficientPrivilege = 20004 // 权限不足
	ErrAccountLocked         = 20005 // 账户已锁定
	ErrTooManyAttempts       = 20006 // 尝试次数过多
	ErrPasswordExpired       = 20007 // 密码已过期
	ErrUserNotFound          = 20008 // 用户不存在
	ErrInvalidCredentials    = 20009 // 用户名或密码错误
	ErrUserDisabled          = 20010 // 用户状态异常

	// 输入验证错误 (30000-39999)
	ErrInvalidInput     = 30001
	ErrMissingParameter = 30002
	ErrInvalidFormat    = 30003

	// 业务逻辑错误 (40000-49999)
	ErrResourceNotFound = 40001
	ErrDuplicateEntry   = 40002
	ErrOperationFailed  = 40003

	// 设备相关错误 (50000-59999)
	ErrDeviceNotFound     = 50001
	ErrDeviceOffline      = 50002
	ErrDeviceUnauthorized = 50003

	// 文件和数据处理错误 (60000-69999)
	ErrFileUpload      = 60001
	ErrDataProcessing  = 60002
	ErrFileTooLarge    = 60003
	ErrInvalidFileType = 60004
)

// errorCodeMap 定义错误码映射
var errorCodeMap = map[int]ErrorCode{
	ErrSystemInternal: {ErrSystemInternal, "内部系统错误", http.StatusInternalServerError},
	ErrDatabaseError:  {ErrDatabaseError, "数据库操作错误", http.StatusInternalServerError},
	ErrNetworkError:   {ErrNetworkError, "网络通信错误", http.StatusBadGateway},
	ErrConfigError:    {ErrConfigError, "系统配置错误", http.StatusInternalServerError},
	ErrCacheError:     {ErrCacheError, "缓存操作错误", http.StatusInternalServerError},

	ErrAuthFailed:            {ErrAuthFailed, "认证失败", http.StatusUnauthorized},
	ErrTokenExpired:          {ErrTokenExpired, "令牌已过期", http.StatusUnauthorized},
	ErrInvalidToken:          {ErrInvalidToken, "无效的令牌", http.StatusUnauthorized},
	ErrInsufficientPrivilege: {ErrInsufficientPrivilege, "权限不足", http.StatusForbidden},
	ErrAccountLocked:         {ErrAccountLocked, "账户已锁定", http.StatusForbidden},
	ErrTooManyAttempts:       {ErrTooManyAttempts, "尝试次数过多", http.StatusTooManyRequests},
	ErrPasswordExpired:       {ErrPasswordExpired, "密码已过期", http.StatusForbidden},
	ErrUserNotFound:          {ErrUserNotFound, "用户不存在", http.StatusNotFound},
	ErrInvalidCredentials:    {ErrInvalidCredentials, "用户名或密码错误", http.StatusOK},
	ErrUserDisabled:          {ErrUserDisabled, "用户状态异常", http.StatusForbidden},

	ErrInvalidInput:       {ErrInvalidInput, "无效的输入", http.StatusBadRequest},
	ErrMissingParameter:   {ErrMissingParameter, "缺少必要参数", http.StatusBadRequest},
	ErrInvalidFormat:      {ErrInvalidFormat, "格式错误", http.StatusBadRequest},
	ErrResourceNotFound:   {ErrResourceNotFound, "资源未找到", http.StatusNotFound},
	ErrDuplicateEntry:     {ErrDuplicateEntry, "重复条目", http.StatusConflict},
	ErrOperationFailed:    {ErrOperationFailed, "操作失败", http.StatusInternalServerError},
	ErrDeviceNotFound:     {ErrDeviceNotFound, "设备未找到", http.StatusNotFound},
	ErrDeviceOffline:      {ErrDeviceOffline, "设备离线", http.StatusServiceUnavailable},
	ErrDeviceUnauthorized: {ErrDeviceUnauthorized, "设备未授权", http.StatusForbidden},

	ErrFileUpload:      {ErrFileUpload, "文件上传错误", http.StatusBadRequest},
	ErrDataProcessing:  {ErrDataProcessing, "数据处理错误", http.StatusInternalServerError},
	ErrFileTooLarge:    {ErrFileTooLarge, "文件过大", http.StatusRequestEntityTooLarge},
	ErrInvalidFileType: {ErrInvalidFileType, "无效的文件类型", http.StatusBadRequest},
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

// Error 实现 error 接口
func (e *ErrorCode) Error() string {
	return fmt.Sprintf("Error Code: %d, Message: %s", e.Code, e.Message)
}

// Wrap 封装错误信息
func Wrap(err error, code int) *ErrorCode {
	if err == nil {
		return nil
	}
	errorCode := NewError(code)
	errorCode.Message = fmt.Sprintf("%s: %s", errorCode.Message, err.Error())
	return errorCode
}

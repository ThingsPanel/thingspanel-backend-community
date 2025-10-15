package processor

import "errors"

// 错误码常量
const (
	ErrCodeScriptNotFound      = "SCRIPT_NOT_FOUND"       // 脚本不存在
	ErrCodeScriptDisabled      = "SCRIPT_DISABLED"        // 脚本未启用
	ErrCodeScriptExecuteFailed = "SCRIPT_EXEC_FAILED"     // 脚本执行失败
	ErrCodeScriptTimeout       = "SCRIPT_TIMEOUT"         // 脚本执行超时
	ErrCodeInvalidInput        = "INVALID_INPUT"          // 输入参数无效
	ErrCodeCacheError          = "CACHE_ERROR"            // 缓存操作失败
	ErrCodeDatabaseError       = "DATABASE_ERROR"         // 数据库查询失败
)

// 预定义错误
var (
	ErrScriptNotFound      = errors.New("script not found")
	ErrScriptDisabled      = errors.New("script is disabled")
	ErrScriptExecuteFailed = errors.New("script execution failed")
	ErrScriptTimeout       = errors.New("script execution timeout")
	ErrInvalidInput        = errors.New("invalid input parameters")
	ErrCacheError          = errors.New("cache operation failed")
	ErrDatabaseError       = errors.New("database query failed")
	ErrInvalidDataType     = errors.New("invalid data type")
)

// ProcessorError 处理器错误（包含错误码和详细信息）
type ProcessorError struct {
	Code    string // 错误码
	Message string // 错误信息
	Cause   error  // 原始错误
}

func (e *ProcessorError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *ProcessorError) Unwrap() error {
	return e.Cause
}

// NewProcessorError 创建处理器错误
func NewProcessorError(code, message string, cause error) *ProcessorError {
	return &ProcessorError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// 便捷错误构造函数
func NewScriptNotFoundError(deviceConfigID, scriptType string) *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeScriptNotFound,
		Message: "script not found for device_config_id: " + deviceConfigID + ", script_type: " + scriptType,
		Cause:   ErrScriptNotFound,
	}
}

func NewScriptDisabledError(deviceConfigID, scriptType string) *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeScriptDisabled,
		Message: "script is disabled for device_config_id: " + deviceConfigID + ", script_type: " + scriptType,
		Cause:   ErrScriptDisabled,
	}
}

func NewScriptExecuteError(cause error) *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeScriptExecuteFailed,
		Message: "script execution failed",
		Cause:   cause,
	}
}

func NewScriptTimeoutError() *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeScriptTimeout,
		Message: "script execution timeout",
		Cause:   ErrScriptTimeout,
	}
}

func NewInvalidInputError(message string) *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeInvalidInput,
		Message: message,
		Cause:   ErrInvalidInput,
	}
}

func NewCacheError(cause error) *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeCacheError,
		Message: "cache operation failed",
		Cause:   cause,
	}
}

func NewDatabaseError(cause error) *ProcessorError {
	return &ProcessorError{
		Code:    ErrCodeDatabaseError,
		Message: "database query failed",
		Cause:   cause,
	}
}

// pkg/errcode/code.go
package errcode

// 系统级错误码
const (
	CodeSuccess     = "200"    // 成功
	CodeSystemError = "100000" // 系统内部错误
	CodeParamError  = "100002" // 参数错误
	CodeNotFound    = "100404" // 资源不存在
	CodeDBError     = "101001" // 数据库错误
	CodeCacheError  = "102001" // 缓存错误
)

// 业务级错误码
const (
	// 用户模块 (200xxx)
	CodeUnauthorized = "200001" // 未授权
	CodeInvalidAuth  = "200002" // 用户名或密码错误
	CodeUserLocked   = "200003" // 用户被锁定

	// 权限模块 (201xxx)
	CodeNoPermission = "201001" // 无权限
	CodeOpDenied     = "201002" // 操作被拒绝
)

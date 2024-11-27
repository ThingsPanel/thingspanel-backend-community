package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Controller struct {
	UserApi                       // 用户管理
	DictApi                       // 字典管理
	ProductApi                    // 产品管理
	OTAApi                        // ota管理
	UpLoadApi                     // 上传
	ProtocolPluginApi             // 协议插件
	DeviceApi                     // 设备
	DeviceModelApi                // 设备物模型
	UiElementsApi                 // UI元素控制
	BoardApi                      // 首页
	TelemetryDataApi              // 遥测数据
	AttributeDataApi              // 属性数据
	EventDataApi                  // 事件数据
	CommandSetLogApi              // 命令下发记录
	OperationLogsApi              // 系统日志
	LogoApi                       // 站标
	DataPolicyApi                 // 数据清理
	DeviceConfigApi               // 设备配置
	DataScriptApi                 // 数据处理脚本
	RoleApi                       // 用户管理
	CasbinApi                     // 权限管理
	NotificationGroupApi          // 通知组
	NotificationHistoryApi        // 通知历史
	NotificationServicesConfigApi // 通知服务配置
	AlarmApi                      // 告警
	SceneAutomationsApi           //场景联动
	SceneApi                      //场景
	SystemApi                     //系统相关
	SysFunctionApi                //功能设置
	VisPluginApi                  //可视化插件
	ServicePluginApi              //插件管理
	ServiceAccessApi              //服务接入管理
	ExpectedDataApi               // 预期数据
}

var Controllers = new(Controller)
var Validate *validator.Validate

func init() {
	Validate = validator.New()
}

// ValidateStruct validates the request structure
func ValidateStruct(i interface{}) error {
	err := Validate.Struct(i)
	if err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			return err
		}

		var errors []string
		for _, err := range err.(validator.ValidationErrors) {
			var tError string
			switch err.Tag() {
			case "required":
				tError = fmt.Sprintf("Field '%s' is required", err.Field())
			case "email":
				tError = fmt.Sprintf("Field '%s' must be a valid email address", err.Field())
			case "gte":
				tError = fmt.Sprintf("The value of field '%s' must be at least %s", err.Field(), err.Param())
			case "lte":
				tError = fmt.Sprintf("The value of field '%s' must be at most %s", err.Field(), err.Param())
			default:
				tError = fmt.Sprintf("Field '%s' failed validation (%s)", err.Field(), validationErrorToText(err))
			}
			errors = append(errors, tError)
		}

		return fmt.Errorf("%s", errors[0])
	}
	return nil
}

// validationErrorToText converts validation errors to more descriptive text
func validationErrorToText(e validator.FieldError) string {
	switch e.Tag() {
	case "min":
		return fmt.Sprintf("At least %s characters", e.Param())
	case "max":
		return fmt.Sprintf("At most %s characters", e.Param())
	case "len":
		return fmt.Sprintf("Must be %s characters", e.Param())
	// Add more cases as needed
	default:
		return "Does not meet validation rules"
	}
}

// 定义统一的HTTP响应结构
type ApiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ErrorHandler 统一错误处理
func ErrorHandler(c *gin.Context, code int, err error) {
	// if strings.Contains(err.Error(), "SQLSTATE 23503") {
	// 	// 处理外键约束违反
	// 	err = fmt.Errorf("操作无法完成：请先删除与此项相关联的数据后再进行尝试")
	// }
	// if strings.Contains(err.Error(), "SQLSTATE 23505") {
	// 	// 处理唯一键约束违反
	// 	err = fmt.Errorf("操作无法完成：已存在相同的数据")
	// }
	// fmt.Printf("%T\n", err)
	// // 检查这个错误是否是 *pgconn.PgError
	// var pgErr *pgconn.PgError
	// if errors.As(err, &pgErr) {
	// 	logrus.Error("-----------------")
	// 	// 现在 pgErr 是 err 中的 *pgconn.PgError 部分（如果存在）
	// 	if pgErr.SQLState() == "23503" {
	// 		// 这就是一个外键约束违反错误
	// 		err = fmt.Errorf("外键约束违反: %w", err)
	// 	}
	// }
	logrus.Error(err)
	c.JSON(http.StatusOK, ApiResponse{
		Code:    code,
		Message: err.Error(),
	})
}

// SuccessHandler 统一成功响应
func SuccessHandler(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    http.StatusOK,
		Message: message,
		Data:    data,
	})
}

// SuccessOK 统一成功响应
func SuccessOK(c *gin.Context) {
	c.JSON(http.StatusOK, ApiResponse{
		Code:    http.StatusOK,
		Message: "Success",
	})
}

func BindAndValidate(c *gin.Context, obj interface{}) bool {
	// 判断请求方法
	if c.Request.Method == http.MethodGet {
		if err := c.ShouldBindQuery(obj); err != nil {
			ErrorHandler(c, http.StatusBadRequest, err)
			return false
		}
	} else if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete {
		if err := c.ShouldBindJSON(obj); err != nil {
			ErrorHandler(c, http.StatusBadRequest, err)
			return false
		}
	}

	if err := ValidateStruct(obj); err != nil {
		// 如果是验证错误，返回422 Unprocessable Entity
		ErrorHandler(c, http.StatusUnprocessableEntity, err)
		return false
	}

	return true
}

var Wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		// 不做跨域检查
		return true
	},
}

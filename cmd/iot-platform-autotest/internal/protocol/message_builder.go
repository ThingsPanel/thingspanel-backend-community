package protocol

// MessageBuilder 消息构建器接口
type MessageBuilder interface {
	// BuildTelemetry 构建遥测数据报文
	BuildTelemetry(data interface{}) ([]byte, error)

	// BuildAttribute 构建属性数据报文
	BuildAttribute(data interface{}) ([]byte, error)

	// BuildEvent 构建事件数据报文
	BuildEvent(method string, params interface{}) ([]byte, error)

	// BuildResponse 构建响应报文
	BuildResponse(success bool, method string) ([]byte, error)
}

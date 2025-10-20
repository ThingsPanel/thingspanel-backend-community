package mqttadapter

import "fmt"

// Topic 模板定义（协议规范，不应放配置文件）
// Topic 是设备通信协议的一部分，类似 RESTful API 的路由规则

// 上行 Topic 模板（设备 → 平台）
const (
	// TopicPatternTelemetry 遥测数据上报 Topic 模式
	// 格式: devices/telemetry (不使用通配符，订阅所有遥测数据)
	TopicPatternTelemetry = "devices/telemetry"

	// TopicPatternAttribute 属性上报 Topic 模式
	// 格式: devices/attributes/{message_id}
	TopicPatternAttribute = "devices/attributes/+"

	// TopicPatternEvent 事件上报 Topic 模式
	// 格式: devices/event/{message_id}
	TopicPatternEvent = "devices/event/+"

	// TopicPatternStatus 状态上报 Topic 模式
	// 格式: devices/status/{device_id}
	TopicPatternStatus = "devices/status/+"

	// 网关 Topic 模式（前缀为 gateway/）
	TopicPatternGatewayTelemetry = "gateway/telemetry"
	TopicPatternGatewayAttribute = "gateway/attributes/+"
	TopicPatternGatewayEvent     = "gateway/event/+"
)

// 下行 Topic 模板（平台 → 设备）
const (
	// TopicTemplateAttributeSet 属性设置 Topic 模板
	// %s = device_number
	TopicTemplateAttributeSet = "devices/attributes/set/%s"

	// TopicTemplateCommand 命令下发 Topic 模板
	// %s = device_number
	TopicTemplateCommand = "devices/command/%s"

	// 网关下行 Topic 模板
	TopicTemplateGatewayAttributeSet = "gateway/attributes/set/%s"
	TopicTemplateGatewayCommand      = "gateway/command/%s"
)

// 响应 Topic 模板（设备 → 平台的 ACK）
const (
	// TopicTemplateAttributeResponse 属性上报响应 Topic 模板
	// 参数: device_number, message_id
	TopicTemplateAttributeResponse = "devices/attributes/response/%s/%s"

	// TopicTemplateEventResponse 事件上报响应 Topic 模板
	// 参数: device_number, message_id
	TopicTemplateEventResponse = "devices/event/response/%s/%s"

	// 命令/属性设置响应订阅模式
	TopicPatternCommandResponse      = "devices/command/response/+"
	TopicPatternAttributeSetResponse = "devices/attributes/set/response/+"
	TopicPatternGatewayCommandResponse      = "gateway/command/response/+"
	TopicPatternGatewayAttributeSetResponse = "gateway/attributes/set/response/+"
)

// BuildAttributeResponseTopic 构造属性上报响应 Topic
func BuildAttributeResponseTopic(deviceNumber, messageID string) string {
	return fmt.Sprintf(TopicTemplateAttributeResponse, deviceNumber, messageID)
}

// BuildEventResponseTopic 构造事件上报响应 Topic
func BuildEventResponseTopic(deviceNumber, messageID string) string {
	return fmt.Sprintf(TopicTemplateEventResponse, deviceNumber, messageID)
}

// BuildAttributeSetTopic 构造属性设置 Topic
func BuildAttributeSetTopic(deviceNumber string) string {
	return fmt.Sprintf(TopicTemplateAttributeSet, deviceNumber)
}

// BuildCommandTopic 构造命令下发 Topic
func BuildCommandTopic(deviceNumber string) string {
	return fmt.Sprintf(TopicTemplateCommand, deviceNumber)
}

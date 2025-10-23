package device

import (
	"time"
)

// Device 设备接口，定义所有设备类型的统一行为
type Device interface {
	// Connect 连接到MQTT Broker
	Connect() error

	// Disconnect 断开连接
	Disconnect()

	// IsConnected 检查连接状态
	IsConnected() bool

	// PublishTelemetry 上报遥测数据
	PublishTelemetry(data interface{}) error

	// PublishAttribute 上报属性数据
	PublishAttribute(data interface{}, messageID string) error

	// PublishEvent 上报事件数据
	PublishEvent(method string, params interface{}, messageID string) error

	// PublishCommandResponse 发送命令响应
	PublishCommandResponse(messageID string, success bool, method string) error

	// PublishAttributeSetResponse 发送属性设置响应
	PublishAttributeSetResponse(messageID string, success bool) error

	// SubscribeAll 订阅所有需要的主题
	SubscribeAll() error

	// GetReceivedMessages 获取接收到的消息
	GetReceivedMessages(topicPattern string, timeout time.Duration) []ReceivedMessage

	// ClearReceivedMessages 清空接收到的消息
	ClearReceivedMessages(topicPattern string)
}

// ReceivedMessage 接收到的消息
type ReceivedMessage struct {
	Topic     string
	Payload   []byte
	Timestamp time.Time
}

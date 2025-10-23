package downlink

// MessagePublisher 消息发布接口（协议无关）
// 实现可以是：MQTT、Kafka、AMQP 等
type MessagePublisher interface {
	// PublishMessage 发布消息到设备
	// deviceNumber: 目标设备编号
	// msgType: 消息类型（用于选择Topic路径）
	// deviceType: 设备类型（用于区分devices/*还是gateway/*）
	// topicPrefix: Topic前缀（协议插件使用，MQTT为空）
	// messageID: 消息唯一标识（命令/属性设置需要拼接到Topic）
	// qos: 消息质量等级
	// payload: 消息内容（字节流）
	PublishMessage(deviceNumber string, msgType MessageType, deviceType string, topicPrefix string, messageID string, qos byte, payload []byte) error
}

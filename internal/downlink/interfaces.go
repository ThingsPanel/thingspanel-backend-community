package downlink

// MessagePublisher 消息发布接口（协议无关）
// 实现可以是：MQTT、Kafka、AMQP 等
type MessagePublisher interface {
	// Publish 发布消息到消息队列/代理
	// topic: 主题/队列名称
	// qos: 质量等级（MQTT QoS，Kafka 可忽略）
	// payload: 消息内容（字节流）
	Publish(topic string, qos byte, payload []byte) error
}

package device

import (
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	"iot-platform-autotest/internal/config"
	"iot-platform-autotest/internal/protocol"
	"iot-platform-autotest/internal/utils"
)

// DirectDevice 直连设备实现
type DirectDevice struct {
	config  *config.Config
	client  mqtt.Client
	topics  *utils.MQTTTopics
	builder protocol.MessageBuilder
	logger  *zap.Logger

	// 消息存储
	receivedMessages map[string][]ReceivedMessage
	mu               sync.RWMutex
}

// NewDirectDevice 创建直连设备
func NewDirectDevice(cfg *config.Config, logger *zap.Logger) *DirectDevice {
	return &DirectDevice{
		config:           cfg,
		topics:           utils.NewMQTTTopics(cfg.Device.DeviceNumber),
		builder:          protocol.NewDirectMessageBuilder(),
		logger:           logger,
		receivedMessages: make(map[string][]ReceivedMessage),
	}
}

// Connect 连接到MQTT Broker
func (d *DirectDevice) Connect() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s", d.config.MQTT.Broker))
	opts.SetClientID(d.config.MQTT.ClientID)
	opts.SetUsername(d.config.MQTT.Username)
	opts.SetPassword(d.config.MQTT.Password)
	opts.SetCleanSession(d.config.MQTT.CleanSession)
	opts.SetKeepAlive(time.Duration(d.config.MQTT.KeepAlive) * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetMaxReconnectInterval(10 * time.Second)

	// 连接丢失处理
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		d.logger.Error("MQTT connection lost", zap.Error(err))
	})

	// 连接成功处理
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		d.logger.Info("MQTT connected successfully")
	})

	d.client = mqtt.NewClient(opts)

	token := d.client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("connection timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("connection failed: %w", token.Error())
	}

	d.logger.Info("Direct device connected",
		zap.String("broker", d.config.MQTT.Broker),
		zap.String("client_id", d.config.MQTT.ClientID),
		zap.String("device_number", d.config.Device.DeviceNumber))

	return nil
}

// Disconnect 断开连接
func (d *DirectDevice) Disconnect() {
	if d.client != nil && d.client.IsConnected() {
		d.client.Disconnect(250)
		d.logger.Info("Direct device disconnected")
	}
}

// IsConnected 检查连接状态
func (d *DirectDevice) IsConnected() bool {
	return d.client != nil && d.client.IsConnected()
}

// PublishTelemetry 上报遥测数据
func (d *DirectDevice) PublishTelemetry(data interface{}) error {
	payload, err := d.builder.BuildTelemetry(data)
	if err != nil {
		return err
	}

	topic := d.topics.Telemetry()
	token := d.client.Publish(topic, d.config.MQTT.QoS, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("publish failed: %w", token.Error())
	}

	d.logger.Info("Telemetry published",
		zap.String("topic", topic),
		zap.String("payload", string(payload)))

	return nil
}

// PublishAttribute 上报属性数据
func (d *DirectDevice) PublishAttribute(data interface{}, messageID string) error {
	payload, err := d.builder.BuildAttribute(data)
	if err != nil {
		return err
	}

	topic := d.topics.Attributes(messageID)
	token := d.client.Publish(topic, d.config.MQTT.QoS, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("publish failed: %w", token.Error())
	}

	d.logger.Info("Attribute published",
		zap.String("topic", topic),
		zap.String("message_id", messageID),
		zap.String("payload", string(payload)))

	return nil
}

// PublishEvent 上报事件数据
func (d *DirectDevice) PublishEvent(method string, params interface{}, messageID string) error {
	payload, err := d.builder.BuildEvent(method, params)
	if err != nil {
		return err
	}

	topic := d.topics.Event(messageID)
	token := d.client.Publish(topic, d.config.MQTT.QoS, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("publish failed: %w", token.Error())
	}

	d.logger.Info("Event published",
		zap.String("topic", topic),
		zap.String("message_id", messageID),
		zap.String("method", method),
		zap.String("payload", string(payload)))

	return nil
}

// PublishCommandResponse 发送命令响应
func (d *DirectDevice) PublishCommandResponse(messageID string, success bool, method string) error {
	payload, err := d.builder.BuildResponse(success, method)
	if err != nil {
		return err
	}

	topic := d.topics.CommandResponse(messageID)
	token := d.client.Publish(topic, d.config.MQTT.QoS, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("publish failed: %w", token.Error())
	}

	d.logger.Info("Command response published",
		zap.String("topic", topic),
		zap.String("message_id", messageID),
		zap.String("payload", string(payload)))

	return nil
}

// PublishAttributeSetResponse 发送属性设置响应
func (d *DirectDevice) PublishAttributeSetResponse(messageID string, success bool) error {
	payload, err := d.builder.BuildResponse(success, "")
	if err != nil {
		return err
	}

	topic := d.topics.AttributeSetResponse(messageID)
	token := d.client.Publish(topic, d.config.MQTT.QoS, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("publish failed: %w", token.Error())
	}

	d.logger.Info("Attribute set response published",
		zap.String("topic", topic),
		zap.String("message_id", messageID),
		zap.String("payload", string(payload)))

	return nil
}

// messageHandler 通用消息处理器
func (d *DirectDevice) messageHandler(client mqtt.Client, msg mqtt.Message) {
	d.mu.Lock()
	defer d.mu.Unlock()

	topic := msg.Topic()
	payload := msg.Payload()

	// 存储接收到的消息
	d.receivedMessages[topic] = append(d.receivedMessages[topic], ReceivedMessage{
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
	})

	d.logger.Info("Message received",
		zap.String("topic", topic),
		zap.String("payload", string(payload)))
}

// subscribe 订阅主题
func (d *DirectDevice) subscribe(topic string) error {
	token := d.client.Subscribe(topic, d.config.MQTT.QoS, d.messageHandler)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("subscribe timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("subscribe failed: %w", token.Error())
	}

	d.logger.Info("Subscribed to topic", zap.String("topic", topic))
	return nil
}

// SubscribeAll 订阅所有需要的主题
func (d *DirectDevice) SubscribeAll() error {
	topics := []string{
		d.topics.TelemetryControl(),
		d.topics.AttributeSet(),
		d.topics.AttributeGet(),
		d.topics.Command(),
		d.topics.AttributeResponse(),
		d.topics.EventResponse(),
	}

	for _, topic := range topics {
		if err := d.subscribe(topic); err != nil {
			return fmt.Errorf("failed to subscribe %s: %w", topic, err)
		}
	}

	return nil
}

// GetReceivedMessages 获取接收到的消息
func (d *DirectDevice) GetReceivedMessages(topicPattern string, timeout time.Duration) []ReceivedMessage {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		d.mu.RLock()
		for topic, messages := range d.receivedMessages {
			if matchTopic(topic, topicPattern) && len(messages) > 0 {
				result := make([]ReceivedMessage, len(messages))
				copy(result, messages)
				d.mu.RUnlock()

				d.logger.Debug("Found matching messages",
					zap.String("pattern", topicPattern),
					zap.String("actual_topic", topic),
					zap.Int("count", len(result)))

				return result
			}
		}
		d.mu.RUnlock()
		time.Sleep(100 * time.Millisecond)
	}

	// 超时后打印调试信息
	d.mu.RLock()
	d.logger.Warn("No matching messages found",
		zap.String("pattern", topicPattern),
		zap.Int("total_topics", len(d.receivedMessages)))
	for topic, msgs := range d.receivedMessages {
		d.logger.Debug("Available topic",
			zap.String("topic", topic),
			zap.Int("message_count", len(msgs)))
	}
	d.mu.RUnlock()

	return nil
}

// ClearReceivedMessages 清空接收到的消息
func (d *DirectDevice) ClearReceivedMessages(topicPattern string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if topicPattern == "" {
		d.receivedMessages = make(map[string][]ReceivedMessage)
		return
	}

	for topic := range d.receivedMessages {
		if matchTopic(topic, topicPattern) {
			delete(d.receivedMessages, topic)
		}
	}
}

// matchTopic MQTT主题匹配(支持+和#通配符)
func matchTopic(topic, pattern string) bool {
	// 完全匹配
	if pattern == topic {
		return true
	}

	// 分割主题和模式
	topicParts := splitTopic(topic)
	patternParts := splitTopic(pattern)

	// 处理 # 通配符
	if len(patternParts) > 0 && patternParts[len(patternParts)-1] == "#" {
		// # 必须是最后一个且匹配所有剩余层级
		patternParts = patternParts[:len(patternParts)-1]
		if len(topicParts) < len(patternParts) {
			return false
		}
		// 只比较 # 之前的部分
		topicParts = topicParts[:len(patternParts)]
	} else {
		// 没有 #,长度必须相等
		if len(topicParts) != len(patternParts) {
			return false
		}
	}

	// 逐层匹配
	for i := 0; i < len(patternParts); i++ {
		if patternParts[i] == "+" {
			// + 匹配任意单层
			continue
		}
		if patternParts[i] != topicParts[i] {
			return false
		}
	}

	return true
}

// splitTopic 分割主题
func splitTopic(topic string) []string {
	if topic == "" {
		return []string{}
	}

	var parts []string
	start := 0
	for i, c := range topic {
		if c == '/' {
			if i > start {
				parts = append(parts, topic[start:i])
			}
			start = i + 1
		}
	}
	if start < len(topic) {
		parts = append(parts, topic[start:])
	}

	return parts
}

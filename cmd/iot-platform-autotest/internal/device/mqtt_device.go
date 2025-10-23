package device

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	"iot-platform-autotest/internal/config"
	"iot-platform-autotest/internal/utils"
)

// ReceivedMessage 接收到的消息
type ReceivedMessage struct {
	Topic     string
	Payload   []byte
	Timestamp time.Time
}

// MQTTDevice MQTT设备模拟器
type MQTTDevice struct {
	config *config.Config
	client mqtt.Client
	topics *utils.MQTTTopics
	logger *zap.Logger

	// 消息存储
	receivedMessages map[string][]ReceivedMessage
	mu               sync.RWMutex
}

// NewMQTTDevice 创建MQTT设备
func NewMQTTDevice(cfg *config.Config, logger *zap.Logger) *MQTTDevice {
	return &MQTTDevice{
		config:           cfg,
		topics:           utils.NewMQTTTopics(cfg.Device.DeviceNumber),
		logger:           logger,
		receivedMessages: make(map[string][]ReceivedMessage),
	}
}

// Connect 连接到MQTT Broker
func (d *MQTTDevice) Connect() error {
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

	d.logger.Info("MQTT device connected",
		zap.String("broker", d.config.MQTT.Broker),
		zap.String("client_id", d.config.MQTT.ClientID))

	return nil
}

// Disconnect 断开连接
func (d *MQTTDevice) Disconnect() {
	if d.client != nil && d.client.IsConnected() {
		d.client.Disconnect(250)
		d.logger.Info("MQTT device disconnected")
	}
}

// IsConnected 检查连接状态
func (d *MQTTDevice) IsConnected() bool {
	return d.client != nil && d.client.IsConnected()
}

// PublishTelemetry 上报遥测数据
func (d *MQTTDevice) PublishTelemetry(data map[string]interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal telemetry data: %w", err)
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
func (d *MQTTDevice) PublishAttribute(data map[string]interface{}, messageID string) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal attribute data: %w", err)
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
func (d *MQTTDevice) PublishEvent(method string, params map[string]interface{}, messageID string) error {
	data := map[string]interface{}{
		"method": method,
		"params": params,
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
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
func (d *MQTTDevice) PublishCommandResponse(messageID string, success bool, method string) error {
	response := utils.BuildResponseData(success, method)

	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
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
func (d *MQTTDevice) PublishAttributeSetResponse(messageID string, success bool) error {
	response := utils.BuildResponseData(success, "")

	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
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
func (d *MQTTDevice) messageHandler(client mqtt.Client, msg mqtt.Message) {
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

// Subscribe 订阅主题
func (d *MQTTDevice) subscribe(topic string) error {
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
func (d *MQTTDevice) SubscribeAll() error {
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
func (d *MQTTDevice) GetReceivedMessages(topicPattern string, timeout time.Duration) []ReceivedMessage {
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
func (d *MQTTDevice) ClearReceivedMessages(topicPattern string) {
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

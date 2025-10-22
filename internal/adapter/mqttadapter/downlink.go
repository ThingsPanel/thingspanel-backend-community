package mqttadapter

import (
	"fmt"

	"project/internal/downlink"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// MQTTPublisher MQTT 发布适配器（实现 downlink.MessagePublisher 接口）
// 负责下行消息发送：command、attribute set
type MQTTPublisher struct {
	mqttClient mqtt.Client
	logger     *logrus.Logger
}

// 确保实现了接口
var _ downlink.MessagePublisher = (*MQTTPublisher)(nil)

// NewMQTTPublisher 创建 MQTT 发布器（使用 Adapter 的客户端）
func NewMQTTPublisher(mqttClient mqtt.Client, logger *logrus.Logger) *MQTTPublisher {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &MQTTPublisher{
		mqttClient: mqttClient,
		logger:     logger,
	}
}

// PublishMessage 实现 MessagePublisher 接口
// 根据设备信息构造Topic并发送MQTT消息
func (p *MQTTPublisher) PublishMessage(deviceNumber string, msgType downlink.MessageType, deviceType string, topicPrefix string, qos byte, payload []byte) error {
	if p.mqttClient == nil {
		return fmt.Errorf("mqtt client is nil")
	}

	if !p.mqttClient.IsConnected() {
		return fmt.Errorf("mqtt client not connected")
	}

	// 1. 根据设备类型和协议选择Topic基础路径
	// 协议插件：始终使用 devices 路径（插件自己处理层级关系）
	// MQTT协议：根据设备类型选择 devices 或 gateway
	basePattern := "devices"
	if topicPrefix == "" && deviceType != "1" {
		// MQTT 协议的网关/子设备：使用 gateway 路径
		basePattern = "gateway"
	}

	// 2. 根据消息类型选择Topic路径
	var topicPath string
	switch msgType {
	case downlink.MessageTypeTelemetry:
		topicPath = "telemetry/control"
	case downlink.MessageTypeCommand:
		topicPath = "command"
	case downlink.MessageTypeAttributeSet:
		topicPath = "attributes/set"
	case downlink.MessageTypeAttributeGet:
		topicPath = "attributes/get"
	default:
		return fmt.Errorf("unknown message type: %s", msgType)
	}

	// 3. 拼接完整Topic
	topic := fmt.Sprintf("%s%s/%s/%s",
		topicPrefix,   // 协议插件前缀（MQTT为空）
		basePattern,   // devices 或 gateway
		topicPath,     // telemetry/control 等
		deviceNumber)  // 设备编号

	// 4. 发送MQTT消息
	p.logger.WithFields(logrus.Fields{
		"topic":        topic,
		"device_type":  deviceType,
		"msg_type":     msgType,
		"topic_prefix": topicPrefix,
		"qos":          qos,
	}).Debug("Publishing to MQTT")

	token := p.mqttClient.Publish(topic, qos, false, payload)
	token.Wait()

	if err := token.Error(); err != nil {
		p.logger.WithFields(logrus.Fields{
			"topic":   topic,
			"qos":     qos,
			"payload": string(payload),
			"error":   err,
		}).Error("MQTT publish failed")
		return err
	}

	p.logger.WithField("topic", topic).Debug("MQTT message published successfully")
	return nil
}

// Publish 发布消息到 MQTT（旧接口，保留以防其他地方使用）
func (p *MQTTPublisher) Publish(topic string, qos byte, payload []byte) error {
	if p.mqttClient == nil {
		return fmt.Errorf("mqtt client is nil")
	}

	if !p.mqttClient.IsConnected() {
		return fmt.Errorf("mqtt client not connected")
	}

	p.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"qos":     qos,
		"payload": string(payload),
	}).Debug("Publishing to MQTT")

	token := p.mqttClient.Publish(topic, qos, false, payload)
	token.Wait()

	if err := token.Error(); err != nil {
		p.logger.WithFields(logrus.Fields{
			"topic":   topic,
			"qos":     qos,
			"payload": string(payload),
			"error":   err,
		}).Error("MQTT publish failed")
		return err
	}

	p.logger.WithField("topic", topic).Debug("MQTT message published successfully")
	return nil
}

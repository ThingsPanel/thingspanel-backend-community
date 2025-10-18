package adapter

import (
	"fmt"

	"project/internal/downlink"
	"project/mqtt/publish"

	"github.com/sirupsen/logrus"
)

// MQTTPublisher MQTT 发布适配器（实现 downlink.MessagePublisher 接口）
type MQTTPublisher struct {
	logger *logrus.Logger
}

// 确保实现了接口
var _ downlink.MessagePublisher = (*MQTTPublisher)(nil)

// NewMQTTPublisher 创建 MQTT 发布器
func NewMQTTPublisher(logger *logrus.Logger) *MQTTPublisher {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &MQTTPublisher{
		logger: logger,
	}
}

// Publish 发布消息到 MQTT（实现 MessagePublisher 接口）
func (p *MQTTPublisher) Publish(topic string, qos byte, payload []byte) error {
	// 动态获取全局 MQTT 客户端
	client := publish.GetMQTTClient()

	if client == nil {
		return fmt.Errorf("mqtt client is nil")
	}

	if !client.IsConnected() {
		return fmt.Errorf("mqtt client not connected")
	}

	p.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"qos":     qos,
		"payload": string(payload),
	}).Debug("Publishing to MQTT")

	token := client.Publish(topic, qos, false, payload)
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

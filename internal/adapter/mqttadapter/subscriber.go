package mqttadapter

import (
	"strings"
	"time"

	"project/initialize"
	"project/internal/uplink"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// SubscribeResponseTopics 订阅响应 Topic（供 MQTT 服务初始化时调用）
// 在 MQTT 客户端连接成功后调用此方法
func (a *Adapter) SubscribeResponseTopics(client mqtt.Client) error {
	topics := map[string]byte{
		TopicPatternCommandResponse:             1, // 设备命令响应
		TopicPatternAttributeSetResponse:        1, // 设备属性设置响应
		TopicPatternGatewayCommandResponse:      1, // 网关命令响应
		TopicPatternGatewayAttributeSetResponse: 1, // 网关属性设置响应
	}

	for topic, qos := range topics {
		token := client.Subscribe(topic, qos, a.handleResponseMessage)
		token.Wait()
		if err := token.Error(); err != nil {
			a.logger.WithFields(logrus.Fields{
				"topic": topic,
				"error": err,
			}).Error("Failed to subscribe response topic")
			return err
		}
		a.logger.WithField("topic", topic).Info("Subscribed to response topic")
	}

	return nil
}

// handleResponseMessage 处理响应消息（MQTT 回调函数）
func (a *Adapter) handleResponseMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"payload": string(payload),
	}).Debug("Received response message")

	// 1. 从 Topic 解析 message_id
	// Topic 格式: devices/command/response/{message_id}
	//           gateway/attributes/set/response/{message_id}
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		a.logger.WithField("topic", topic).Error("Invalid response topic format")
		return
	}

	messageID := parts[len(parts)-1]
	msgType := a.detectResponseType(topic)

	// 2. 验证 payload 格式
	responsePayload, err := a.verifyPayload(payload)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Invalid response payload")
		return
	}

	// 3. 获取设备信息
	device, err := initialize.GetDeviceCacheById(responsePayload.DeviceId)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": responsePayload.DeviceId,
			"error":     err,
		}).Error("Device not found in cache")
		return
	}

	// 4. 构造 UplinkMessage
	flowMsg := &UplinkMessage{
		Type:      msgType,
		DeviceID:  device.ID,
		TenantID:  device.TenantID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   responsePayload.Values,
		Metadata: map[string]interface{}{
			"device_id":       device.ID,
			"topic":           topic,
			"source_protocol": "mqtt",
			"message_id":      messageID, // ✨ 关键：传递 message_id
		},
	}

	// 5. 发送到 Bus
	if err := a.bus.Publish(flowMsg); err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id":  device.ID,
			"message_id": messageID,
			"error":      err,
		}).Error("Failed to publish response message to bus")
		return
	}

	a.logger.WithFields(logrus.Fields{
		"device_id":  device.ID,
		"message_id": messageID,
		"msg_type":   msgType,
	}).Info("Response message published to bus")
}

// detectResponseType 检测响应类型
func (a *Adapter) detectResponseType(topic string) string {
	// Topic 格式:
	// - devices/command/response/{message_id} → "command_response"
	// - devices/attributes/set/response/{message_id} → "attribute_set_response"
	// - gateway/command/response/{message_id} → "gateway_command_response"
	// - gateway/attributes/set/response/{message_id} → "gateway_attribute_set_response"

	if strings.Contains(topic, "command/response") {
		if strings.HasPrefix(topic, "gateway/") {
			return uplink.MessageTypeGatewayCommandResponse
		}
		return uplink.MessageTypeCommandResponse
	}

	if strings.Contains(topic, "attributes/set/response") {
		if strings.HasPrefix(topic, "gateway/") {
			return uplink.MessageTypeGatewayAttributeSetResponse
		}
		return uplink.MessageTypeAttributeSetResponse
	}

	return "unknown_response"
}

// genSharedTopic 生成共享订阅 Topic（用于负载均衡）
func genSharedTopic(topic string) string {
	return "$share/mygroup/" + topic
}

// SubscribeDeviceTopics 订阅设备上行 Topic（供 MQTT 服务初始化时调用）
func (a *Adapter) SubscribeDeviceTopics(client mqtt.Client) error {
	topics := map[string]struct {
		qos      byte
		handler  mqtt.MessageHandler
		describe string
	}{
		TopicPatternTelemetry: {
			qos:      1,
			handler:  a.handleTelemetryMessage,
			describe: "设备遥测上报",
		},
		TopicPatternAttribute: {
			qos:      1,
			handler:  a.handleAttributeMessage,
			describe: "设备属性上报",
		},
		TopicPatternEvent: {
			qos:      1,
			handler:  a.handleEventMessage,
			describe: "设备事件上报",
		},
		TopicPatternStatus: {
			qos:      1,
			handler:  a.handleStatusMessage,
			describe: "设备状态上报",
		},
	}

	for topic, config := range topics {
		// 使用共享订阅（VerneMQ 支持）
		sharedTopic := genSharedTopic(topic)
		token := client.Subscribe(sharedTopic, config.qos, config.handler)
		token.Wait()
		if err := token.Error(); err != nil {
			a.logger.WithFields(logrus.Fields{
				"topic": sharedTopic,
				"error": err,
			}).Error("Failed to subscribe device topic")
			return err
		}
		a.logger.WithFields(logrus.Fields{
			"topic":    sharedTopic,
			"describe": config.describe,
		}).Info("Subscribed to device topic")
	}

	return nil
}

// SubscribeGatewayTopics 订阅网关上行 Topic（供 MQTT 服务初始化时调用）
func (a *Adapter) SubscribeGatewayTopics(client mqtt.Client) error {
	topics := map[string]struct {
		qos      byte
		handler  mqtt.MessageHandler
		describe string
	}{
		TopicPatternGatewayTelemetry: {
			qos:      0,
			handler:  a.handleTelemetryMessage,
			describe: "网关遥测上报",
		},
		TopicPatternGatewayAttribute: {
			qos:      1,
			handler:  a.handleAttributeMessage,
			describe: "网关属性上报",
		},
		TopicPatternGatewayEvent: {
			qos:      1,
			handler:  a.handleEventMessage,
			describe: "网关事件上报",
		},
	}

	for topic, config := range topics {
		// 使用共享订阅（VerneMQ 支持）
		sharedTopic := genSharedTopic(topic)
		token := client.Subscribe(sharedTopic, config.qos, config.handler)
		token.Wait()
		if err := token.Error(); err != nil {
			a.logger.WithFields(logrus.Fields{
				"topic": sharedTopic,
				"error": err,
			}).Error("Failed to subscribe gateway topic")
			return err
		}
		a.logger.WithFields(logrus.Fields{
			"topic":    sharedTopic,
			"describe": config.describe,
		}).Info("Subscribed to gateway topic")
	}

	return nil
}

// handleTelemetryMessage 处理遥测消息（MQTT 回调函数）
func (a *Adapter) handleTelemetryMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.WithFields(logrus.Fields{
		"topic":        topic,
		"payload_size": len(payload),
	}).Debug("Received telemetry message")

	// 直接调用 Adapter 的处理方法（会发送到 Bus）
	if err := a.HandleTelemetryMessage(payload, topic); err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Failed to handle telemetry message")
	}
}

// handleAttributeMessage 处理属性消息（MQTT 回调函数）
func (a *Adapter) handleAttributeMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.WithFields(logrus.Fields{
		"topic":        topic,
		"payload_size": len(payload),
	}).Debug("Received attribute message")

	// 调用处理方法并立即发送 ACK
	if err := a.HandleAttributeMessage(payload, topic); err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Failed to handle attribute message")
	}
}

// handleEventMessage 处理事件消息（MQTT 回调函数）
func (a *Adapter) handleEventMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.WithFields(logrus.Fields{
		"topic":        topic,
		"payload_size": len(payload),
	}).Debug("Received event message")

	// 调用处理方法并立即发送 ACK
	if err := a.HandleEventMessage(payload, topic); err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Failed to handle event message")
	}
}

// handleStatusMessage 处理状态消息（MQTT 回调函数）
func (a *Adapter) handleStatusMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"payload": string(payload),
	}).Debug("Received status message")

	// source = "status_message" 表示来自设备主动上报
	if err := a.HandleStatusMessage(payload, topic, "status_message"); err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Failed to handle status message")
	}
}

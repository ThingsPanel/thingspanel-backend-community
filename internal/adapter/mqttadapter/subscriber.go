package mqttadapter

import (
	"strings"
	"time"

	"project/initialize"
	"project/internal/flow"

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

	// 4. 构造 FlowMessage
	flowMsg := &FlowMessage{
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
			return flow.MessageTypeGatewayCommandResponse
		}
		return flow.MessageTypeCommandResponse
	}

	if strings.Contains(topic, "attributes/set/response") {
		if strings.HasPrefix(topic, "gateway/") {
			return flow.MessageTypeGatewayAttributeSetResponse
		}
		return flow.MessageTypeAttributeSetResponse
	}

	return "unknown_response"
}

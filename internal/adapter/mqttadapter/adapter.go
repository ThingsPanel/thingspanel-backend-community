package mqttadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"project/initialize"
	"project/internal/diagnostics"
	"project/internal/downlink"
	"project/internal/uplink"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// UplinkMessage Flow 层需要的消息格式（避免导入 flow 包）
type UplinkMessage struct {
	Type      string
	DeviceID  string
	TenantID  string
	Timestamp int64
	Payload   []byte
	Metadata  map[string]interface{}
}

// Adapter MQTT 适配器
// 负责将 MQTT 消息转换为统一的 DeviceMessage 格式
type Adapter struct {
	bus        *uplink.Bus
	mqttClient mqtt.Client
	logger     *logrus.Logger
}

// publicPayload MQTT 消息格式
type publicPayload struct {
	DeviceId string `json:"device_id"`
	Values   []byte `json:"values"`
}

// NewAdapter 创建 MQTT 适配器
func NewAdapter(bus *uplink.Bus, mqttClient mqtt.Client, logger *logrus.Logger) *Adapter {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &Adapter{
		bus:        bus,
		mqttClient: mqttClient,
		logger:     logger,
	}
}

// GetMQTTClient 获取 MQTT 客户端（供其他模块使用）
func (a *Adapter) GetMQTTClient() mqtt.Client {
	return a.mqttClient
}

// HandleTelemetryMessage 处理遥测消息
// 这个函数替换原来的 mqtt/subscribe/telemetry_message.go:TelemetryMessages()
func (a *Adapter) HandleTelemetryMessage(payload []byte, topic string) error {
	// 1. 验证 payload 格式
	telemetryPayload, err := a.verifyPayload(payload)
	if err != nil {
		// 记录诊断：适配器验证失败
		deviceID := a.extractDeviceIDFromPayload(payload)
		if deviceID != "" {
			diagnostics.GetInstance().RecordUplinkFailed(deviceID, diagnostics.StageAdapter, fmt.Sprintf("消息格式错误：%v", err))
		}
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Invalid telemetry payload")
		return err
	}

	// 2. 获取设备信息（从缓存）
	device, err := initialize.GetDeviceCacheById(telemetryPayload.DeviceId)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": telemetryPayload.DeviceId,
			"error":     err,
		}).Error("Device not found in cache")
		return err
	}

	// 3. 根据 Topic 判断是网关消息还是直连设备消息
	msgType := a.detectMessageType(topic, "telemetry")

	// 4. 构造 UplinkMessage
	msg := &UplinkMessage{
		Type:      msgType,
		DeviceID:  device.ID,
		TenantID:  device.TenantID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   telemetryPayload.Values,
		Metadata: map[string]interface{}{
			"device_id":       device.ID, // 只存储设备ID，避免对象序列化问题
			"topic":           topic,
			"source_protocol": "mqtt",
		},
	}

	// 5. 发送到 Bus
	if err := a.bus.Publish(msg); err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Failed to publish message to bus")
		return err
	}

	a.logger.WithFields(logrus.Fields{
		"device_id":  device.ID,
		"topic":      topic,
		"msg_type":   msgType,
		"is_gateway": msgType == "gateway_telemetry",
	}).Debug("Telemetry message published to bus via Flow layer")

	return nil
}

// HandleEventMessage 处理事件消息
// 这个函数替换原来的 mqtt/subscribe/event_message.go:DeviceEvent()
func (a *Adapter) HandleEventMessage(payload []byte, topic string) error {
	// 1. 解析 topic 获取 messageID
	messageID, err := a.parseAttributeOrEventTopic(topic)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Debug("Failed to parse message_id from topic, skipping response")
		// 继续处理，只是不发送响应
		messageID = ""
	}

	// 2. 验证 payload 格式
	eventPayload, err := a.verifyPayload(payload)
	if err != nil {
		// 记录诊断：适配器验证失败
		deviceID := a.extractDeviceIDFromPayload(payload)
		if deviceID != "" {
			diagnostics.GetInstance().RecordUplinkFailed(deviceID, diagnostics.StageAdapter, fmt.Sprintf("消息格式错误：%v", err))
		}
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Invalid event payload")
		// 发送错误响应
		a.publishEventResponse("", messageID, "", err)
		return err
	}

	// 3. 解析 method 字段（用于响应）
	method := a.parseEventMethod(eventPayload.Values)

	// 4. 获取设备信息（从缓存）
	device, err := initialize.GetDeviceCacheById(eventPayload.DeviceId)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": eventPayload.DeviceId,
			"error":     err,
		}).Error("Device not found in cache")
		// 发送错误响应
		a.publishEventResponse("", messageID, method, err)
		return err
	}

	// 5. 根据 Topic 判断消息类型
	msgType := a.detectMessageType(topic, "event")

	// 6. 构造 UplinkMessage
	msg := &UplinkMessage{
		Type:      msgType,
		DeviceID:  device.ID,
		TenantID:  device.TenantID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   eventPayload.Values,
		Metadata: map[string]interface{}{
			"device_id":       device.ID,
			"topic":           topic,
			"source_protocol": "mqtt",
		},
	}

	// 7. 发送到 Bus（异步处理）
	busErr := a.bus.Publish(msg)
	if busErr != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     busErr,
		}).Error("Failed to publish event message to bus")
	}

	// 8. 立即发送 ACK 响应（协议层行为，不等待业务处理完成）
	a.publishEventResponse(device.DeviceNumber, messageID, method, busErr)

	a.logger.WithFields(logrus.Fields{
		"device_id":  device.ID,
		"topic":      topic,
		"msg_type":   msgType,
		"is_gateway": msgType == "gateway_event",
		"message_id": messageID,
		"method":     method,
	}).Debug("Event message published to bus and ACK sent")

	return busErr
}

// HandleAttributeMessage 处理属性消息
// 这个函数替换原来的 mqtt/subscribe/attribute_message.go:DeviceAttributeReport()
func (a *Adapter) HandleAttributeMessage(payload []byte, topic string) error {
	// 1. 解析 topic 获取 messageID
	messageID, err := a.parseAttributeOrEventTopic(topic)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Debug("Failed to parse message_id from topic, skipping response")
		// 继续处理，只是不发送响应
		messageID = ""
	}

	// 2. 验证 payload 格式
	attributePayload, err := a.verifyPayload(payload)
	if err != nil {
		// 记录诊断：适配器验证失败
		deviceID := a.extractDeviceIDFromPayload(payload)
		if deviceID != "" {
			diagnostics.GetInstance().RecordUplinkFailed(deviceID, diagnostics.StageAdapter, fmt.Sprintf("消息格式错误：%v", err))
		}
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Invalid attribute payload")
		// 发送错误响应
		a.publishAttributeResponse("", messageID, err)
		return err
	}

	// 3. 获取设备信息（从缓存）
	device, err := initialize.GetDeviceCacheById(attributePayload.DeviceId)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": attributePayload.DeviceId,
			"error":     err,
		}).Error("Device not found in cache")
		// 发送错误响应
		a.publishAttributeResponse("", messageID, err)
		return err
	}

	// 4. 根据 Topic 判断消息类型
	msgType := a.detectMessageType(topic, "attribute")

	// 5. 构造 UplinkMessage
	msg := &UplinkMessage{
		Type:      msgType,
		DeviceID:  device.ID,
		TenantID:  device.TenantID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   attributePayload.Values,
		Metadata: map[string]interface{}{
			"device_id":       device.ID,
			"topic":           topic,
			"source_protocol": "mqtt",
		},
	}

	// 6. 发送到 Bus（异步处理）
	busErr := a.bus.Publish(msg)
	if busErr != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     busErr,
		}).Error("Failed to publish attribute message to bus")
	}

	// 7. 立即发送 ACK 响应（协议层行为，不等待业务处理完成）
	a.publishAttributeResponse(device.DeviceNumber, messageID, busErr)

	a.logger.WithFields(logrus.Fields{
		"device_id":  device.ID,
		"topic":      topic,
		"msg_type":   msgType,
		"is_gateway": msgType == "gateway_attribute",
		"message_id": messageID,
	}).Debug("Attribute message published to bus and ACK sent")

	return busErr
}

// HandleStatusMessage 处理状态消息
// topic: devices/status/{device_id}
// payload: "0" (离线) 或 "1" (在线)
// source: "status_message" (设备主动上报) / "heartbeat_expired" / "timeout_expired"
func (a *Adapter) HandleStatusMessage(payload []byte, topic string, source string) error {
	a.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"payload": string(payload),
		"source":  source,
	}).Debug("🔵 MQTTAdapter: HandleStatusMessage called")

	// 1. 从 topic 解析 device_id: devices/status/{device_id}
	parts := strings.Split(topic, "/")
	if len(parts) != 3 {
		return fmt.Errorf("invalid status topic format: %s (expected: devices/status/{device_id})", topic)
	}
	deviceID := parts[2]

	// 2. 获取设备信息
	device, err := initialize.GetDeviceCacheById(deviceID)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("❌ Device not found in cache")
		return err
	}

	// 3. 构造 UplinkMessage
	msg := &UplinkMessage{
		Type:      "status",
		DeviceID:  device.ID,
		TenantID:  device.TenantID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
		Metadata: map[string]interface{}{
			"device_id":       device.ID,
			"topic":           topic,
			"source_protocol": "mqtt",
			"source":          source, // 来源标识
		},
	}

	// 4. 发送到 Bus
	if err := a.bus.Publish(msg); err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"source":    source,
			"error":     err,
		}).Error("❌ Failed to publish status message to bus")
		return err
	}

	a.logger.WithFields(logrus.Fields{
		"device_id": device.ID,
		"topic":     topic,
		"source":    source,
		"status":    string(payload),
	}).Debug("✅ Status message published to bus successfully")

	return nil
}

// verifyPayload 验证 MQTT 消息格式
func (a *Adapter) verifyPayload(body []byte) (*publicPayload, error) {
	payload := &publicPayload{
		Values: make([]byte, 0),
	}

	if err := json.Unmarshal(body, payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	if len(payload.DeviceId) == 0 {
		return nil, errors.New("device_id cannot be empty")
	}

	if len(payload.Values) == 0 {
		return nil, errors.New("values cannot be empty")
	}

	return payload, nil
}

// detectMessageType 根据 Topic 检测消息类型(网关/直连)
// MQTT协议特定:通过主题前缀判断
// 其他协议(HTTP/CoAP)可以通过其他方式(URL路径/请求头等)
func (a *Adapter) detectMessageType(topic string, baseType string) string {
	// MQTT主题格式:
	// - 直连设备: devices/{type}/{device_id}
	// - 网关设备: gateway/{type}/{device_id}
	if len(topic) >= 8 && topic[:8] == "gateway/" {
		return "gateway_" + baseType
	}
	return baseType
}

// parseAttributeOrEventTopic 解析属性/事件 Topic 获取 messageID
// Topic 格式: devices/attributes/{messageID} 或 devices/event/{messageID}
// 返回: (messageID, error)
func (a *Adapter) parseAttributeOrEventTopic(topic string) (string, error) {
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid topic format: %s (expected at least 3 parts)", topic)
	}
	messageID := parts[2]
	if messageID == "" {
		return "", fmt.Errorf("message_id is empty in topic: %s", topic)
	}
	return messageID, nil
}

// parseEventMethod 从 event payload 中解析 method 字段
func (a *Adapter) parseEventMethod(payload []byte) string {
	var eventData struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(payload, &eventData); err != nil {
		a.logger.WithError(err).Debug("Failed to parse event method, using empty string")
		return ""
	}
	return eventData.Method
}

// extractDeviceIDFromPayload 从 payload 中提取 device_id（用于诊断记录）
func (a *Adapter) extractDeviceIDFromPayload(payload []byte) string {
	var tempPayload struct {
		DeviceId string `json:"device_id"`
	}
	if err := json.Unmarshal(payload, &tempPayload); err != nil {
		return ""
	}
	return tempPayload.DeviceId
}

// PublishMessage 实现 MessagePublisher 接口
// 根据设备信息构造Topic并发送MQTT消息
func (a *Adapter) PublishMessage(deviceNumber string, msgType downlink.MessageType, deviceType string, topicPrefix string, messageID string, qos byte, payload []byte) error {
	// 1. 根据设备类型和协议选择Topic基础路径
	// 协议插件：始终使用 devices 路径（插件自己处理层级关系）
	// MQTT协议：根据设备类型选择 devices 或 gateway
	isGateway := topicPrefix == "" && deviceType != "1"

	// 2. 构造完整Topic（使用 topics.go 中的构造函数）
	var topic string
	switch msgType {
	case downlink.MessageTypeTelemetry:
		if isGateway {
			topic = topicPrefix + BuildGatewayTelemetryControlTopic(deviceNumber)
		} else {
			topic = topicPrefix + BuildTelemetryControlTopic(deviceNumber)
		}
	case downlink.MessageTypeCommand:
		if isGateway {
			topic = topicPrefix + BuildGatewayCommandTopic(deviceNumber, messageID)
		} else {
			topic = topicPrefix + BuildCommandTopic(deviceNumber, messageID)
		}
	case downlink.MessageTypeAttributeSet:
		if isGateway {
			topic = topicPrefix + BuildGatewayAttributeSetTopic(deviceNumber, messageID)
		} else {
			topic = topicPrefix + BuildAttributeSetTopic(deviceNumber, messageID)
		}
	case downlink.MessageTypeAttributeGet:
		if isGateway {
			topic = topicPrefix + BuildGatewayAttributeGetTopic(deviceNumber)
		} else {
			topic = topicPrefix + BuildAttributeGetTopic(deviceNumber)
		}
	default:
		return fmt.Errorf("unknown message type: %s", msgType)
	}

	// 3. 发送MQTT消息
	token := a.mqttClient.Publish(topic, qos, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout: topic=%s", topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt publish failed: topic=%s, error=%w", topic, err)
	}

	a.logger.WithFields(logrus.Fields{
		"topic":        topic,
		"device_type":  deviceType,
		"msg_type":     msgType,
		"message_id":   messageID,
		"topic_prefix": topicPrefix,
		"qos":          qos,
	}).Debug("Message published successfully")

	return nil
}

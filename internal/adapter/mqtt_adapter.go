package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"project/initialize"
	"project/internal/flow"

	"github.com/sirupsen/logrus"
)

// FlowMessage Flow 层需要的消息格式（避免导入 flow 包）
type FlowMessage struct {
	Type      string
	DeviceID  string
	TenantID  string
	Timestamp int64
	Payload   []byte
	Metadata  map[string]interface{}
}

// MQTTAdapter MQTT 适配器
// 负责将 MQTT 消息转换为统一的 DeviceMessage 格式
type MQTTAdapter struct {
	bus    *flow.Bus
	logger *logrus.Logger
}

// publicPayload MQTT 消息格式
type publicPayload struct {
	DeviceId string `json:"device_id"`
	Values   []byte `json:"values"`
}

// NewMQTTAdapter 创建 MQTT 适配器
func NewMQTTAdapter(bus *flow.Bus, logger *logrus.Logger) *MQTTAdapter {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &MQTTAdapter{
		bus:    bus,
		logger: logger,
	}
}

// HandleTelemetryMessage 处理遥测消息
// 这个函数替换原来的 mqtt/subscribe/telemetry_message.go:TelemetryMessages()
func (a *MQTTAdapter) HandleTelemetryMessage(payload []byte, topic string) error {
	// 1. 验证 payload 格式
	telemetryPayload, err := a.verifyPayload(payload)
	if err != nil {
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

	// 4. 构造 FlowMessage
	msg := &FlowMessage{
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

// verifyPayload 验证 MQTT 消息格式
func (a *MQTTAdapter) verifyPayload(body []byte) (*publicPayload, error) {
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
func (a *MQTTAdapter) detectMessageType(topic string, baseType string) string {
	// MQTT主题格式:
	// - 直连设备: devices/{type}/{device_id}
	// - 网关设备: gateway/{type}/{device_id}
	if len(topic) >= 8 && topic[:8] == "gateway/" {
		return "gateway_" + baseType
	}
	return baseType
}

// HandleEventMessage 处理事件消息
// 这个函数替换原来的 mqtt/subscribe/event_message.go:DeviceEvent()
func (a *MQTTAdapter) HandleEventMessage(payload []byte, topic string) error {
	// 1. 验证 payload 格式
	eventPayload, err := a.verifyPayload(payload)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Invalid event payload")
		return err
	}

	// 2. 获取设备信息（从缓存）
	device, err := initialize.GetDeviceCacheById(eventPayload.DeviceId)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": eventPayload.DeviceId,
			"error":     err,
		}).Error("Device not found in cache")
		return err
	}

	// 3. 根据 Topic 判断消息类型
	msgType := a.detectMessageType(topic, "event")

	// 4. 构造 FlowMessage
	msg := &FlowMessage{
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

	// 5. 发送到 Bus
	if err := a.bus.Publish(msg); err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Failed to publish event message to bus")
		return err
	}

	a.logger.WithFields(logrus.Fields{
		"device_id":  device.ID,
		"topic":      topic,
		"msg_type":   msgType,
		"is_gateway": msgType == "gateway_event",
	}).Debug("Event message published to bus")

	return nil
}

// HandleAttributeMessage 处理属性消息
// 这个函数替换原来的 mqtt/subscribe/attribute_message.go:DeviceAttributeReport()
func (a *MQTTAdapter) HandleAttributeMessage(payload []byte, topic string) error {
	// 1. 验证 payload 格式
	attributePayload, err := a.verifyPayload(payload)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"topic": topic,
			"error": err,
		}).Error("Invalid attribute payload")
		return err
	}

	// 2. 获取设备信息（从缓存）
	device, err := initialize.GetDeviceCacheById(attributePayload.DeviceId)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": attributePayload.DeviceId,
			"error":     err,
		}).Error("Device not found in cache")
		return err
	}

	// 3. 根据 Topic 判断消息类型
	msgType := a.detectMessageType(topic, "attribute")

	// 4. 构造 FlowMessage
	msg := &FlowMessage{
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

	// 5. 发送到 Bus
	if err := a.bus.Publish(msg); err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Failed to publish attribute message to bus")
		return err
	}

	a.logger.WithFields(logrus.Fields{
		"device_id":  device.ID,
		"topic":      topic,
		"msg_type":   msgType,
		"is_gateway": msgType == "gateway_attribute",
	}).Debug("Attribute message published to bus")

	return nil
}

// HandleStatusMessage 处理状态消息
// topic: devices/status/{device_id}
// payload: "0" (离线) 或 "1" (在线)
// source: "status_message" (设备主动上报) / "heartbeat_expired" / "timeout_expired"
func (a *MQTTAdapter) HandleStatusMessage(payload []byte, topic string, source string) error {
	a.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"payload": string(payload),
		"source":  source,
	}).Info("🔵 MQTTAdapter: HandleStatusMessage called")

	// 1. 从 topic 解析 device_id: devices/status/{device_id}
	parts := strings.Split(topic, "/")
	if len(parts) != 3 {
		return fmt.Errorf("invalid status topic format: %s (expected: devices/status/{device_id})", topic)
	}
	deviceID := parts[2]

	a.logger.WithField("device_id", deviceID).Info("🔍 Parsed device_id from topic")

	// 2. 获取设备信息
	device, err := initialize.GetDeviceCacheById(deviceID)
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("❌ Device not found in cache")
		return err
	}

	a.logger.WithFields(logrus.Fields{
		"device_id": device.ID,
		"tenant_id": device.TenantID,
	}).Info("✅ Device found in cache")

	// 3. 构造 FlowMessage
	msg := &FlowMessage{
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

	a.logger.Info("📦 FlowMessage constructed, publishing to Bus")

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
	}).Info("✅ Status message published to bus successfully")

	return nil
}

// TODO: 后续实现其他消息类型的处理
// - HandleCommandMessage()

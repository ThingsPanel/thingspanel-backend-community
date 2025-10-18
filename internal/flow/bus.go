package flow

import (
	"encoding/json"
	"sync"

	"github.com/sirupsen/logrus"
)

// MessageType 定义消息类型
const (
	MessageTypeTelemetry = "telemetry"
	MessageTypeAttribute = "attribute"
	MessageTypeEvent     = "event"
	MessageTypeStatus    = "status"

	// ✨ 新增：响应类型（用于下行指令的响应）
	MessageTypeCommandResponse             = "command_response"
	MessageTypeAttributeSetResponse        = "attribute_set_response"
	MessageTypeGatewayCommandResponse      = "gateway_command_response"
	MessageTypeGatewayAttributeSetResponse = "gateway_attribute_set_response"
)

// Bus 消息总线
// 负责在 Adapter 和 Flow 之间分发消息
type Bus struct {
	// 按消息类型分发的 channel
	telemetryChan chan *DeviceMessage
	attributeChan chan *DeviceMessage
	eventChan     chan *DeviceMessage
	statusChan    chan *DeviceMessage

	// ✨ 新增：响应 channel
	responseChan chan *DeviceMessage

	// 缓冲区大小
	bufferSize int

	// 关闭标识
	closed bool
	mu     sync.RWMutex

	// 日志
	logger *logrus.Logger
}

// BusConfig Bus 配置
type BusConfig struct {
	BufferSize int // channel 缓冲区大小，默认 10000
}

// NewBus 创建消息总线
func NewBus(config BusConfig, logger *logrus.Logger) *Bus {
	if config.BufferSize <= 0 {
		config.BufferSize = 10000 // 默认缓冲区大小
	}

	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &Bus{
		telemetryChan: make(chan *DeviceMessage, config.BufferSize),
		attributeChan: make(chan *DeviceMessage, config.BufferSize),
		eventChan:     make(chan *DeviceMessage, config.BufferSize),
		statusChan:    make(chan *DeviceMessage, config.BufferSize),

		// ✨ 新增：响应 channel
		responseChan: make(chan *DeviceMessage, config.BufferSize),

		bufferSize: config.BufferSize,
		logger:     logger,
	}
}

// MessageLike 消息接口（避免循环导入）
type MessageLike interface{}

// Publish 发布消息到总线
func (b *Bus) Publish(msgInterface MessageLike) error {
	// 将 interface{} 转换为 DeviceMessage
	// 这里依赖运行时的结构体字段兼容性
	var msg *DeviceMessage

	// 通过 JSON 序列化/反序列化实现类型转换
	// adapter.FlowMessage 和 flow.DeviceMessage 结构完全一致
	switch v := msgInterface.(type) {
	case *DeviceMessage:
		msg = v
	default:
		// 使用 JSON 转换（adapter.FlowMessage -> flow.DeviceMessage）
		jsonData, err := json.Marshal(msgInterface)
		if err != nil {
			b.logger.WithError(err).Error("Failed to marshal message")
			return err
		}

		msg = &DeviceMessage{}
		if err := json.Unmarshal(jsonData, msg); err != nil {
			b.logger.WithError(err).Error("Failed to unmarshal message")
			return err
		}
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		b.logger.Warn("Bus is closed, message dropped")
		return ErrBusClosed
	}
	b.mu.RUnlock()

	// 根据消息类型路由到不同的 channel
	// 支持网关消息类型(gateway_telemetry/gateway_attribute/gateway_event)
	switch msg.Type {
	case MessageTypeTelemetry:
		select {
		case b.telemetryChan <- msg:
			// 发送成功
		default:
			// channel 满了，阻塞发送（背压机制）
			b.logger.Warnf("Telemetry channel full, blocking publish")
			b.telemetryChan <- msg
		}

	case MessageTypeAttribute:
		select {
		case b.attributeChan <- msg:
		default:
			b.logger.Warnf("Attribute channel full, blocking publish")
			b.attributeChan <- msg
		}

	case MessageTypeEvent:
		select {
		case b.eventChan <- msg:
		default:
			b.logger.Warnf("Event channel full, blocking publish")
			b.eventChan <- msg
		}

	case MessageTypeStatus:
		b.logger.WithFields(logrus.Fields{
			"device_id": msg.DeviceID,
			"type":      msg.Type,
		}).Info("📮 Bus: Routing status message to statusChan")

		select {
		case b.statusChan <- msg:
			b.logger.Info("✅ Status message sent to statusChan")
		default:
			b.logger.Warnf("Status channel full, blocking publish")
			b.statusChan <- msg
			b.logger.Info("✅ Status message sent (after blocking)")
		}

	// ✨ 新增：响应消息路由
	case MessageTypeCommandResponse,
		MessageTypeAttributeSetResponse,
		MessageTypeGatewayCommandResponse,
		MessageTypeGatewayAttributeSetResponse:
		return b.PublishResponse(msg)

	default:
		b.logger.Errorf("Unknown message type: %s", msg.Type)
		return ErrUnknownMessageType
	}

	return nil
}

// PublishResponse 发布响应消息
func (b *Bus) PublishResponse(msg *DeviceMessage) error {
	select {
	case b.responseChan <- msg:
		return nil
	default:
		b.logger.WithFields(logrus.Fields{
			"device_id": msg.DeviceID,
			"type":      msg.Type,
		}).Warn("Response channel is full, message dropped")
		return ErrChannelFull
	}
}

// SubscribeTelemetry 订阅遥测消息
func (b *Bus) SubscribeTelemetry() <-chan *DeviceMessage {
	return b.telemetryChan
}

// SubscribeAttribute 订阅属性消息
func (b *Bus) SubscribeAttribute() <-chan *DeviceMessage {
	return b.attributeChan
}

// SubscribeEvent 订阅事件消息
func (b *Bus) SubscribeEvent() <-chan *DeviceMessage {
	return b.eventChan
}

// SubscribeStatus 订阅状态消息
func (b *Bus) SubscribeStatus() <-chan *DeviceMessage {
	return b.statusChan
}

// ✨ 新增：订阅响应消息
func (b *Bus) SubscribeResponse() <-chan *DeviceMessage {
	return b.responseChan
}

// Close 关闭总线
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true

	// 关闭所有 channel
	close(b.telemetryChan)
	close(b.attributeChan)
	close(b.eventChan)
	close(b.statusChan)

	// ✨ 新增：关闭响应 channel
	close(b.responseChan)

	b.logger.Info("Bus closed")
}

// GetChannelStats 获取 channel 统计信息（用于监控）
func (b *Bus) GetChannelStats() map[string]interface{} {
	return map[string]interface{}{
		"telemetry_len": len(b.telemetryChan),
		"telemetry_cap": cap(b.telemetryChan),
		"attribute_len": len(b.attributeChan),
		"attribute_cap": cap(b.attributeChan),
		"event_len":     len(b.eventChan),
		"event_cap":     cap(b.eventChan),
		"status_len":    len(b.statusChan),
		"status_cap":    cap(b.statusChan),

		// ✨ 新增：响应队列统计
		"response_queue": len(b.responseChan),

		"buffer_size": b.bufferSize,
	}
}

// 错误定义

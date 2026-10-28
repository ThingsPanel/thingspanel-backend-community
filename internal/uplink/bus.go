package uplink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const defaultPublishTimeout = 30 * time.Second

// MessageType 定义消息类型
const (
	MessageTypeTelemetry = "telemetry"
	MessageTypeAttribute = "attribute"
	MessageTypeEvent     = "event"
	MessageTypeStatus    = "status"

	MessageTypeCommandResponse             = "command_response"
	MessageTypeAttributeSetResponse        = "attribute_set_response"
	MessageTypeGatewayCommandResponse      = "gateway_command_response"
	MessageTypeGatewayAttributeSetResponse = "gateway_attribute_set_response"
)

// Bus 消息总线
// 负责将 MQTT Adapter 消息转发到 Flow；关闭总线会取消等待中的发布。
type Bus struct {
	telemetryChan chan *DeviceMessage
	attributeChan chan *DeviceMessage
	eventChan     chan *DeviceMessage
	statusChan    chan *DeviceMessage
	responseChan  chan *DeviceMessage
	bufferSize    int

	closed    bool // protected by mu
	mu        sync.RWMutex
	closeOnce sync.Once
	done      chan struct{}
	logger    *logrus.Logger
}

// BusConfig Bus 配置
type BusConfig struct {
	BufferSize int // 每类消息 channel 的缓冲区大小，默认 10000
}

// NewBus 创建消息总线
func NewBus(config BusConfig, logger *logrus.Logger) *Bus {
	if config.BufferSize <= 0 {
		config.BufferSize = 10000
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Bus{
		telemetryChan: make(chan *DeviceMessage, config.BufferSize),
		attributeChan: make(chan *DeviceMessage, config.BufferSize),
		eventChan:     make(chan *DeviceMessage, config.BufferSize),
		statusChan:    make(chan *DeviceMessage, config.BufferSize),
		responseChan:  make(chan *DeviceMessage, config.BufferSize),
		bufferSize:    config.BufferSize,
		done:          make(chan struct{}),
		logger:        logger,
	}
}

// MessageLike 消息接口（避免导入 adapter 包）
type MessageLike interface{}

// Publish performs a bounded publish for callers that do not own a context.
// A full queue is returned as ErrChannelFull after the default wait interval.
func (b *Bus) Publish(msgInterface MessageLike) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultPublishTimeout)
	defer cancel()
	err := b.PublishContext(ctx, msgInterface)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: publish timed out after %s", ErrChannelFull, defaultPublishTimeout)
	}
	return err
}

// PublishContext routes a message to its channel. It returns when accepted,
// canceled, the queue wait expires, or the bus closes; it never drops a message.
func (b *Bus) PublishContext(ctx context.Context, msgInterface MessageLike) error {
	if ctx == nil {
		return errors.New("publish context is nil")
	}
	msg, err := toDeviceMessage(msgInterface, b.logger)
	if err != nil {
		return err
	}
	channel, err := b.channelFor(msg)
	if err != nil {
		return err
	}
	return b.publishTo(ctx, channel, msg)
}

func toDeviceMessage(value MessageLike, logger *logrus.Logger) (*DeviceMessage, error) {
	if msg, ok := value.(*DeviceMessage); ok {
		if msg == nil {
			return nil, errors.New("message is nil")
		}
		return msg, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		logger.WithError(err).Error("Failed to marshal message")
		return nil, err
	}
	msg := &DeviceMessage{}
	if err := json.Unmarshal(data, msg); err != nil {
		logger.WithError(err).Error("Failed to unmarshal message")
		return nil, err
	}
	return msg, nil
}

func (b *Bus) channelFor(msg *DeviceMessage) (chan *DeviceMessage, error) {
	switch msg.Type {
	case MessageTypeTelemetry, "gateway_telemetry":
		return b.telemetryChan, nil
	case MessageTypeAttribute, "gateway_attribute":
		return b.attributeChan, nil
	case MessageTypeEvent, "gateway_event":
		return b.eventChan, nil
	case MessageTypeStatus:
		return b.statusChan, nil
	case MessageTypeCommandResponse, MessageTypeAttributeSetResponse,
		MessageTypeGatewayCommandResponse, MessageTypeGatewayAttributeSetResponse:
		return b.responseChan, nil
	default:
		b.logger.Errorf("Unknown message type: %s", msg.Type)
		return nil, ErrUnknownMessageType
	}
}

func (b *Bus) publishTo(ctx context.Context, channel chan *DeviceMessage, msg *DeviceMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}
	// Keep the read lock through send. Close first cancels done, then takes the
	// write lock, so it can wake blocked publishers before closing any channel.
	select {
	case channel <- msg:
		b.mu.RUnlock()
		return nil
	case <-ctx.Done():
		b.mu.RUnlock()
		return ctx.Err()
	case <-b.done:
		b.mu.RUnlock()
		return ErrBusClosed
	}
}

// PublishResponse preserves the response queue's immediate full check.
func (b *Bus) PublishResponse(msg *DeviceMessage) error {
	if msg == nil {
		return errors.New("message is nil")
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBusClosed
	}
	select {
	case b.responseChan <- msg:
		return nil
	case <-b.done:
		return ErrBusClosed
	default:
		b.logger.WithFields(logrus.Fields{
			"device_id": msg.DeviceID,
			"type":      msg.Type,
		}).Warn("Response channel is full")
		return ErrChannelFull
	}
}

// PublishResponseContext publishes a response with caller-controlled backpressure.
func (b *Bus) PublishResponseContext(ctx context.Context, msg *DeviceMessage) error {
	if msg == nil {
		return errors.New("message is nil")
	}
	return b.publishTo(ctx, b.responseChan, msg)
}

// PublishStatusOffline 实现 StatusPublisher 接口
func (b *Bus) PublishStatusOffline(deviceID, source string) error {
	return b.Publish(&DeviceMessage{
		Type:     MessageTypeStatus,
		DeviceID: deviceID,
		Payload:  []byte("0"),
		Metadata: map[string]interface{}{"source": source},
	})
}

func (b *Bus) SubscribeTelemetry() <-chan *DeviceMessage { return b.telemetryChan }
func (b *Bus) SubscribeAttribute() <-chan *DeviceMessage { return b.attributeChan }
func (b *Bus) SubscribeEvent() <-chan *DeviceMessage     { return b.eventChan }
func (b *Bus) SubscribeStatus() <-chan *DeviceMessage    { return b.statusChan }
func (b *Bus) SubscribeResponse() <-chan *DeviceMessage  { return b.responseChan }

// Close cancels blocked publishes before closing channels. The write lock waits
// for every in-flight publisher to leave its read-locked send section.
func (b *Bus) Close() {
	b.closeOnce.Do(func() { close(b.done) })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.telemetryChan)
	close(b.attributeChan)
	close(b.eventChan)
	close(b.statusChan)
	close(b.responseChan)
	b.logger.Info("Bus closed")
}

// GetChannelStats 获取 channel 统计信息
func (b *Bus) GetChannelStats() map[string]interface{} {
	return map[string]interface{}{
		"telemetry_len": len(b.telemetryChan), "telemetry_cap": cap(b.telemetryChan),
		"attribute_len": len(b.attributeChan), "attribute_cap": cap(b.attributeChan),
		"event_len": len(b.eventChan), "event_cap": cap(b.eventChan),
		"status_len": len(b.statusChan), "status_cap": cap(b.statusChan),
		"response_queue": len(b.responseChan), "buffer_size": b.bufferSize,
	}
}

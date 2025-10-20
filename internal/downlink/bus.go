package downlink

import (
	"context"
	"sync"
)

// Bus 下行消息总线
type Bus struct {
	commandChan      chan *Message
	attributeSetChan chan *Message
	attributeGetChan chan *Message
	telemetryChan    chan *Message
	bufferSize       int
	wg               sync.WaitGroup
}

// NewBus 创建消息总线
func NewBus(bufferSize int) *Bus {
	return &Bus{
		commandChan:      make(chan *Message, bufferSize),
		attributeSetChan: make(chan *Message, bufferSize),
		attributeGetChan: make(chan *Message, bufferSize),
		telemetryChan:    make(chan *Message, bufferSize),
		bufferSize:       bufferSize,
	}
}

// PublishCommand 发布命令下发消息
func (b *Bus) PublishCommand(msg *Message) {
	b.commandChan <- msg
}

// PublishAttributeSet 发布属性设置消息
func (b *Bus) PublishAttributeSet(msg *Message) {
	b.attributeSetChan <- msg
}

// PublishAttributeGet 发布属性获取消息
func (b *Bus) PublishAttributeGet(msg *Message) {
	b.attributeGetChan <- msg
}

// PublishTelemetry 发布遥测下发消息
func (b *Bus) PublishTelemetry(msg *Message) {
	b.telemetryChan <- msg
}

// SubscribeCommand 订阅命令消息
func (b *Bus) SubscribeCommand() <-chan *Message {
	return b.commandChan
}

// SubscribeAttributeSet 订阅属性设置消息
func (b *Bus) SubscribeAttributeSet() <-chan *Message {
	return b.attributeSetChan
}

// SubscribeAttributeGet 订阅属性获取消息
func (b *Bus) SubscribeAttributeGet() <-chan *Message {
	return b.attributeGetChan
}

// SubscribeTelemetry 订阅遥测下发消息
func (b *Bus) SubscribeTelemetry() <-chan *Message {
	return b.telemetryChan
}

// Close 关闭总线
func (b *Bus) Close() {
	close(b.commandChan)
	close(b.attributeSetChan)
	close(b.attributeGetChan)
	close(b.telemetryChan)
	b.wg.Wait()
}

// Start 启动总线（与 Handler 配合使用）
func (b *Bus) Start(ctx context.Context, handler *Handler) {
	// 启动命令处理协程
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-b.commandChan:
				if msg != nil {
					handler.HandleCommand(ctx, msg)
				}
			}
		}
	}()

	// 启动属性设置处理协程
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-b.attributeSetChan:
				if msg != nil {
					handler.HandleAttributeSet(ctx, msg)
				}
			}
		}
	}()

	// 启动属性获取处理协程
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-b.attributeGetChan:
				if msg != nil {
					handler.HandleAttributeGet(ctx, msg)
				}
			}
		}
	}()

	// 启动遥测下发处理协程
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-b.telemetryChan:
				if msg != nil {
					handler.HandleTelemetry(ctx, msg)
				}
			}
		}
	}()
}

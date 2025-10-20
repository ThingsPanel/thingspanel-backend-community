package uplink

import "errors"

var (
	// ErrBusClosed Bus 已关闭
	ErrBusClosed = errors.New("bus is closed")

	// ErrUnknownMessageType 未知的消息类型
	ErrUnknownMessageType = errors.New("unknown message type")

	ErrChannelFull = errors.New("channel is full") // ✨ 新增

	// ErrUplinkStopped Flow 已停止
	ErrUplinkStopped = errors.New("flow is stopped")

	// ErrProcessorFailed 数据处理失败
	ErrProcessorFailed = errors.New("processor failed")

	// ErrInvalidPayload 无效的 payload
	ErrInvalidPayload = errors.New("invalid payload")
)

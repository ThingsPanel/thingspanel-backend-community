package adapter

import "time"

// MessageType 消息类型
type MessageType string

const (
	// 直连设备消息类型
	MessageTypeTelemetry MessageType = "telemetry" // 遥测数据
	MessageTypeAttribute MessageType = "attribute" // 属性数据
	MessageTypeEvent     MessageType = "event"     // 事件数据
	MessageTypeCommand   MessageType = "command"   // 命令响应

	// 网关消息类型(用于区分需要拆分的网关批量消息)
	MessageTypeGatewayTelemetry MessageType = "gateway_telemetry" // 网关遥测数据
	MessageTypeGatewayAttribute MessageType = "gateway_attribute" // 网关属性数据
	MessageTypeGatewayEvent     MessageType = "gateway_event"     // 网关事件数据
)

// IsGatewayMessage 判断是否为网关消息类型
func (t MessageType) IsGatewayMessage() bool {
	return t == MessageTypeGatewayTelemetry ||
		t == MessageTypeGatewayAttribute ||
		t == MessageTypeGatewayEvent
}

// DeviceMessage 统一设备消息格式
// 用于在 Adapter 和 Flow 层之间传递消息
type DeviceMessage struct {
	// 消息类型
	Type MessageType `json:"type"`

	// 设备信息
	DeviceID string `json:"device_id"` // 设备ID
	TenantID string `json:"tenant_id"` // 租户ID

	// 时间戳（毫秒）
	Timestamp int64 `json:"timestamp"`

	// 原始数据（字节流）
	// - 对于上行数据：设备发送的原始字节
	// - 对于下行数据：平台下发的原始字节
	Payload []byte `json:"payload"`

	// 元数据（可选）
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// NewDeviceMessage 创建设备消息
func NewDeviceMessage(msgType MessageType, deviceID, tenantID string, payload []byte) *DeviceMessage {
	return &DeviceMessage{
		Type:      msgType,
		DeviceID:  deviceID,
		TenantID:  tenantID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
		Metadata:  make(map[string]interface{}),
	}
}

// SetMetadata 设置元数据
func (m *DeviceMessage) SetMetadata(key string, value interface{}) *DeviceMessage {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	m.Metadata[key] = value
	return m
}

// GetMetadata 获取元数据
func (m *DeviceMessage) GetMetadata(key string) (interface{}, bool) {
	if m.Metadata == nil {
		return nil, false
	}
	val, ok := m.Metadata[key]
	return val, ok
}

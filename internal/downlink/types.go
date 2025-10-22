package downlink

import "encoding/json"

// MessageType 下行消息类型
type MessageType string

const (
	MessageTypeCommand      MessageType = "command"       // 命令下发
	MessageTypeAttributeSet MessageType = "attribute_set" // 属性设置
	MessageTypeAttributeGet MessageType = "attribute_get" // 属性获取
	MessageTypeTelemetry    MessageType = "telemetry"     // 遥测数据下发
)

// Message 下行消息（Service 传递给 downlink 的数据）
type Message struct {
	DeviceID       string          // 设备 ID（Service 层已处理网关子设备 ID）
	DeviceNumber   string          // 目标设备编号（网关/子设备时为顶层网关编号）
	DeviceType     string          // 设备类型："1"=直连,"2"=网关,"3"=子设备
	DeviceConfigID string          // 设备配置 ID（用于查找脚本）
	Type           MessageType     // 消息类型
	Data           json.RawMessage // 标准化数据（JSON 格式）
	Topic          string          // MQTT Topic（已废弃，由Adapter构造）
	TopicPrefix    string          // 协议插件Topic前缀（MQTT为空）
	MessageID      string          // 消息 ID（用于日志关联）
}

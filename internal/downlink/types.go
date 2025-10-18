package downlink

import "encoding/json"

// MessageType 下行消息类型
type MessageType string

const (
	MessageTypeCommand      MessageType = "command"       // 命令下发
	MessageTypeAttributeSet MessageType = "attribute_set" // 属性设置
)

// Message 下行消息（Service 传递给 downlink 的数据）
type Message struct {
	DeviceID       string          // 设备 ID（Service 层已处理网关子设备 ID）
	DeviceConfigID string          // 设备配置 ID（用于查找脚本）
	Type           MessageType     // 消息类型
	Data           json.RawMessage // 标准化数据（JSON 格式）
	Topic          string          // MQTT Topic（Service 层构造好）
	MessageID      string          // 消息 ID（用于日志关联）✨ 新增
}

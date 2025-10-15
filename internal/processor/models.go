package processor

import "encoding/json"

// DecodeInput 上行数据解码输入
type DecodeInput struct {
	DeviceConfigID string   `json:"device_config_id"` // 设备配置ID（用于查找脚本）*必填
	Type           DataType `json:"type"`             // 数据类型：telemetry/attribute/event *必填
	RawData        []byte   `json:"raw_data"`         // 原始字节数据 *必填
	Timestamp      int64    `json:"timestamp"`        // 时间戳（毫秒）可选
}

// DecodeOutput 上行数据解码输出
type DecodeOutput struct {
	Success   bool            `json:"success"`   // 执行是否成功
	Data      json.RawMessage `json:"data"`      // 标准化后的数据（JSON格式）
	Timestamp int64           `json:"timestamp"` // 处理时间戳
	Error     error           `json:"error"`     // 错误信息（Success=false 时有值）
}

// EncodeInput 下行数据编码输入
type EncodeInput struct {
	DeviceConfigID string          `json:"device_config_id"` // 设备配置ID（用于查找脚本）*必填
	Type           DataType         `json:"type"`             // 数据类型：telemetry_control/attribute_set/command *必填
	Data           json.RawMessage  `json:"data"`             // 标准化数据（JSON格式）*必填
	Timestamp      int64            `json:"timestamp"`        // 时间戳（毫秒）可选
}

// EncodeOutput 下行数据编码输出
type EncodeOutput struct {
	Success     bool   `json:"success"`      // 执行是否成功
	EncodedData []byte `json:"encoded_data"` // 编码后的设备协议数据（字节流）
	Error       error  `json:"error"`        // 错误信息（Success=false 时有值）
}

// Validate 验证 DecodeInput 参数
func (i *DecodeInput) Validate() error {
	if i.DeviceConfigID == "" {
		return NewInvalidInputError("device_config_id is required")
	}
	if i.Type == "" {
		return NewInvalidInputError("type is required")
	}
	if len(i.RawData) == 0 {
		return NewInvalidInputError("raw_data is required")
	}
	// 验证 type 是否为上行类型
	if i.Type != DataTypeTelemetry && i.Type != DataTypeAttribute && i.Type != DataTypeEvent {
		return NewInvalidInputError("invalid type for decode, expected: telemetry/attribute/event")
	}
	return nil
}

// Validate 验证 EncodeInput 参数
func (i *EncodeInput) Validate() error {
	if i.DeviceConfigID == "" {
		return NewInvalidInputError("device_config_id is required")
	}
	if i.Type == "" {
		return NewInvalidInputError("type is required")
	}
	if len(i.Data) == 0 {
		return NewInvalidInputError("data is required")
	}
	// 验证 type 是否为下行类型
	if i.Type != DataTypeTelemetryControl && i.Type != DataTypeAttributeSet && i.Type != DataTypeCommand {
		return NewInvalidInputError("invalid type for encode, expected: telemetry_control/attribute_set/command")
	}
	return nil
}

// CachedScript 缓存的脚本结构
type CachedScript struct {
	ID         string `json:"id"`          // 脚本ID
	Content    string `json:"content"`     // 脚本内容
	EnableFlag string `json:"enable_flag"` // 启用标识 Y/N
	ScriptType string `json:"script_type"` // 脚本类型
}

// IsEnabled 检查脚本是否启用
func (s *CachedScript) IsEnabled() bool {
	return s.EnableFlag == EnableFlagEnabled
}

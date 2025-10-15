package processor

import "context"

// DataProcessor 数据处理器核心接口
type DataProcessor interface {
	// Decode 上行数据解码：设备协议数据 -> 标准化数据
	// 用于：telemetry、attribute、event
	Decode(ctx context.Context, input *DecodeInput) (*DecodeOutput, error)

	// Encode 下行数据编码：标准化数据 -> 设备协议数据
	// 用于：telemetry_control、attribute_set、command
	Encode(ctx context.Context, input *EncodeInput) (*EncodeOutput, error)
}

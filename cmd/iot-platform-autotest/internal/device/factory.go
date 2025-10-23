package device

import (
	"fmt"

	"go.uber.org/zap"

	"iot-platform-autotest/internal/config"
)

// DeviceType 设备类型
type DeviceType string

const (
	// DeviceTypeDirect 直连设备
	DeviceTypeDirect DeviceType = "direct"
	// DeviceTypeGateway 网关设备
	DeviceTypeGateway DeviceType = "gateway"
)

// NewDevice 根据配置创建设备实例
func NewDevice(cfg *config.Config, logger *zap.Logger) (Device, error) {
	switch DeviceType(cfg.DeviceType) {
	case DeviceTypeDirect:
		return NewDirectDevice(cfg, logger), nil
	case DeviceTypeGateway:
		// TODO: 网关设备将在后续实现
		return nil, fmt.Errorf("gateway device not implemented yet")
	default:
		return nil, fmt.Errorf("unknown device type: %s", cfg.DeviceType)
	}
}

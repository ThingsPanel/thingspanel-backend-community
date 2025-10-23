package device

// SubDeviceConfig 子设备配置
type SubDeviceConfig struct {
	SubDeviceNumber string `yaml:"sub_device_number"` // 子设备编号
	DeviceID        string `yaml:"device_id"`         // 设备ID
	Description     string `yaml:"description"`       // 描述
}

// SubGatewayConfig 子网关配置
type SubGatewayConfig struct {
	SubGatewayNumber string            `yaml:"sub_gateway_number"` // 子网关编号
	DeviceID         string            `yaml:"device_id"`          // 设备ID
	Description      string            `yaml:"description"`        // 描述
	SubDevices       []SubDeviceConfig `yaml:"sub_devices"`        // 子网关下的子设备
}

// GatewayTopology 网关拓扑结构
type GatewayTopology struct {
	SubDevices  []SubDeviceConfig  `yaml:"sub_devices"`  // 直连子设备
	SubGateways []SubGatewayConfig `yaml:"sub_gateways"` // 子网关
}

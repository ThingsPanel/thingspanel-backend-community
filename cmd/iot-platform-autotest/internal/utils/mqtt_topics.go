package utils

import "fmt"

// MQTTTopics MQTT主题构建器
type MQTTTopics struct {
	deviceNumber string
	isGateway    bool // 是否为网关设备
}

// NewMQTTTopics 创建主题构建器（直连设备）
func NewMQTTTopics(deviceNumber string) *MQTTTopics {
	return &MQTTTopics{
		deviceNumber: deviceNumber,
		isGateway:    false,
	}
}

// NewGatewayMQTTTopics 创建网关主题构建器
func NewGatewayMQTTTopics(deviceNumber string) *MQTTTopics {
	return &MQTTTopics{
		deviceNumber: deviceNumber,
		isGateway:    true,
	}
}

// 设备上报主题
func (t *MQTTTopics) Telemetry() string {
	if t.isGateway {
		return "gateway/telemetry"
	}
	return "devices/telemetry"
}

func (t *MQTTTopics) Attributes(messageID string) string {
	if t.isGateway {
		return fmt.Sprintf("gateway/attributes/%s", messageID)
	}
	return fmt.Sprintf("devices/attributes/%s", messageID)
}

func (t *MQTTTopics) Event(messageID string) string {
	if t.isGateway {
		return fmt.Sprintf("gateway/event/%s", messageID)
	}
	return fmt.Sprintf("devices/event/%s", messageID)
}

func (t *MQTTTopics) CommandResponse(messageID string) string {
	if t.isGateway {
		return fmt.Sprintf("gateway/command/response/%s", messageID)
	}
	return fmt.Sprintf("devices/command/response/%s", messageID)
}

func (t *MQTTTopics) AttributeSetResponse(messageID string) string {
	if t.isGateway {
		return fmt.Sprintf("gateway/attributes/set/response/%s", messageID)
	}
	return fmt.Sprintf("devices/attributes/set/response/%s", messageID)
}

func (t *MQTTTopics) OTAProgress() string {
	return "ota/devices/progress"
}

// 设备订阅主题
func (t *MQTTTopics) TelemetryControl() string {
	if t.isGateway {
		return fmt.Sprintf("gateway/telemetry/control/%s", t.deviceNumber)
	}
	return fmt.Sprintf("devices/telemetry/control/%s", t.deviceNumber)
}

func (t *MQTTTopics) AttributeSet() string {
	if t.isGateway {
		return fmt.Sprintf("gateway/attributes/set/%s/+", t.deviceNumber)
	}
	return fmt.Sprintf("devices/attributes/set/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) AttributeGet() string {
	if t.isGateway {
		return fmt.Sprintf("gateway/attributes/get/%s", t.deviceNumber)
	}
	return fmt.Sprintf("devices/attributes/get/%s", t.deviceNumber)
}

func (t *MQTTTopics) Command() string {
	if t.isGateway {
		return fmt.Sprintf("gateway/command/%s/+", t.deviceNumber)
	}
	return fmt.Sprintf("devices/command/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) AttributeResponse() string {
	if t.isGateway {
		return fmt.Sprintf("gateway/attributes/response/%s/+", t.deviceNumber)
	}
	return fmt.Sprintf("devices/attributes/response/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) EventResponse() string {
	if t.isGateway {
		return fmt.Sprintf("gateway/event/response/%s/+", t.deviceNumber)
	}
	return fmt.Sprintf("devices/event/response/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) OTAInform() string {
	return fmt.Sprintf("ota/devices/inform/%s", t.deviceNumber)
}

package utils

import "fmt"

// MQTTTopics MQTT主题构建器
type MQTTTopics struct {
	deviceNumber string
}

// NewMQTTTopics 创建主题构建器
func NewMQTTTopics(deviceNumber string) *MQTTTopics {
	return &MQTTTopics{
		deviceNumber: deviceNumber,
	}
}

// 设备上报主题
func (t *MQTTTopics) Telemetry() string {
	return "devices/telemetry"
}

func (t *MQTTTopics) Attributes(messageID string) string {
	return fmt.Sprintf("devices/attributes/%s", messageID)
}

func (t *MQTTTopics) Event(messageID string) string {
	return fmt.Sprintf("devices/event/%s", messageID)
}

func (t *MQTTTopics) CommandResponse(messageID string) string {
	return fmt.Sprintf("devices/command/response/%s", messageID)
}

func (t *MQTTTopics) AttributeSetResponse(messageID string) string {
	return fmt.Sprintf("devices/attributes/set/response/%s", messageID)
}

func (t *MQTTTopics) OTAProgress() string {
	return "ota/devices/progress"
}

// 设备订阅主题
func (t *MQTTTopics) TelemetryControl() string {
	return fmt.Sprintf("devices/telemetry/control/%s", t.deviceNumber)
}

func (t *MQTTTopics) AttributeSet() string {
	return fmt.Sprintf("devices/attributes/set/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) AttributeGet() string {
	return fmt.Sprintf("devices/attributes/get/%s", t.deviceNumber)
}

func (t *MQTTTopics) Command() string {
	return fmt.Sprintf("devices/command/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) AttributeResponse() string {
	return fmt.Sprintf("devices/attributes/response/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) EventResponse() string {
	return fmt.Sprintf("devices/event/response/%s/+", t.deviceNumber)
}

func (t *MQTTTopics) OTAInform() string {
	return fmt.Sprintf("ota/devices/inform/%s", t.deviceNumber)
}

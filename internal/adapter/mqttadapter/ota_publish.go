package mqttadapter

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

func (a *Adapter) PublishRaw(topic string, qos byte, payload []byte) error {
	if a == nil || a.mqttClient == nil {
		return fmt.Errorf("mqtt adapter is not ready")
	}
	token := a.mqttClient.Publish(topic, qos, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout: topic=%s", topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt publish failed: topic=%s, error=%w", topic, err)
	}
	logrus.WithField("topic", topic).Info("OTA MQTT published")
	return nil
}

func PublishOTAInform(deviceNumber string, payload []byte) error {
	if globalMQTTAdapter == nil {
		return fmt.Errorf("mqtt adapter is not ready")
	}
	topics := []string{
		"ota/devices/infrom/" + deviceNumber, // 官方配置拼写
		"ota/devices/inform/" + deviceNumber, // 文档正确拼写
	}
	var last error
	for _, topic := range topics {
		if err := globalMQTTAdapter.PublishRaw(topic, 1, payload); err != nil {
			last = err
			logrus.WithError(err).WithField("topic", topic).Error("OTA inform publish failed")
		}
	}
	return last
}

var globalMQTTAdapter *Adapter

func SetGlobalAdapter(a *Adapter) {
	globalMQTTAdapter = a
}

package mqttadapter

import (
	"project/pkg/common"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// Response delivery must not hold a durable-ingress callback indefinitely during
// shutdown or a broker outage. Legacy profile retains its existing wait policy.
func (a *Adapter) waitResponseToken(token mqtt.Token) error {
	if a.inputProfile == "ingress_v1" {
		return WaitTokenContext(a.adapterContext(), token, 5*time.Second)
	}
	token.Wait()
	return token.Error()
}

// publishAttributeResponse 发送属性上报 ACK 响应
// 协议层行为：告诉设备"我收到了你的属性上报"
func (a *Adapter) publishAttributeResponse(deviceNumber, messageID string, err error) {
	if deviceNumber == "" || messageID == "" {
		a.logger.Debug("Skip attribute response: empty deviceNumber or messageID")
		return
	}

	// 构造响应 Topic
	topic := BuildAttributeResponseTopic(deviceNumber, messageID)

	// 构造响应 Payload
	payload := common.GetResponsePayload("", err)

	// 发布消息
	token := a.mqttClient.Publish(topic, 1, false, payload)
	if publishErr := a.waitResponseToken(token); publishErr != nil {
		a.logger.WithFields(logrus.Fields{
			"device_number": deviceNumber,
			"message_id":    messageID,
			"topic":         topic,
			"error":         publishErr,
		}).Error("Failed to publish attribute response")
	} else {
		a.logger.WithFields(logrus.Fields{
			"device_number": deviceNumber,
			"message_id":    messageID,
			"topic":         topic,
		}).Debug("Attribute response sent successfully")
	}
}

// publishEventResponse 发送事件上报 ACK 响应
// 协议层行为：告诉设备"我收到了你的事件上报"
func (a *Adapter) publishEventResponse(deviceNumber, messageID, method string, err error) {
	if deviceNumber == "" || messageID == "" {
		a.logger.Debug("Skip event response: empty deviceNumber or messageID")
		return
	}

	// 构造响应 Topic
	topic := BuildEventResponseTopic(deviceNumber, messageID)

	// 构造响应 Payload（包含 method）
	payload := common.GetResponsePayload(method, err)

	// 发布消息
	token := a.mqttClient.Publish(topic, 1, false, payload)
	if publishErr := a.waitResponseToken(token); publishErr != nil {
		a.logger.WithFields(logrus.Fields{
			"device_number": deviceNumber,
			"message_id":    messageID,
			"method":        method,
			"topic":         topic,
			"error":         publishErr,
		}).Error("Failed to publish event response")
	} else {
		a.logger.WithFields(logrus.Fields{
			"device_number": deviceNumber,
			"message_id":    messageID,
			"method":        method,
			"topic":         topic,
		}).Debug("Event response sent successfully")
	}
}

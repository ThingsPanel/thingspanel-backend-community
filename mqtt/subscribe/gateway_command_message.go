package subscribe

import (
	"encoding/json"
	"project/internal/model"
	config "project/mqtt"
	"strings"

	"github.com/sirupsen/logrus"
)

// 平台订阅网关属性上报处理
// @description GatewayAttributeMessages
// param payload []byte
// param topic string
// @return error
// 订阅topic gateway/command/response/{message_id}
func GatewayDeviceCommandResponse(payload []byte, topic string) {
	var messageId string
	topicList := strings.Split(topic, "/")
	if len(topicList) >= 4 {
		messageId = topicList[3]
	}
	if messageId == "" {
		return
	}
	logrus.Debug("payload:", string(payload))
	// 验证消息有效性
	commandResponsePayload, err := verifyPayload(payload)
	if err != nil {
		return
	}
	logrus.Debug("payload:", string(commandResponsePayload.Values))
	result := model.MqttResponse{}
	if err := json.Unmarshal(commandResponsePayload.Values, &result); err != nil {
		return
	}
	if ch, ok := config.MqttResponseFuncMap[messageId]; ok {
		ch <- result
	}
}

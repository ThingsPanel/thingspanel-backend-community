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
// @param payload []byte
// @param topic string
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
	attributePayload, err := verifyPayload(payload)
	if err != nil {
		return
	}
	logrus.Debug("payload:", string(attributePayload.Values))
	result := model.GatewayResponse{}
	if err := json.Unmarshal(attributePayload.Values, &result); err != nil {
		return
	}
	if ch, ok := config.GatewayResponseFuncMap[messageId]; ok {
		ch <- result
	}
}

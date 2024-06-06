package subscribe

import (
	config "project/mqtt"
	"strings"

	"github.com/sirupsen/logrus"
)

// 接收设备命令的响应消息
func DeviceCommand(payload []byte, topic string) (string, error) {
	/*
		消息规范：topic:devices/command/response/+
				 +是device_id
				 payload是json格式的命令消息
	*/
	// 验证消息有效性
	// TODO处理消息
	logrus.Debug("command message:", string(payload))
	var messageId string
	topicList := strings.Split(topic, "/")
	if len(topicList) < 4 {
		messageId = ""
	} else {
		messageId = topicList[3]
	}
	// 验证消息有效性
	attributePayload, err := verifyPayload(payload)
	if err != nil {
		return "", err
	}
	logrus.Debug("command values message:", string(attributePayload.Values))
	// 验证消息有效性
	commandResponsePayload, err := verifyCommandResponsePayload(attributePayload.Values)
	if err != nil {
		logrus.Error(err.Error())
		return "", err
	}
	logrus.Debug("command response message:", commandResponsePayload)

	//log := dal.CommandSetLogsQuery{}
	//// 通过消息id检查命令历史一小时内是否存在消息
	//if m, err := log.FilterOneHourByMessageID(messageId); err != nil || m == nil {
	//	logrus.Error(err.Error())
	//	return "", err
	//}
	//// 存在消息id,处理消息,入库
	//logInfo := &model.CommandSetLog{
	//	MessageID: &messageId,
	//}
	//if commandResponsePayload.Result == 0 {
	//	execFail := "3"
	//	logInfo.Status = &execFail
	//} else {
	//	execSuccess := "4"
	//	logInfo.Status = &execSuccess
	//	logInfo.RspDatum = &commandResponsePayload.Errcode
	//	logInfo.ErrorMessage = &commandResponsePayload.Message
	//}
	//err = log.Update(nil, logInfo)
	if ch, ok := config.MqttDirectResponseFuncMap[messageId]; ok {
		ch <- *commandResponsePayload
	}
	return messageId, err
}

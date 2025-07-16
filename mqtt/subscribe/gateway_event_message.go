package subscribe

import (
	"encoding/json"
	dal "project/internal/dal"
	"project/internal/model"
	"strings"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// 平台订阅网关事件处理
// @description GatewayEventCallback
// param payload []byte
// param topic string
// @return messageId string, gatewayDeive *model.Device, respon model.GatewayResponse, err error
// 订阅topic gateway/event/{message_id}
func GatewayEventCallback(payload []byte, topic string) (string, *model.Device, model.MqttResponse, error) {
	var messageId string
	var response model.MqttResponse
	topicList := strings.Split(topic, "/")
	if len(topicList) >= 3 {
		messageId = topicList[2]
	}

	logrus.Debug("payload:", string(payload))
	// 验证消息有效性
	attributePayload, err := verifyPayload(payload)
	if err != nil {
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayEventCallback][verifyPayload]fail")
	}
	payloads := &model.GatewayCommandPulish{}
	if err := json.Unmarshal(attributePayload.Values, payloads); err != nil {
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayEventCallback][verifyPayload2]fail")
	}
	deviceInfo, err := dal.GetDeviceCacheById(attributePayload.DeviceId)
	if err != nil {
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayEventCallback][GetDeviceCacheById]fail")
	}

	if payloads.GatewayData != nil {
		logrus.Debug("attribute message:", payloads.GatewayData)
		// 验证values消息有效性
		// eventValues, err := verifyEventPayload(payloads.GatewayData)
		// if err != nil {
		// 	return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayEventCallback][verifyEventPayload]fail")
		// }
		err = deviceEventHandle(deviceInfo, payloads.GatewayData, topic)
		response = *getWagewayResponse(err)
	}
	if payloads.SubDeviceData != nil {
		var subDeviceAddrs []string
		for deviceAddr := range *payloads.SubDeviceData {
			subDeviceAddrs = append(subDeviceAddrs, deviceAddr)
		}
		subDeviceInfos, _ := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, deviceInfo.ID)
		for subDeviceAddr, data := range *payloads.SubDeviceData {
			if subInfo, ok := subDeviceInfos[subDeviceAddr]; ok {
				err = deviceEventHandle(subInfo, &data, topic)

			}
		}
		response = *getWagewayResponse(err)
	}
	return messageId, deviceInfo, response, nil
}

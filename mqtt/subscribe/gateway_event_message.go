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

	// 处理子网关数据（递归处理多级网关）
	if payloads.SubGatewayData != nil {
		err = processSubGatewayEventData(*payloads.SubGatewayData, deviceInfo.ID, topic, 1)
		response = *getWagewayResponse(err)
	}

	return messageId, deviceInfo, response, nil
}

// processSubGatewayEventData 递归处理子网关事件数据
func processSubGatewayEventData(subGatewayData map[string]*model.GatewayCommandPulish, parentGatewayID string, topic string, depth int) error {
	// 限制最大递归深度为5层
	if depth > 5 {
		logrus.Warn("[processSubGatewayEventData] Maximum depth (5) exceeded, skipping deeper levels")
		return nil
	}

	// 获取当前层级的子网关设备地址
	var subGatewayAddrs []string
	for gatewayAddr := range subGatewayData {
		subGatewayAddrs = append(subGatewayAddrs, gatewayAddr)
	}

	// 查找子网关设备信息
	subGatewayInfos, err := dal.GetDeviceBySubDeviceAddress(subGatewayAddrs, parentGatewayID)
	if err != nil {
		return pkgerrors.Wrap(err, "[processSubGatewayEventData][GetDeviceBySubDeviceAddress]fail")
	}

	// 处理每个子网关的数据
	for gatewayAddr, gatewayData := range subGatewayData {
		subGatewayInfo, ok := subGatewayInfos[gatewayAddr]
		if !ok {
			logrus.Warnf("[processSubGatewayEventData] Sub gateway not found: %s", gatewayAddr)
			continue
		}

		// 处理子网关自身事件数据
		if gatewayData.GatewayData != nil {
			err = deviceEventHandle(subGatewayInfo, gatewayData.GatewayData, topic)
			if err != nil {
				logrus.Error(pkgerrors.Wrap(err, "[processSubGatewayEventData][deviceEventHandle]fail"))
			}
		}

		// 处理子网关下的子设备事件数据
		if gatewayData.SubDeviceData != nil {
			var subDeviceAddrs []string
			for deviceAddr := range *gatewayData.SubDeviceData {
				subDeviceAddrs = append(subDeviceAddrs, deviceAddr)
			}
			subDeviceInfos, _ := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, subGatewayInfo.ID)
			for subDeviceAddr, data := range *gatewayData.SubDeviceData {
				if subInfo, ok := subDeviceInfos[subDeviceAddr]; ok {
					err = deviceEventHandle(subInfo, &data, topic)
					if err != nil {
						logrus.Error(pkgerrors.Wrap(err, "[processSubGatewayEventData][deviceEventHandle SubDevice]fail"))
					}
				}
			}
		}

		// 递归处理更深层的子网关数据
		if gatewayData.SubGatewayData != nil {
			err = processSubGatewayEventData(*gatewayData.SubGatewayData, subGatewayInfo.ID, topic, depth+1)
			if err != nil {
				logrus.Error(pkgerrors.Wrap(err, "[processSubGatewayEventData][recursive]fail"))
			}
		}
	}

	return nil
}

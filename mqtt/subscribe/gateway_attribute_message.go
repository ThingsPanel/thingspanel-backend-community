package subscribe

import (
	"encoding/json"
	"project/internal/dal"
	"project/internal/model"
	config "project/mqtt"
	"strings"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// 平台订阅网关属性上报处理
// @description GatewayAttributeMessages
// param payload []byte
// param topic string
// @return messageId string, gatewayDeive *model.Device, respon model.GatewayResponse, err error
// 订阅topic gateway/attributes/{message_id}
func GatewayAttributeMessages(payload []byte, topic string) (string, *model.Device, model.MqttResponse, error) {
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
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayAttributeMessages][verifyPayload]fail")
	}
	logrus.Debug("attribute message:", attributePayload)
	payloads := &model.GatewayPublish{}
	if err := json.Unmarshal(attributePayload.Values, payloads); err != nil {
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayAttributeMessages][verifyPayload2]fail")
	}
	deviceInfo, err := dal.GetDeviceCacheById(attributePayload.DeviceId)
	if err != nil {
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayAttributeMessages][GetDeviceCacheById]fail")
	}
	if payloads.GatewayData != nil {
		err = deviceAttributesHandle(deviceInfo, *payloads.GatewayData, topic)
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
				err = deviceAttributesHandle(subInfo, data, topic)
			}
		}
		response = *getWagewayResponse(err)
	}

	// 处理子网关数据（递归处理多级网关）
	if payloads.SubGatewayData != nil {
		err = processSubGatewayAttributeData(*payloads.SubGatewayData, deviceInfo.ID, topic, 1)
		response = *getWagewayResponse(err)
	}

	return messageId, deviceInfo, response, nil
}

func getWagewayResponse(err error, _ ...string) *model.MqttResponse {
	var mqttResponse *model.MqttResponse
	now := time.Now().Unix()
	if err == nil {
		mqttResponse = &model.MqttResponse{
			Result:  model.MQTT_RESPONSE_RESULT_FAIL,
			Message: "success",
			Ts:      now,
		}
	} else {
		logrus.Error("属性或事件处理失败:", err)
		var errmsg = err.Error()
		mqttResponse = &model.MqttResponse{
			Result:  model.MQTT_RESPONSE_RESULT_FAIL,
			Message: errmsg,
			Ts:      now,
		}
	}
	return mqttResponse
}

// GatewayDeviceSetAttributesResponse
//
// @description 平台设置属性
// param payload []byte
// param topic string
// @return messageId string, gatewayDeive *model.Device, respon model.GatewayResponse, err error
// 订阅topic gateway/attributes/{message_id}
func GatewayDeviceSetAttributesResponse(payload []byte, topic string) {
	//devices/attributes/set/response/+
	var messageId string
	topicList := strings.Split(topic, "/")
	if len(topicList) >= 5 {
		messageId = topicList[4]
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
	result := model.MqttResponse{}
	if err := json.Unmarshal(attributePayload.Values, &result); err != nil {
		return
	}

	if ch, ok := config.MqttResponseFuncMap[messageId]; ok {
		logrus.Debug("payload: ok:", result)
		ch <- result
	}
}

// processSubGatewayAttributeData 递归处理子网关属性数据
func processSubGatewayAttributeData(subGatewayData map[string]*model.GatewayPublish, parentGatewayID string, topic string, depth int) error {
	// 限制最大递归深度为5层
	if depth > 5 {
		logrus.Warn("[processSubGatewayAttributeData] Maximum depth (5) exceeded, skipping deeper levels")
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
		return pkgerrors.Wrap(err, "[processSubGatewayAttributeData][GetDeviceBySubDeviceAddress]fail")
	}

	// 处理每个子网关的数据
	for gatewayAddr, gatewayData := range subGatewayData {
		subGatewayInfo, ok := subGatewayInfos[gatewayAddr]
		if !ok {
			logrus.Warnf("[processSubGatewayAttributeData] Sub gateway not found: %s", gatewayAddr)
			continue
		}

		// 处理子网关自身属性数据
		if gatewayData.GatewayData != nil {
			err = deviceAttributesHandle(subGatewayInfo, *gatewayData.GatewayData, topic)
			if err != nil {
				logrus.Error(pkgerrors.Wrap(err, "[processSubGatewayAttributeData][deviceAttributesHandle]fail"))
			}
		}

		// 处理子网关下的子设备属性数据
		if gatewayData.SubDeviceData != nil {
			var subDeviceAddrs []string
			for deviceAddr := range *gatewayData.SubDeviceData {
				subDeviceAddrs = append(subDeviceAddrs, deviceAddr)
			}
			subDeviceInfos, _ := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, subGatewayInfo.ID)
			for subDeviceAddr, data := range *gatewayData.SubDeviceData {
				if subInfo, ok := subDeviceInfos[subDeviceAddr]; ok {
					err = deviceAttributesHandle(subInfo, data, topic)
					if err != nil {
						logrus.Error(pkgerrors.Wrap(err, "[processSubGatewayAttributeData][deviceAttributesHandle SubDevice]fail"))
					}
				}
			}
		}

		// 递归处理更深层的子网关数据
		if gatewayData.SubGatewayData != nil {
			err = processSubGatewayAttributeData(*gatewayData.SubGatewayData, subGatewayInfo.ID, topic, depth+1)
			if err != nil {
				logrus.Error(pkgerrors.Wrap(err, "[processSubGatewayAttributeData][recursive]fail"))
			}
		}
	}

	return nil
}

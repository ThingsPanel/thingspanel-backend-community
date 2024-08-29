package subscribe

import (
	"encoding/json"
	"project/dal"
	"project/internal/model"
	config "project/mqtt"
	"strings"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// 平台订阅网关属性上报处理
// @description GatewayAttributeMessages
// @param payload []byte
// @param topic string
// @return messageId string, gatewayDeive *model.Device, respon model.GatewayResponse, err error
// 订阅topic gateway/attributes/{message_id}
func GatewayAttributeMessages(payload []byte, topic string) (string, *model.Device, model.GatewayResponse, error) {
	var messageId string
	var response model.GatewayResponse
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
	deviceInfo, err := dal.GetDeviceById(attributePayload.DeviceId)
	if err != nil {
		return messageId, nil, response, pkgerrors.Wrap(err, "[GatewayAttributeMessages][GetDeviceById]fail")
	}
	if payloads.GatewayData != nil {
		err = deviceAttributesHandle(deviceInfo, *payloads.GatewayData, topic)
		response.GatewayData = getWagewayResponse(err)
	}
	if payloads.SubDeviceData != nil {
		subDeviceData := make(map[string]model.MqttResponse)
		var subDeviceAddrs []string
		for deviceAddr := range *payloads.SubDeviceData {
			subDeviceAddrs = append(subDeviceAddrs, deviceAddr)
		}
		subDeviceInfos, _ := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, deviceInfo.ID)
		for subDeviceAddr, data := range *payloads.SubDeviceData {
			if subInfo, ok := subDeviceInfos[subDeviceAddr]; ok {
				err = deviceAttributesHandle(subInfo, data, topic)
			}
			subDeviceData[subDeviceAddr] = *getWagewayResponse(err)
		}
		response.SubDeviceData = subDeviceData
	}
	return messageId, deviceInfo, response, nil
}

func getWagewayResponse(err error, extends ...string) *model.MqttResponse {
	var mqttResponse *model.MqttResponse
	now := time.Now().Unix()
	if err == nil {
		mqttResponse = &model.MqttResponse{
			Result:  model.MQTT_RESPONSE_RESULT_FAIL,
			Message: "success",
			Ts:      now,
		}
	} else {
		logrus.Errorf("属性或事件处理失败:%w", err)
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
// @param payload []byte
// @param topic string
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
	result := model.GatewayResponse{}
	if err := json.Unmarshal(attributePayload.Values, &result); err != nil {
		return
	}

	if ch, ok := config.GatewayResponseFuncMap[messageId]; ok {
		logrus.Debug("payload: ok:", result)
		ch <- result
	}
}

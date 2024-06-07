package subscribe

import (
	"encoding/json"
	dal "project/dal"
	"project/model"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func GatewayTelemetryMessages(payload []byte, topic string) {

	// 验证消息有效性
	attributePayload, err := verifyPayload(payload)
	if err != nil {
		logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][verifyPayload]fail"))
		return
	}
	logrus.Debug("attribute message:", attributePayload)
	payloads := &model.GatewayPublish{}
	if err := json.Unmarshal(attributePayload.Values, payloads); err != nil {
		logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][Telemetry]fail"))
		return
	}
	deviceInfo, err := dal.GetDeviceById(attributePayload.DeviceId)
	if err != nil {
		logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][GetDeviceById]fail"))
		return
	}
	if payloads.GatewayData != nil {
		gatewayBoy, err := json.Marshal(payloads.GatewayData)
		if err != nil {
			logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][GetDeviceById]fail"))
		} else {
			TelemetryMessagesHandle(deviceInfo, gatewayBoy, topic)
		}
	}
	if payloads.SubDeviceData == nil {
		return
	}
	var subDeviceAddrs []string
	for deviceAddr := range *payloads.SubDeviceData {
		subDeviceAddrs = append(subDeviceAddrs, deviceAddr)
	}
	subDeviceInfos, _ := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, deviceInfo.ID)
	for subDeviceAddr, data := range *payloads.SubDeviceData {
		if subInfo, ok := subDeviceInfos[subDeviceAddr]; ok {
			subDeviceBoy, err := json.Marshal(data)
			if err != nil {
				logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][GetDeviceById]fail"))
			}
			TelemetryMessagesHandle(subInfo, subDeviceBoy, topic)
		}

	}
}

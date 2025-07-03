package subscribe

import (
	"encoding/json"
	dal "project/internal/dal"
	"project/internal/model"
	service "project/internal/service"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func GatewayTelemetryMessages(payload []byte, topic string) {
	// 验证消息有效性
	telemetryPayload, err := verifyPayload(payload)
	if err != nil {
		logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][verifyPayload]fail"))
		return
	}

	// 获取网关设备信息
	deviceInfo, err := dal.GetDeviceCacheById(telemetryPayload.DeviceId)
	if err != nil {
		logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][GetDeviceCacheById]fail"))
		return
	}

	// 脚本处理 - 对原始Values进行预处理
	processedValues := telemetryPayload.Values
	if deviceInfo.DeviceConfigID != nil && *deviceInfo.DeviceConfigID != "" {
		newValues, err := service.GroupApp.DataScript.Exec(deviceInfo, "A", telemetryPayload.Values, topic)
		if err != nil {
			logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][DataScript.Exec]fail"))
			return
		}
		if newValues != nil {
			processedValues = newValues
		}
	}

	logrus.Debug("gateway telemetry message after script:", string(processedValues))

	// 解析处理后的数据
	payloads := &model.GatewayPublish{}
	if err := json.Unmarshal(processedValues, payloads); err != nil {
		logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][Unmarshal]fail"))
		return
	}

	// 处理网关自身数据（已经过脚本处理，直接进入业务逻辑）
	if payloads.GatewayData != nil {
		gatewayBody, err := json.Marshal(payloads.GatewayData)
		if err != nil {
			logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][Marshal GatewayData]fail"))
		} else {
			// 跳过脚本处理，直接调用业务处理逻辑
			telemetryMessagesHandleCore(deviceInfo, gatewayBody, topic)
		}
	}

	// 处理子设备数据（每个子设备单独进行脚本处理）
	if payloads.SubDeviceData != nil {
		var subDeviceAddrs []string
		for deviceAddr := range *payloads.SubDeviceData {
			subDeviceAddrs = append(subDeviceAddrs, deviceAddr)
		}
		subDeviceInfos, _ := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, deviceInfo.ID)
		for subDeviceAddr, data := range *payloads.SubDeviceData {
			if subInfo, ok := subDeviceInfos[subDeviceAddr]; ok {
				subDeviceBody, err := json.Marshal(data)
				if err != nil {
					logrus.Error(pkgerrors.Wrap(err, "[GatewayTelemetryMessages][Marshal SubDeviceData]fail"))
					continue
				}
				// 子设备需要单独进行脚本处理
				TelemetryMessagesHandle(subInfo, subDeviceBody, topic)
			}
		}
	}
}

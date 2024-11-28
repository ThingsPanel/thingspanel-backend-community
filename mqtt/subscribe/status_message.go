package subscribe

import (
	"context"
	"encoding/json"
	"fmt"
	initialize "project/initialize"
	dal "project/internal/dal"
	"project/internal/model"
	service "project/internal/service"
	"project/pkg/global"
	"strings"

	"github.com/sirupsen/logrus"
)

func validateStatus(payload []byte) (int16, error) {
	str := string(payload)
	switch str {
	case "0":
		return 0, nil
	case "1":
		return 1, nil
	default:
		return 0, fmt.Errorf("状态值只能是0或1，当前值: %s", str)
	}
}

func DeviceOnline(payload []byte, topic string) {
	/*
		消息规范：topic:devices/status/+
				 +是device_id
				 payload（1-在线 0-离线）如:1
				在线离线状态是devices表的is_online字段
	*/
	// 验证消息有效性
	status, err := validateStatus(payload)
	if err != nil {
		logrus.Error(err.Error())
		return
	}

	deviceId := strings.Split(topic, "/")[2]
	logrus.Debug(deviceId, " device status message:", status)
	err = dal.UpdateDeviceStatus(deviceId, status)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	if status == int16(1) {
		// 发送预期数据
		err := service.GroupApp.ExpectedData.Send(context.Background(), deviceId)
		if err != nil {
			logrus.Error(err.Error())
		}

	}
	// 清理缓存
	initialize.DelDeviceCache(deviceId)

	var device *model.Device
	device, err = dal.GetDeviceCacheById(deviceId)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	// 上下线通知客户端程序
	go toUserClient(device, status)
	//自动化
	go func() {
		var loginStatus string
		if status == 1 {
			loginStatus = "ON-LINE"
		} else {
			loginStatus = "OFF-LINE"
		}
		err := service.GroupApp.Execute(device, service.AutomateFromExt{
			TriggerParamType: model.TRIGGER_PARAM_TYPE_STATUS,
			TriggerParam:     []string{},
			TriggerValues: map[string]interface{}{
				"login": loginStatus,
			},
		})
		if err != nil {
			logrus.Error("自动化执行失败, err: %w", err)
		}
	}()

	err = initialize.SetRedisForJsondata(deviceId, device, 0)
	if err != nil {
		logrus.Error(err.Error())
		return
	}

}

// 设备上线通知
func toUserClient(device *model.Device, status int16) {
	// 发送事件
	var deviceName string
	sseEvent := global.SSEEvent{
		Type:     "device_online",
		TenantID: device.TenantID,
	}

	if device.Name != nil {
		deviceName = *device.Name
	} else {
		deviceName = device.DeviceNumber
	}
	if status == int16(1) {
		jsonBytes, _ := json.Marshal(map[string]interface{}{
			"device_id":   device.DeviceNumber,
			"device_name": deviceName,
			"is_online":   true,
		})
		sseEvent.Message = string(jsonBytes)
	} else {
		jsonBytes, _ := json.Marshal(map[string]interface{}{
			"device_id":   device.DeviceNumber,
			"device_name": deviceName,
			"is_online":   false,
		})
		sseEvent.Message = string(jsonBytes)
	}
	global.TPSSEManager.BroadcastEventToTenant(device.TenantID, sseEvent)
}

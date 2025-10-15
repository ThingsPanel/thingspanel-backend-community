package subscribe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/global"
)

// SubscribeDeviceStatus 订阅设备状态消息
func SubscribeDeviceStatus() error {
	topic := GenTopic("devices/status/+")
	logrus.Info("订阅设备状态主题: ", topic)

	if token := SubscribeMqttClient.Subscribe(topic, 0, DeviceStatusCallback); token.Wait() && token.Error() != nil {
		logrus.Error("订阅设备状态主题失败: ", token.Error())
		return token.Error()
	}

	logrus.Info("✅ 设备状态主题订阅成功")
	return nil
}

// DeviceStatusCallback 设备状态消息回调
// topic: devices/status/+
// payload: 1-在线 0-离线
func DeviceStatusCallback(_ mqtt.Client, d mqtt.Message) {
	logrus.WithFields(logrus.Fields{
		"topic":   d.Topic(),
		"payload": string(d.Payload()),
	}).Info("📩 Received device status message")

	// 使用 Flow 层处理
	if mqttAdapter != nil {
		logrus.Info("✅ Using Flow layer to process status message")
		// source = "status_message" 表示来自设备主动上报
		if err := mqttAdapter.HandleStatusMessage(d.Payload(), d.Topic(), "status_message"); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"topic":   d.Topic(),
				"payload": string(d.Payload()),
			}).Error("❌ Flow layer status processing failed")
		} else {
			logrus.Info("✅ Flow layer status processing succeeded")
		}
		return
	}

	// 如果 Adapter 未初始化,记录错误并使用旧逻辑降级
	logrus.Warn("⚠️ MQTT Adapter not initialized, using legacy status processing")
	DeviceOnline(d.Payload(), d.Topic())
}

// DeviceOnline 旧的状态处理逻辑(保留作为降级备用)
// DEPRECATED: 使用 Flow 层的 StatusFlow 替代
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
		time.Sleep(3 * time.Second)
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

// toUserClient 设备上线通知
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

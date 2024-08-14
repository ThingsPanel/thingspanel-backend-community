package subscribe

import (
	"context"
	dal "project/dal"
	initialize "project/initialize"
	"project/model"
	service "project/service"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

func DeviceOnline(payload []byte, topic string) {
	/*
		消息规范：topic:devices/status/+
				 +是device_id
				 payload（1-在线 0-离线）如:1
				在线离线状态是devices表的is_online字段
	*/
	// 验证消息有效性
	payloadInt, err := strconv.Atoi(string(payload))
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	status := int16(payloadInt)
	deviceId := strings.Split(topic, "/")[2]
	logrus.Debug(deviceId, " device status message:", status)
	err = dal.UpdateDeviceStatus(deviceId, status)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	if status == 1 {
		// 发送预期数据
		err := service.GroupApp.ExpectedData.Send(context.Background(), deviceId)
		if err != nil {
			logrus.Error(err.Error())
		}
	}
	// 清理缓存
	initialize.DelDeviceCache(deviceId)

	var device *model.Device
	device, err = dal.GetDeviceById(deviceId)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	//自动化
	go func() {
		err := service.GroupApp.Execute(device, service.AutomateFromExt{
			TriggerParamType: model.TRIGGER_PARAM_TYPE_STATUS,
			TriggerParam:     []string{},
		})
		if err != nil {
			logrus.Errorf("自动化执行失败, err: %w", err)
		}
	}()

	err = initialize.SetRedisForJsondata(deviceId, device, 0)
	if err != nil {
		logrus.Error(err.Error())
		return
	}

}

// // 设备在线离线的有效负载。
// type statusPayload struct {
// 	DeviceId string `json:"device_id"`
// 	Status   int16  `json:"status"`
// }

// // verifyPayload 函数验证设备上报属性消息的有效负载。
// func verifyDeviceStatusPayload(body []byte) (*statusPayload, error) {
// 	payload := &statusPayload{}
// 	if err := json.Unmarshal(body, payload); err != nil {
// 		logrus.Error("解析消息失败:", err)
// 		return payload, err
// 	}
// 	if len(payload.DeviceId) == 0 {
// 		return payload, errors.New("DeviceId不能为空:" + payload.DeviceId)
// 	}
// 	if payload.Status != 0 && payload.Status != 1 {
// 		return payload, errors.New("Status只能为0或1:" + fmt.Sprint(payload.Status))
// 	}
// 	return payload, nil
// }

package subscribe

import (
	"encoding/json"
	"fmt"
	dal "project/dal"
	initialize "project/initialize"
	"project/model"
	config "project/mqtt"
	service "project/service"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// 设备属性上报消息处理
func DeviceAttributeReport(payload []byte, topic string) (string, string, error) {
	/*
		消息规范：topic:devices/attributes/+
				 +是message_id
				 payload是设备属性的json字符串
	*/

	// 这里认为topic的第三个部分是device_number，首先判断topic是否有第三个部分
	var messageId string
	topicList := strings.Split(topic, "/")
	if len(topicList) < 3 {
		messageId = ""
	} else {
		messageId = topicList[2]
	}

	logrus.Debug("payload:", string(payload))
	// 验证消息有效性
	attributePayload, err := verifyPayload(payload)
	if err != nil {
		logrus.Error(err.Error())
		return "", "", err
	}
	logrus.Debug("attribute message:", attributePayload)

	// 处理消息
	device, err := initialize.GetDeviceById(attributePayload.DeviceId)
	if err != nil {
		logrus.Error(err.Error())
		return "", messageId, err
	}

	//byte转map
	var reqMap = make(map[string]interface{})
	err = json.Unmarshal(attributePayload.Values, &reqMap)
	if err != nil {
		logrus.Error(err.Error())
		return device.DeviceNumber, messageId, err
	}
	err = deviceAttributesHandle(device, reqMap)
	if err != nil {
		logrus.Error(err.Error())
		return device.DeviceNumber, messageId, err
	}
	return device.DeviceNumber, messageId, err
	// TODO响应消息
}

// 设备属性处理 和网关公用
// @description deviceAttributesHandle
// @param device *model.Device
// @param reqMap map[string]interface{
// @return err error
func deviceAttributesHandle(device *model.Device, reqMap map[string]interface{}) error {
	// TODO脚本处理

	//自动化处理
	go func() {
		err := service.GroupApp.Execute(device)
		if err != nil {
			logrus.Errorf("自动化执行失败, err: %w", err)
		}
	}()
	//属性保存
	ts := time.Now().UTC()
	logrus.Debug(device, ts)
	for k, v := range reqMap {
		logrus.Debug(k, "(", v, ")")
		var d model.AttributeData
		switch value := v.(type) {
		case string:
			d = model.AttributeData{
				DeviceID: device.ID,
				Key:      k,
				T:        ts,
				StringV:  &value,
				TenantID: &device.TenantID,
			}
		case bool:
			d = model.AttributeData{
				DeviceID: device.ID,
				Key:      k,
				T:        ts,
				BoolV:    &value,
				TenantID: &device.TenantID,
			}
		case float64:
			d = model.AttributeData{
				DeviceID: device.ID,
				Key:      k,
				T:        ts,
				NumberV:  &value,
				TenantID: &device.TenantID,
			}
		default:
			s := fmt.Sprint(value)
			d = model.AttributeData{
				DeviceID: device.ID,
				Key:      k,
				T:        ts,
				StringV:  &s,
				TenantID: &device.TenantID,
			}
		}
		logrus.Debug("attribute data:", d)
		_, err := dal.UpdateAttributeData(&d)
		if err != nil {
			logrus.Error(err.Error())
			return err
		}
	}
	return nil
}

func DeviceSetAttributeResponse(payload []byte, topic string) {
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
		return
	}
	logrus.Debug("command values message:", string(attributePayload.Values))
	// 验证消息有效性
	commandResponsePayload, err := verifyCommandResponsePayload(attributePayload.Values)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	logrus.Debug("command response message:", commandResponsePayload)

	if ch, ok := config.MqttDirectResponseFuncMap[messageId]; ok {
		ch <- *commandResponsePayload
	}

}

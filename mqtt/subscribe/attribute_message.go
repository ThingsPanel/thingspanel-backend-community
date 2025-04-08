package subscribe

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	initialize "project/initialize"
	dal "project/internal/dal"
	"project/internal/model"
	service "project/internal/service"
	config "project/mqtt"

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
	device, err := initialize.GetDeviceCacheById(attributePayload.DeviceId)
	if err != nil {
		logrus.Error(err.Error())
		return "", messageId, err
	}

	// byte转map
	reqMap := make(map[string]interface{})
	err = json.Unmarshal(attributePayload.Values, &reqMap)
	if err != nil {
		logrus.Error(err.Error())
		return device.DeviceNumber, messageId, err
	}
	err = deviceAttributesHandle(device, reqMap, topic)
	if err != nil {
		logrus.Error(err.Error())
		return device.DeviceNumber, messageId, err
	}
	return device.DeviceNumber, messageId, err
	// TODO响应消息
}

// 设备属性处理 和网关公用
// @description deviceAttributesHandle
// param device *model.Device
// param reqMap map[string]interface{
// @return err error
func deviceAttributesHandle(device *model.Device, reqMap map[string]interface{}, topic string) error {
	// TODO脚本处理
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		scriptType := "C"
		attributesBody, _ := json.Marshal(reqMap)
		newAttributesBody, err := service.GroupApp.DataScript.Exec(device, scriptType, attributesBody, topic)
		if err != nil {
			logrus.Error("Error in attribute script processing: ", err.Error())
		}
		if newAttributesBody != nil {
			err = json.Unmarshal(newAttributesBody, &reqMap)
			if err != nil {
				logrus.Error("Error in attribute script processing: ", err.Error())
			}
		}
	}

	// 属性保存
	ts := time.Now().UTC()
	logrus.Debug(device, ts)
	var (
		triggerParam  []string
		triggerValues = make(map[string]interface{})
	)
	for k, v := range reqMap {
		logrus.Debug(k, "(", v, ")")

		d := model.AttributeData{
			DeviceID: device.ID,
			Key:      k,
			T:        ts,
			TenantID: &device.TenantID,
		}

		// 根据类型设置值字段
		switch value := v.(type) {
		case string:
			d.StringV = &value
		case bool:
			d.BoolV = &value
		case float64:
			d.NumberV = &value
		case int:
			// 处理整数类型
			f := float64(value)
			d.NumberV = &f
		case int64:
			// 处理长整数类型
			f := float64(value)
			d.NumberV = &f
		case []interface{}, map[string]interface{}:
			// 处理 JSON 对象或数组
			if jsonBytes, err := json.Marshal(value); err == nil {
				s := string(jsonBytes)
				d.StringV = &s
			} else {
				s := fmt.Sprint(value)
				d.StringV = &s
			}
		default:
			// 尝试检测是否为 JSON 字符串
			if jsonStr, ok := tryParseAsJSON(value); ok {
				d.StringV = &jsonStr
			} else {
				s := fmt.Sprint(value)
				d.StringV = &s
			}
		}
		triggerParam = append(triggerParam, k)
		triggerValues[k] = v
		logrus.Debug("attribute data:", d)
		_, err := dal.UpdateAttributeData(&d)
		if err != nil {
			logrus.Error(err.Error())
			return err
		}
	}
	// 自动化处理
	go func() {
		err := service.GroupApp.Execute(device, service.AutomateFromExt{
			TriggerParam:     triggerParam,
			TriggerValues:    triggerValues,
			TriggerParamType: model.TRIGGER_PARAM_TYPE_ATTR,
		})
		if err != nil {
			logrus.Error("自动化执行失败, err: ", err)
		}
	}()
	return nil
}

func DeviceSetAttributeResponse(payload []byte, topic string) {
	logrus.Debug("command message:", string(payload))
	var messageId string
	topicList := strings.Split(topic, "/")
	if len(topicList) < 5 {
		messageId = ""
	} else {
		messageId = topicList[4]
	}
	// 验证消息有效性
	attributePayload, err := verifyPayload(payload)
	if err != nil {
		return
	}
	logrus.Debug("command values message:", string(attributePayload.Values))
	// 验证消息有效性
	commandResponsePayload, err := verifyAttributeResponsePayload(attributePayload.Values)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	logrus.Debug("command response message:", commandResponsePayload)

	if ch, ok := config.MqttDirectResponseFuncMap[messageId]; ok {
		ch <- *commandResponsePayload
	}
}

package subscribe

import (
	"encoding/json"
	"errors"
	"project/internal/model"

	"github.com/sirupsen/logrus"
)

// 设备上报属性消息的有效负载。
type publicPayload struct {
	DeviceId string `json:"device_id"`
	Values   []byte `json:"values"`
}

// verifyPayload 函数验证设备上报属性消息的有效负载。
func verifyPayload(body []byte) (*publicPayload, error) {
	payload := &publicPayload{
		Values: make([]byte, 0),
	}
	if err := json.Unmarshal(body, payload); err != nil {
		logrus.Error("解析消息失败:", err)
		return payload, err
	}
	if len(payload.DeviceId) == 0 {
		return payload, errors.New("DeviceId不能为空:" + payload.DeviceId)
	}
	if len(payload.Values) == 0 {
		return payload, errors.New("values消息内容不能为空")
	}
	return payload, nil
}

// 验证事件消息的有效负载。
// "values":{"method":"事件标识符","params":{"key1":"value1","key2":"value2"}}
func verifyEventPayload(values interface{}) (*model.EventInfo, error) {
	eventPayload := &model.EventInfo{}
	if err := json.Unmarshal(values.([]byte), eventPayload); err != nil {
		logrus.Error("解析消息失败:", err)
		return eventPayload, err
	}
	if len(eventPayload.Method) == 0 {
		return eventPayload, errors.New("method不能为空:" + eventPayload.Method)
	}
	if len(eventPayload.Params) == 0 {
		return eventPayload, errors.New("params消息内容不能为空")
	}
	return eventPayload, nil
}

// 验证命令响应消息的有效负载。
// "values":{"result":0,"message":"命令执行结果","method":"事件标识符"}
//
//	"values":{"result":1,"errcode":"xxx","message":"xxxxxx","ts":1609143039,"method":"xxxxx"}
//
// 注意：method 字段不是必须的，设备响应时可能不包含
func verifyCommandResponsePayload(values interface{}) (*model.MqttResponse, error) {
	payload := &model.MqttResponse{}
	if err := json.Unmarshal(values.([]byte), payload); err != nil {
		logrus.Error("解析消息失败:", err)
		return payload, err
	}
	// method 字段不是必须的，不进行校验
	// message 字段也不强制校验，因为有些设备可能只返回 result
	return payload, nil
}

func verifyAttributeResponsePayload(values interface{}) (*model.MqttResponse, error) {
	payload := &model.MqttResponse{}
	if err := json.Unmarshal(values.([]byte), payload); err != nil {
		logrus.Error("解析消息失败:", err)
		return payload, err
	}
	return payload, nil
}

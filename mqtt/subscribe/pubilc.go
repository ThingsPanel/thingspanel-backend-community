package subscribe

import (
	"encoding/json"
	"errors"

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

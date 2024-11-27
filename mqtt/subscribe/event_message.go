package subscribe

import (
	"encoding/json"
	initialize "project/initialize"
	dal "project/internal/dal"
	"project/internal/model"
	service "project/internal/service"
	"strings"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

// DeviceEvent 接收设备事件消息
/*
消息示例{"device_id":"xxxxx","values":{"method":"事件标识符","params":{"key1":"value1","key2":"value2"}}}
*/
func DeviceEvent(payload []byte, topic string) (string, string, string, error) {
	/*
		消息规范：topic:devices/event/+
				 +是message_id
				 payload是json格式的事件消息
	*/
	var messageId string
	topicList := strings.Split(topic, "/")
	if len(topicList) < 3 {
		messageId = ""
	} else {
		messageId = topicList[2]
	}
	// 验证消息有效性
	eventPayload, err := verifyPayload(payload)
	if err != nil {
		logrus.Error(err.Error())
		return "", "", "", err
	}

	device, err := initialize.GetDeviceById(eventPayload.DeviceId)
	if err != nil {
		logrus.Error(err.Error())
		return "", "", "", err
	}

	logrus.Debug("event message:", eventPayload)
	// 验证values消息有效性
	eventValues, err := verifyEventPayload(eventPayload.Values)
	if err != nil {
		logrus.Error(err.Error())
		return device.DeviceNumber, messageId, "", err
	}
	logrus.Debug("event message:", eventValues)
	// 处理消息
	err = deviceEventHandle(device, eventValues, topic)
	if err != nil {
		logrus.Error(err.Error())
		return device.DeviceNumber, messageId, "", err
	}
	return device.DeviceNumber, messageId, eventValues.Method, nil
	// TODO响应消息

}

func deviceEventHandle(device *model.Device, eventValues *model.EventInfo, topic string) error {
	// TODO脚本处理
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		eventValuesByte, err := json.Marshal(eventValues)
		if err != nil {
			logrus.Error("JSON marshaling failed:", err)
			return err
		}
		neweventValues, err := service.GroupApp.DataScript.Exec(device, "F", eventValuesByte, topic)
		if err != nil {
			logrus.Error("Error in event script processing: ", err.Error())
		}
		if neweventValues != nil {
			err = json.Unmarshal(neweventValues, &eventValues)
			if err != nil {
				logrus.Error("Error in attribute script processing: ", err.Error())
			}
		}
	}

	// 写入表event_datas,model/event_datas.gen.go
	//将eventValues.Params转换为json字符串
	paramsJsonBytes, err := json.Marshal(eventValues.Params)
	if err != nil {
		logrus.Fatalf("JSON marshaling failed: %s", err)
		return err
	}
	paramsJsonString := string(paramsJsonBytes)
	eventDatas := &model.EventData{
		ID:       uuid.New(),
		DeviceID: device.ID,
		Identify: eventValues.Method,
		T:        time.Now().UTC(),
		Datum:    &paramsJsonString,
		TenantID: &device.TenantID,
	}
	// TODO自动化处理
	go func() {

		err = service.GroupApp.Execute(device, service.AutomateFromExt{
			TriggerParamType: model.TRIGGER_PARAM_TYPE_EVT,
			TriggerParam:     []string{eventValues.Method},
			TriggerValues: map[string]interface{}{
				eventValues.Method: paramsJsonString,
			},
		})
		if err != nil {
			logrus.Error("自动化执行失败, err:", err)
		}
	}()
	err = dal.CreateEventData(eventDatas)
	if err != nil {
		logrus.Error(err.Error())
		return err
	}
	return err
}

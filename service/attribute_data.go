package service

import (
	"context"
	"encoding/json"
	"fmt"
	"project/common"
	"project/constant"
	dal "project/dal"
	"project/initialize"
	model "project/model"
	config "project/mqtt"
	"project/mqtt/publish"
	utils "project/utils"
	"strconv"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type AttributeData struct{}

func (t *AttributeData) GetAttributeDataList(device_id string) (interface{}, error) {
	data, err := dal.GetAttributeDataListWithDeviceName(device_id)
	if err != nil {
		return nil, err
	}

	var easyData []map[string]interface{}
	for _, v := range data {
		d := make(map[string]interface{})
		d["id"] = v["id"]
		d["device_id"] = device_id
		d["ts"] = v["ts"]
		d["key"] = v["key"]
		d["data_name"] = v["data_name"]
		d["unit"] = v["unit"]
		if v["string_v"] != nil {
			d["value"] = v["string_v"]
		}

		if v["bool_v"] != nil {
			d["value"] = v["bool_v"]
		}

		if v["number_v"] != nil {
			d["value"] = v["number_v"]
		}

		easyData = append(easyData, d)
	}

	return easyData, nil
}

func (t *AttributeData) DeleteAttributeData(id string) error {
	err := dal.DeleteAttributeData(id)
	return err
}

func (t *AttributeData) GetAttributeSetLogsDataListByPage(req model.GetAttributeSetLogsListByPageReq) (interface{}, error) {
	count, data, err := dal.GetAttributeSetLogsDataListByPage(req)
	if err != nil {
		return nil, err
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data

	return dataMap, nil
}

func (t *AttributeData) AttributePutMessage(ctx context.Context, userID string, param *model.AttributePutMessage, operationType string, fn ...config.MqttDirectResponseFunc) error {
	var (
		log = dal.AttributeSetLogsQuery{}

		errorMessage string
	)

	deviceInfo, err := initialize.GetDeviceById(param.DeviceID)
	if err != nil {
		logrus.Error(ctx, "[AttributePutMessage][GetDeviceById]failed:", err)
		return err
	}
	messageID := common.GetMessageID()
	topic := fmt.Sprintf("%s%s/%s", config.MqttConfig.Attributes.PublishTopic, deviceInfo.DeviceNumber, messageID)

	// 脚本预处理
	if deviceInfo.DeviceConfigID != nil && *deviceInfo.DeviceConfigID != "" {
		newValue, err := GroupApp.DataScript.Exec(deviceInfo, "D", []byte(param.Value), topic)
		if err != nil {
			logrus.Error(ctx, "[AttributePutMessage][ExecDataScript]failed:", err)
			return err
		}
		if newValue != nil {
			param.Value = string(newValue)
		}
	}

	err = publish.PublishAttributeMessage(topic, []byte(param.Value))
	if err != nil {
		logrus.Error(ctx, "下发失败", err)
		errorMessage = err.Error()
	}
	//operationType := strconv.Itoa(constant.Manual)
	description := "下发属性日志记录"
	logInfo := &model.AttributeSetLog{
		ID:            uuid.New(),
		DeviceID:      param.DeviceID,
		OperationType: &operationType,
		MessageID:     &messageID,
		Datum:         &(param.Value),
		RspDatum:      nil,
		Status:        nil,
		ErrorMessage:  &errorMessage,
		CreatedAt:     time.Now().UTC(),
		UserID:        &userID,
		Description:   &description,
	}
	if err != nil {
		logInfo.ErrorMessage = &errorMessage
		status := strconv.Itoa(constant.StatusFailed)
		logInfo.Status = &status
	} else {
		status := strconv.Itoa(constant.StatusOK)
		logInfo.Status = &status
	}
	_, err = log.Create(ctx, logInfo)
	config.MqttDirectResponseFuncMap[messageID] = make(chan model.MqttResponse)
	go func() {
		select {
		case response := <-config.MqttDirectResponseFuncMap[messageID]:
			fmt.Println("接收到数据:", response)
			if len(fn) > 0 {
				_ = fn[0](response)
			}
			log.SetAttributeResultUpdate(context.Background(), logInfo.ID, response)
			close(config.MqttDirectResponseFuncMap[messageID])
			delete(config.MqttDirectResponseFuncMap, messageID)
		case <-time.After(3 * time.Minute): // 设置超时时间为 3 分钟
			fmt.Println("超时，关闭通道")
			//log.SetAttributeResultUpdate(context.Background(), logInfo.ID, model.MqttResponse{
			//	Result:  1,
			//	Errcode: "timeout",
			//	Message: "设备响应超时",
			//	Ts:      time.Now().Unix(),
			//})
			close(config.MqttDirectResponseFuncMap[messageID])
			delete(config.MqttDirectResponseFuncMap, messageID)

			return
		}
	}()
	return err
}

// 发送获取属性请求
func (t *AttributeData) AttributeGetMessage(userClaims *utils.UserClaims, req *model.AttributeGetMessageReq) error {
	logrus.Debug("AttributeGetMessage")
	// 获取设备编码
	d, err := dal.GetDeviceByID(req.DeviceID)
	if err != nil {
		return err
	}

	if d.DeviceNumber == "" {
		// 没有设备编号，不支持获取属性
		return nil
	}
	//组装payload{"keys":["temp","hum"]}||{}
	var payload []byte
	var data map[string]interface{}
	if len(req.Keys) == 0 {
		data = map[string]interface{}{
			"keys": []string{},
		}
	} else {

		data = map[string]interface{}{
			"keys": req.Keys,
		}
	}
	payload, err = json.Marshal(data)
	if err != nil {
		return err
	}
	// 发送获取属性请求
	err = publish.PublishGetAttributeMessage(d.DeviceNumber, payload)
	return err
}

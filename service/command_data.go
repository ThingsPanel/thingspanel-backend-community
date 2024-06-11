package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"project/common"
	"project/constant"
	dal "project/dal"
	"project/initialize"
	model "project/model"
	config "project/mqtt"
	"project/mqtt/publish"
	"project/query"
	"strconv"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type CommandData struct{}

func (t *CommandData) GetCommandSetLogsDataListByPage(req model.GetCommandSetLogsListByPageReq) (interface{}, error) {
	count, data, err := dal.GetCommandSetLogsDataListByPage(req)
	if err != nil {
		return nil, err
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data

	return dataMap, nil
}

func (t *CommandData) CommandPutMessage(ctx context.Context, userID string, param *model.PutMessageForCommand, operationType string, fn ...config.MqttDirectResponseFunc) error {
	var (
		log = dal.CommandSetLogsQuery{}

		errorMessage string
	)

	deviceInfo, err := initialize.GetDeviceById(param.DeviceID)
	if err != nil {
		logrus.Error(ctx, "[CommandPutMessage][GetDeviceById]failed:", err)
		return err
	}
	messageID := common.GetMessageID()
	var paramsMap map[string]interface{}
	if param.Value != nil && *param.Value != "" {
		// 验证是否为json格式
		if !IsJSON(*param.Value) {
			err = errors.New("value is not json format")
			return err
		}
		err = json.Unmarshal([]byte(*param.Value), &paramsMap)
		if err != nil {
			logrus.Error(ctx, "[CommandPutMessage][Unmarshal]failed:", err)
			return err
		}
	}
	// 拼接payload
	var payloadMap = make(map[string]interface{})
	payloadMap["method"] = param.Identify
	if paramsMap != nil {
		payloadMap["params"] = paramsMap
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		logrus.Error(ctx, "[CommandPutMessage][Marshal]failed:", err)
		return err
	}
	topic := fmt.Sprintf("%s%s/%s", config.MqttConfig.Commands.PublishTopic, deviceInfo.DeviceNumber, messageID)
	// 判断是否协议插件，如果是则下发到协议插件
	if deviceInfo.DeviceConfigID != nil {
		// 查询协议插件信息
		protocolPluginInfo, err := dal.GetProtocolPluginByDeviceConfigID(*deviceInfo.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "[CommandPutMessage][GetProtocolPluginByDeviceConfigID]failed:", err)
			return err
		}
		if protocolPluginInfo != nil && protocolPluginInfo.SubTopicPrefix != nil {
			// 增加主题前缀
			topic = fmt.Sprintf("%s%s", *protocolPluginInfo.SubTopicPrefix, topic)
		}
	}
	err = publish.PublishCommandMessage(topic, payload)
	if err != nil {
		logrus.Error(ctx, "下发失败", err)
		errorMessage = err.Error()
	}
	data := string(payload)
	//operationType := strconv.Itoa(constant.Manual)
	description := "下发命令日志记录"
	logInfo := &model.CommandSetLog{
		ID:            uuid.New(),
		DeviceID:      param.DeviceID,
		OperationType: &operationType,
		MessageID:     &messageID,
		Datum:         &data,
		RspDatum:      nil,
		ErrorMessage:  &errorMessage,
		CreatedAt:     time.Now().UTC(),
		UserID:        &userID,
		Description:   &description,
		Identify:      &param.Identify,
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
			log.CommandResultUpdate(context.Background(), logInfo.ID, response)
			close(config.MqttDirectResponseFuncMap[messageID])
			delete(config.MqttDirectResponseFuncMap, messageID)
		case <-time.After(3 * time.Minute): // 设置超时时间为 3 分钟
			fmt.Println("超时，关闭通道")
			//log.CommandResultUpdate(context.Background(), logInfo.ID, model.MqttResponse{
			//	Result:  1,
			//	Errcode: "timeout",
			//	Message: "设备响应超时",
			//	Ts:      time.Now().Unix(),
			//	Method:  param.Identify,
			//})
			close(config.MqttDirectResponseFuncMap[messageID])
			delete(config.MqttDirectResponseFuncMap, messageID)

			return
		}
	}()

	return err
}

func (t *CommandData) GetCommonList(ctx context.Context, id string) ([]model.GetCommandListRes, error) {
	var (
		list = make([]model.GetCommandListRes, 0)
	)

	deviceInfo, err := dal.DeviceQuery{}.First(ctx, query.Device.ID.Eq(id))
	if err != nil {
		logrus.Error(ctx, "[GetCommonList]device failed:", err)
		return list, err
	}

	if deviceInfo.DeviceConfigID == nil || common.CheckEmpty(*deviceInfo.DeviceConfigID) {
		logrus.Debug("device.device_config_id is empty")
		return list, nil
	}

	deviceConfigsInfo, err := dal.DeviceConfigQuery{}.First(ctx, query.DeviceConfig.ID.Eq(*deviceInfo.DeviceConfigID))
	if err != nil {
		logrus.Debug(ctx, "[GetCommonList]device_configs failed:", err)
		return list, err
	}

	if deviceConfigsInfo.DeviceTemplateID == nil || common.CheckEmpty(*deviceConfigsInfo.DeviceTemplateID) {
		logrus.Debug("device_configs.device_template_id is empty")
		return list, err
	}

	commandList, err := dal.DeviceModelCommandsQuery{}.Find(ctx, query.DeviceModelCommand.DeviceTemplateID.Eq(*deviceConfigsInfo.DeviceTemplateID))
	if err != nil {
		logrus.Error(ctx, "[GetCommonList]device_model_command failed:", err)
		return list, err
	}

	for _, info := range commandList {
		commandRes := model.GetCommandListRes{
			Identifier: info.DataIdentifier,
		}
		if info.DataName != nil {
			commandRes.Name = *info.DataName
		}
		if info.Param != nil {
			commandRes.Params = *info.Param
		}
		if info.Description != nil {
			commandRes.Description = *info.Description
		}
		list = append(list, commandRes)
	}

	return list, err
}

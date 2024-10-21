package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"project/initialize"
	dal "project/internal/dal"
	model "project/internal/model"
	"project/internal/query"
	config "project/mqtt"
	"project/mqtt/publish"
	"project/pkg/common"
	"project/pkg/constant"
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
	// 获取设备信息
	deviceInfo, err := initialize.GetDeviceById(param.DeviceID)
	if err != nil {
		return fmt.Errorf("获取设备信息失败: %v", err)
	}

	// 获取设备类型和协议
	deviceType, protocolType := "1", "MQTT"
	var deviceConfig *model.DeviceConfig
	if deviceInfo.DeviceConfigID != nil {
		deviceConfig, err = dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return fmt.Errorf("获取设备配置失败: %v", err)
		}
		deviceType = deviceConfig.DeviceType
		if deviceConfig.ProtocolType != nil {
			protocolType = *deviceConfig.ProtocolType
		} else {
			return fmt.Errorf("protocolType 为空")
		}
	}

	// 生成消息ID和主题
	messageID := common.GetMessageID()
	topic := fmt.Sprintf("%s%s/%s", config.MqttConfig.Commands.PublishTopic, deviceInfo.DeviceNumber, messageID)

	// 处理非MQTT协议
	if deviceConfig != nil && protocolType != "MQTT" {
		subTopicPrefix, err := dal.GetServicePluginSubTopicPrefixByDeviceConfigID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return fmt.Errorf("获取子主题前缀失败: %v", err)
		}
		topic = fmt.Sprintf("%s%s%s/%s", subTopicPrefix, config.MqttConfig.Commands.PublishTopic, deviceInfo.ID, messageID)
	}

	// 构建payload
	payloadMap := map[string]interface{}{"method": param.Identify}
	if param.Value != nil && *param.Value != "" {
		if !IsJSON(*param.Value) {
			return errors.New("value is not JSON format")
		}
		var params interface{}
		if err := json.Unmarshal([]byte(*param.Value), &params); err != nil {
			return fmt.Errorf("解析参数失败: %v", err)
		}
		payloadMap["params"] = params
	}

	// 处理网关和子设备
	if protocolType == "MQTT" && (deviceType == "2" || deviceType == "3") {
		gatewayID := deviceInfo.ID
		if deviceType == "3" {
			if deviceInfo.ParentID == nil {
				return fmt.Errorf("子设备网关信息为空")
			}
			gatewayID = *deviceInfo.ParentID
			if deviceInfo.SubDeviceAddr == nil {
				return fmt.Errorf("子设备地址为空")
			}
			payloadMap = map[string]interface{}{
				"sub_device_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: payloadMap,
				},
			}
		} else {
			payloadMap = map[string]interface{}{"gateway_data": payloadMap}
		}

		gatewayInfo, err := initialize.GetDeviceById(gatewayID)
		if err != nil {
			return fmt.Errorf("获取网关信息失败: %v", err)
		}
		topic = fmt.Sprintf("%s%s/%s", config.MqttConfig.Commands.GatewayPublishTopic, gatewayInfo.DeviceNumber, messageID)
	}

	// 序列化payload
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return fmt.Errorf("生成 payload 失败: %v", err)
	}

	// 执行数据脚本
	if deviceInfo.DeviceConfigID != nil && *deviceInfo.DeviceConfigID != "" {
		if newPayload, err := GroupApp.DataScript.Exec(deviceInfo, "E", payload, topic); err != nil {
			return fmt.Errorf("执行数据脚本失败: %v", err)
		} else if newPayload != nil {
			payload = newPayload
		}
	}

	// 发布消息
	err = publish.PublishCommandMessage(topic, payload)
	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
		logrus.Error(ctx, "下发失败", err)
	}

	// 创建日志
	status := strconv.Itoa(constant.StatusOK)
	if errorMessage != "" {
		status = strconv.Itoa(constant.StatusFailed)
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
		Status:        &status,
		ErrorMessage:  &errorMessage,
		CreatedAt:     time.Now().UTC(),
		UserID:        &userID,
		Description:   &description,
		Identify:      &param.Identify,
	}
	_, _ = dal.CommandSetLogsQuery{}.Create(ctx, logInfo)
	config.MqttDirectResponseFuncMap[messageID] = make(chan model.MqttResponse)
	go func() {
		select {
		case response := <-config.MqttDirectResponseFuncMap[messageID]:
			fmt.Println("接收到数据:", response)
			if len(fn) > 0 {
				_ = fn[0](response)
			}
			dal.CommandSetLogsQuery{}.CommandResultUpdate(context.Background(), logInfo.ID, response)
			close(config.MqttDirectResponseFuncMap[messageID])
			delete(config.MqttDirectResponseFuncMap, messageID)
		case <-time.After(6 * time.Minute): // 设置超时时间为 3 分钟
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

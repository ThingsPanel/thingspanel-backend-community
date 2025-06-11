package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"project/initialize"
	dal "project/internal/dal"
	model "project/internal/model"
	"project/internal/query"
	config "project/mqtt"
	"project/mqtt/publish"
	"project/pkg/common"
	"project/pkg/constant"
	"project/pkg/errcode"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type CommandData struct{}

func (*CommandData) GetCommandSetLogsDataListByPage(req model.GetCommandSetLogsListByPageReq) (interface{}, error) {
	count, data, err := dal.GetCommandSetLogsDataListByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data

	return dataMap, nil
}

func (*CommandData) CommandPutMessage(ctx context.Context, userID string, param *model.PutMessageForCommand, operationType string, fn ...config.MqttDirectResponseFunc) error {
	// 获取设备信息
	deviceInfo, err := initialize.GetDeviceCacheById(param.DeviceID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 获取设备类型和协议
	deviceType, protocolType := "1", "MQTT"
	var deviceConfig *model.DeviceConfig
	if deviceInfo.DeviceConfigID != nil {
		deviceConfig, err = dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		deviceType = deviceConfig.DeviceType
		if deviceConfig.ProtocolType != nil {
			protocolType = *deviceConfig.ProtocolType
		} else {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": "protocol_type is empty",
			})
		}
	}

	// 生成消息ID和主题
	messageID := common.GetMessageID()
	topic := fmt.Sprintf("%s%s/%s", config.MqttConfig.Commands.PublishTopic, deviceInfo.DeviceNumber, messageID)

	// 处理非MQTT协议
	if deviceConfig != nil && protocolType != "MQTT" {
		subTopicPrefix, err := dal.GetServicePluginSubTopicPrefixByDeviceConfigID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		topic = fmt.Sprintf("%s%s%s/%s", subTopicPrefix, config.MqttConfig.Commands.PublishTopic, deviceInfo.ID, messageID)
	}

	// 构建payload
	payloadMap := map[string]interface{}{"method": param.Identify}
	if param.Value != nil && *param.Value != "" {
		if !IsJSON(*param.Value) {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": "value is not json",
			})
		}
		var params interface{}
		if err := json.Unmarshal([]byte(*param.Value), &params); err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		payloadMap["params"] = params
	}

	// 执行数据脚本
	if deviceInfo.DeviceConfigID != nil && *deviceInfo.DeviceConfigID != "" {
		payloadBytes, err := json.Marshal(payloadMap)
		if err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		if newPayload, err := GroupApp.DataScript.Exec(deviceInfo, "E", payloadBytes, topic); err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": err.Error(),
			})
		} else if newPayload != nil {
			var err error
			if err = json.Unmarshal(newPayload, &payloadMap); err != nil {
				return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": err.Error(),
				})
			}
		}
	}

	// 处理网关和子设备
	if protocolType == "MQTT" && (deviceType == "2" || deviceType == "3") {
		gatewayID := deviceInfo.ID
		if deviceType == "3" {
			if deviceInfo.ParentID == nil {
				return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": "sub_device_type is 3, but parent_id is empty",
				})
			}
			gatewayID = *deviceInfo.ParentID
			if deviceInfo.SubDeviceAddr == nil {
				return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error": "sub_device_addr is empty",
				})
			}
			payloadMap = map[string]interface{}{
				"sub_device_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: payloadMap,
				},
			}
		} else {
			payloadMap = map[string]interface{}{"gateway_data": payloadMap}
		}

		gatewayInfo, err := initialize.GetDeviceCacheById(gatewayID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		topic = fmt.Sprintf("%s%s/%s", config.MqttConfig.Commands.GatewayPublishTopic, gatewayInfo.DeviceNumber, messageID)
	}

	// 序列化payload
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": err.Error(),
		})
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
	// operationType := strconv.Itoa(constant.Manual)
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
	// 如果不是直连设备，则使用网关通道
	config.MqttResponseFuncMap[messageID] = make(chan model.MqttResponse)
	go func() {
		select {
		case response := <-config.MqttResponseFuncMap[messageID]:
			fmt.Println("接收到数据:", response)
			if len(fn) > 0 {
				_ = fn[0](response)
			}
			dal.CommandSetLogsQuery{}.CommandResultUpdate(context.Background(), logInfo.ID, response)
			close(config.MqttResponseFuncMap[messageID])
			delete(config.MqttResponseFuncMap, messageID)
		case <-time.After(6 * time.Minute): // 设置超时时间为 3 分钟
			fmt.Println("超时，关闭通道")
			//log.CommandResultUpdate(context.Background(), logInfo.ID, model.MqttResponse{
			//	Result:  1,
			//	Errcode: "timeout",
			//	Message: "设备响应超时",
			//	Ts:      time.Now().Unix(),
			//	Method:  param.Identify,
			//})
			close(config.MqttResponseFuncMap[messageID])
			delete(config.MqttResponseFuncMap, messageID)

			return
		}
	}()

	return err
}

func (*CommandData) GetCommonList(ctx context.Context, id string) ([]model.GetCommandListRes, error) {
	list := make([]model.GetCommandListRes, 0)

	deviceInfo, err := dal.DeviceQuery{}.First(ctx, query.Device.ID.Eq(id))
	if err != nil {
		logrus.Error(ctx, "[GetCommonList]device failed:", err)
		return list, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	if deviceInfo.DeviceConfigID == nil || common.CheckEmpty(*deviceInfo.DeviceConfigID) {
		logrus.Debug("device.device_config_id is empty")
		return list, nil
	}

	deviceConfigsInfo, err := dal.DeviceConfigQuery{}.First(ctx, query.DeviceConfig.ID.Eq(*deviceInfo.DeviceConfigID))
	if err != nil {
		logrus.Debug(ctx, "[GetCommonList]device_configs failed:", err)
		return list, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	if deviceConfigsInfo.DeviceTemplateID == nil || common.CheckEmpty(*deviceConfigsInfo.DeviceTemplateID) {
		logrus.Debug("device_configs.device_template_id is empty")
		return list, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "device_configs.device_template_id is empty",
		})
	}

	commandList, err := dal.DeviceModelCommandsQuery{}.Find(ctx, query.DeviceModelCommand.DeviceTemplateID.Eq(*deviceConfigsInfo.DeviceTemplateID))
	if err != nil {
		logrus.Error(ctx, "[GetCommonList]device_model_command failed:", err)
		return list, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
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

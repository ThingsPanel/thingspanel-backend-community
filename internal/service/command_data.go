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
		// 递归查找顶层网关
		topGatewayInfo, err := findTopLevelGatewayForCommand(deviceInfo, deviceType)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		
		// 构建多层网关格式的payload
		payloadMap, err = transformCommandDataForMultiLevelGateway(payloadMap, deviceInfo, deviceType)
		if err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		
		topic = fmt.Sprintf("%s%s/%s", config.MqttConfig.Commands.GatewayPublishTopic, topGatewayInfo.DeviceNumber, messageID)
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

// findTopLevelGatewayForCommand 递归查找顶层网关（用于命令下发）
func findTopLevelGatewayForCommand(deviceInfo *model.Device, deviceType string) (*model.Device, error) {
	currentDevice := deviceInfo
	
	// 如果是子设备(3)，先找到它的父设备
	if deviceType == "3" {
		if deviceInfo.ParentID == nil {
			return nil, fmt.Errorf("子设备的parentID为空")
		}
		parentDevice, err := initialize.GetDeviceCacheById(*deviceInfo.ParentID)
		if err != nil {
			return nil, fmt.Errorf("获取父设备信息失败: %v", err)
		}
		currentDevice = parentDevice
	}
	
	// 递归查找顶层网关（parent_id为空的设备）
	maxDepth := 10 // 防止无限循环
	depth := 0
	
	for currentDevice.ParentID != nil && depth < maxDepth {
		parentDevice, err := initialize.GetDeviceCacheById(*currentDevice.ParentID)
		if err != nil {
			return nil, fmt.Errorf("获取父设备信息失败: %v", err)
		}
		currentDevice = parentDevice
		depth++
	}
	
	if depth >= maxDepth {
		return nil, fmt.Errorf("网关层级过深，超过最大深度限制")
	}
	
	// 确保找到的是网关设备（device_type=2）
	if currentDevice.DeviceConfigID != nil {
		deviceConfig, err := dal.GetDeviceConfigByID(*currentDevice.DeviceConfigID)
		if err != nil {
			return nil, fmt.Errorf("获取设备配置失败: %v", err)
		}
		if deviceConfig.DeviceType != strconv.Itoa(constant.GATEWAY_DEVICE) {
			return nil, fmt.Errorf("顶层设备不是网关类型")
		}
	}
	
	return currentDevice, nil
}

// transformCommandDataForMultiLevelGateway 为多层网关构建命令数据格式
func transformCommandDataForMultiLevelGateway(payloadMap map[string]interface{}, deviceInfo *model.Device, deviceType string) (map[string]interface{}, error) {
	// 根据设备类型和是否有父网关构建不同的输出数据结构
	var outputData map[string]interface{}
	if deviceType == "3" { // 子设备
		if deviceInfo.SubDeviceAddr == nil {
			return nil, fmt.Errorf("子设备的SubDeviceAddr为空")
		}
		
		// 查找子设备的直接父网关（可能是子网关）
		parentGateway, err := initialize.GetDeviceCacheById(*deviceInfo.ParentID)
		if err != nil {
			return nil, fmt.Errorf("获取父设备信息失败: %v", err)
		}
		
		// 如果父网关是子网关（有parent_id），需要嵌套结构
		if parentGateway.ParentID != nil {
			// 父网关是子网关，需要构建嵌套的sub_gateway_data结构
			if parentGateway.SubDeviceAddr == nil {
				return nil, fmt.Errorf("父网关的SubDeviceAddr为空")
			}
			outputData = buildNestedSubGatewayDataForCommand(parentGateway, *deviceInfo.SubDeviceAddr, payloadMap)
		} else {
			// 父网关是顶层网关，直接构建sub_device_data
			outputData = map[string]interface{}{
				"sub_device_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: payloadMap,
				},
			}
		}
	} else if deviceType == "2" { // 网关设备
		if deviceInfo.ParentID != nil {
			// 子网关：构建为sub_gateway_data格式
			if deviceInfo.SubDeviceAddr == nil {
				return nil, fmt.Errorf("子网关的SubDeviceAddr为空")
			}
			outputData = map[string]interface{}{
				"sub_gateway_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: map[string]interface{}{
						"gateway_data": payloadMap,
					},
				},
			}
		} else {
			// 顶层网关：构建为gateway_data格式
			outputData = map[string]interface{}{
				"gateway_data": payloadMap,
			}
		}
	}

	return outputData, nil
}

// buildNestedSubGatewayDataForCommand 递归构建多层子网关的嵌套命令数据结构
func buildNestedSubGatewayDataForCommand(gateway *model.Device, subDeviceAddr string, payloadMap map[string]interface{}) map[string]interface{} {
	if gateway.ParentID == nil {
		// 到达顶层网关，构建最内层结构
		return map[string]interface{}{
			"sub_device_data": map[string]interface{}{
				subDeviceAddr: payloadMap,
			},
		}
	}
	
	// 递归查找父网关并构建嵌套结构
	parentGateway, err := initialize.GetDeviceCacheById(*gateway.ParentID)
	if err != nil {
		// 如果出错，返回当前层级的结构
		return map[string]interface{}{
			"sub_gateway_data": map[string]interface{}{
				*gateway.SubDeviceAddr: map[string]interface{}{
					"sub_device_data": map[string]interface{}{
						subDeviceAddr: payloadMap,
					},
				},
			},
		}
	}
	
	// 构建当前层级的嵌套结构
	innerData := buildNestedSubGatewayDataForCommand(parentGateway, subDeviceAddr, payloadMap)
	
	// 如果父网关也是子网关，继续嵌套
	if parentGateway.ParentID != nil {
		return map[string]interface{}{
			"sub_gateway_data": map[string]interface{}{
				*gateway.SubDeviceAddr: innerData,
			},
		}
	} else {
		// 父网关是顶层网关
		return map[string]interface{}{
			"sub_gateway_data": map[string]interface{}{
				*gateway.SubDeviceAddr: map[string]interface{}{
					"sub_device_data": map[string]interface{}{
						subDeviceAddr: payloadMap,
					},
				},
			},
		}
	}
}

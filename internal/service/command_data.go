package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"project/initialize"
	"project/internal/dal"
	"project/internal/downlink"
	"project/internal/model"
	query "project/internal/query"
	config "project/mqtt"
	"project/pkg/common"
	"project/pkg/constant"
	"project/pkg/errcode"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type CommandData struct {
	downlinkBus *downlink.Bus // ✨ 依赖注入
}

// SetDownlinkBus 设置 downlink Bus（在 Application 初始化时调用）
func (c *CommandData) SetDownlinkBus(bus *downlink.Bus) {
	c.downlinkBus = bus
}

// PutMessage 下发命令（改造为异步模式，支持多层网关）
// 保持原有的 CommandPutMessage 接口签名
func (c *CommandData) CommandPutMessage(ctx context.Context, operatorID string, putMessageReq *model.PutMessageForCommand, operationType string) error {
	// 1. 获取设备信息
	device, err := initialize.GetDeviceCacheById(putMessageReq.DeviceID)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	// 2. 生成 message_id，8位唯一字符串
	messageId := uuid.New()[:8]

	// 3. 获取设备类型
	var deviceType string
	if device.DeviceConfigID != nil {
		deviceConfig, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		if err != nil {
			return fmt.Errorf("failed to get device config: %w", err)
		}
		deviceType = deviceConfig.DeviceType
	}

	// 4. 构造命令数据
	var valueStr string
	if putMessageReq.Value != nil {
		valueStr = *putMessageReq.Value
	}

	commandData := map[string]interface{}{
		"method": putMessageReq.Identify, // identify 映射为 method
		"params": json.RawMessage(valueStr), // Value 是 JSON 字符串
	}

	// 5. 处理多层网关数据嵌套
	transformedData, err := transformCommandDataForMultiLevelGateway(commandData, device, deviceType)
	if err != nil {
		return fmt.Errorf("failed to transform command data: %w", err)
	}

	jsonData, _ := json.Marshal(transformedData)

	// 6. 处理网关层级，获取顶层网关
	targetDevice, topic, err := c.resolveTopLevelGatewayAndTopic(device, deviceType, messageId)
	if err != nil {
		return err
	}

	// 7. 创建 pending 日志（记录转换后的完整数据）
	transformedDataStr := string(jsonData)
	if err := c.createCommandLogForPut(device, messageId, putMessageReq.Identify, &transformedDataStr, operationType); err != nil {
		logrus.WithError(err).Error("Failed to create command log")
		// 不阻塞发送流程
	}

	// 8. 使用 downlink.Bus 发送
	if c.downlinkBus != nil {
		msg := &downlink.Message{
			DeviceID:       device.ID,                      // 原始设备ID（用于日志关联）
			DeviceConfigID: c.getDeviceConfigID(targetDevice), // 使用顶层网关的配置ID（用于脚本编码）
			Type:           downlink.MessageTypeCommand,
			Data:           jsonData,
			Topic:          topic, // 顶层网关的Topic
			MessageID:      messageId,
		}
		c.downlinkBus.PublishCommand(msg)

		logrus.WithFields(logrus.Fields{
			"device_id":        device.ID,
			"target_device_id": targetDevice.ID,
			"message_id":       messageId,
			"identify":         putMessageReq.Identify,
		}).Info("Command sent via downlink")
	} else {
		return fmt.Errorf("downlink service not available")
	}

	return nil
}

// createCommandLogForPut 创建命令日志（for PutMessageForCommand）
func (c *CommandData) createCommandLogForPut(device *model.Device, messageId, identify string, value *string, operationType string) error {
	status := "0" // pending
	log := &model.CommandSetLog{
		ID:            uuid.New(),
		DeviceID:      device.ID,
		OperationType: &operationType,
		MessageID:     &messageId,
		Identify:      &identify,
		Datum:         value, // 直接使用 *string
		Status:        &status,
		ErrorMessage:  nil,
		CreatedAt:     time.Now(),
	}

	return dal.CreateCommandSetLog(log)
}

// resolveTopLevelGatewayAndTopic 处理多层网关，返回顶层网关设备和Topic
func (c *CommandData) resolveTopLevelGatewayAndTopic(device *model.Device, deviceType, messageId string) (*model.Device, string, error) {
	// 直连设备（无父网关）
	if device.ParentID == nil || *device.ParentID == "" {
		topic := c.buildTopic(config.MqttConfig.Commands.PublishTopic, device.DeviceNumber, messageId)
		return device, topic, nil
	}

	// 网关子设备或子网关，查找顶层网关
	topGateway, err := findTopLevelGatewayForCommand(device, deviceType)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find top level gateway: %w", err)
	}

	// 使用顶层网关构建 Topic
	topic := c.buildTopic(config.MqttConfig.Commands.GatewayPublishTopic, topGateway.DeviceNumber, messageId)

	// 检查是否有协议插件前缀
	if topGateway.DeviceConfigID != nil {
		protocolPlugin, err := dal.GetProtocolPluginByDeviceConfigID(*topGateway.DeviceConfigID)
		if err == nil && protocolPlugin != nil && protocolPlugin.SubTopicPrefix != nil {
			topic = fmt.Sprintf("%s%s", *protocolPlugin.SubTopicPrefix, topic)
		}
	}

	return topGateway, topic, nil
}

// buildTopic 构建 Topic，兼容配置中缺少占位符的情况
func (c *CommandData) buildTopic(topicTemplate, deviceNumber, messageId string) string {
	// 尝试使用 fmt.Sprintf（如果配置中有 %s）
	topic := fmt.Sprintf(topicTemplate, deviceNumber, messageId)

	// 检查是否有格式化错误（%!(EXTRA ...）
	if strings.Contains(topic, "%!(EXTRA") {
		// 配置缺少占位符，手动拼接
		// 清理掉错误提示部分
		topic = topicTemplate + deviceNumber + "/" + messageId

		logrus.WithFields(logrus.Fields{
			"template": topicTemplate,
			"device":   deviceNumber,
			"msg_id":   messageId,
			"result":   topic,
		}).Warn("Topic template missing format specifiers, using fallback concatenation")
	}

	return topic
}

// getDeviceConfigID 获取设备配置ID
func (c *CommandData) getDeviceConfigID(device *model.Device) string {
	if device.DeviceConfigID == nil {
		return ""
	}
	return *device.DeviceConfigID
}

// GetCommonList 获取命令列表（保留原有方法）
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
		return list, nil
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

// findTopLevelGatewayForCommand 递归查找顶层网关（保留原有方法）
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

// transformCommandDataForMultiLevelGateway 为多层网关构建命令数据格式（保留原有方法）
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
	} else {
		// 直连设备（deviceType == "1" 或其他）：不需要嵌套，直接返回原始数据
		outputData = payloadMap
	}

	return outputData, nil
}

// buildNestedSubGatewayDataForCommand 递归构建多层子网关的嵌套命令数据结构（保留原有方法）
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

// GetCommandSetLogsDataListByPage 获取命令下发日志（分页）
func (c *CommandData) GetCommandSetLogsDataListByPage(req model.GetCommandSetLogsListByPageReq) (map[string]interface{}, error) {
	// 查询日志列表
	logs, total, err := dal.GetCommandSetLogsByPage(&req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return map[string]interface{}{
		"list":  logs,
		"total": total,
	}, nil
}

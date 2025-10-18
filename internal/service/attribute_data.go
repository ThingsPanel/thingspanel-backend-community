package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"project/initialize"
	dal "project/internal/dal"
	"project/internal/downlink"
	model "project/internal/model"
	config "project/mqtt"
	"project/mqtt/publish"
	"project/pkg/constant"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type AttributeData struct {
	downlinkBus *downlink.Bus // ✨ 依赖注入
}

func (*AttributeData) GetAttributeDataList(device_id string) (interface{}, error) {
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

		if v["read_write_flag"] != nil {
			d["read_write_flag"] = v["read_write_flag"]
		}

		easyData = append(easyData, d)
	}

	return easyData, nil
}

func (*AttributeData) DeleteAttributeData(id string) error {
	err := dal.DeleteAttributeData(id)
	return err
}

func (*AttributeData) GetAttributeSetLogsDataListByPage(req model.GetAttributeSetLogsListByPageReq) (interface{}, error) {
	count, data, err := dal.GetAttributeSetLogsDataListByPage(req)
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

// 根据key查询设备属性
func (*AttributeData) GetAttributeDataByKey(req model.GetDataListByKeyReq) (interface{}, error) {
	dataMap := make(map[string]interface{})

	data, err := dal.GetAttributeDataByKey(req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dataMap, nil
		}
		return dataMap, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	dataMap["id"] = data.ID
	dataMap["key"] = data.Key
	dataMap["device_id"] = data.DeviceID
	dataMap["ts"] = data.T
	if data.BoolV != nil {
		dataMap["value"] = data.BoolV
	} else if data.NumberV != nil {
		dataMap["value"] = data.NumberV
	} else if data.StringV != nil {
		dataMap["value"] = *data.StringV
	} else {
		dataMap["value"] = nil
	}

	return dataMap, nil
}

// SetDownlinkBus 设置 downlink Bus（在 Application 初始化时调用）
func (a *AttributeData) SetDownlinkBus(bus *downlink.Bus) {
	a.downlinkBus = bus
}

// AttributePutMessage 属性设置下发（改造为异步模式）
func (a *AttributeData) AttributePutMessage(ctx context.Context, operatorID string, putMessageReq *model.AttributePutMessage, operationType string) error {
	// 1. 获取设备信息
	device, err := initialize.GetDeviceCacheById(putMessageReq.DeviceID)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	// 2. 生成 message_id（8位唯一字符串）
	messageId := uuid.New()[:8]

	// 3. 处理网关层级
	actualDeviceID, topic, err := a.resolveDeviceAndTopic(device, messageId)
	if err != nil {
		return err
	}

	// 4. 构造属性数据（保持原有格式）
	// Value 已经是 JSON 字符串格式
	jsonData := []byte(putMessageReq.Value)

	// 5. 创建 pending 日志
	if err := a.createAttributeLog(device, messageId, putMessageReq.Value, operationType); err != nil {
		logrus.WithError(err).Error("Failed to create attribute log")
		// 不阻塞发送流程
	}

	// ✨ 6. 使用 downlink.Bus 发送
	if a.downlinkBus != nil {
		msg := &downlink.Message{
			DeviceID:       actualDeviceID,
			DeviceConfigID: a.getDeviceConfigID(device),
			Type:           downlink.MessageTypeAttributeSet,
			Data:           jsonData,
			Topic:          topic,
			MessageID:      messageId,
		}
		a.downlinkBus.PublishAttributeSet(msg)

		logrus.WithFields(logrus.Fields{
			"device_id":  actualDeviceID,
			"message_id": messageId,
		}).Info("Attribute set sent via downlink")
	} else {
		return fmt.Errorf("downlink service not available")
	}

	return nil
}

// resolveDeviceAndTopic 处理网关层级，返回实际设备ID和Topic
func (a *AttributeData) resolveDeviceAndTopic(device *model.Device, messageId string) (string, string, error) {
	// 直连设备
	if device.ParentID == nil || *device.ParentID == "" {
		topic := a.buildTopic(config.MqttConfig.Attributes.PublishTopic, device.DeviceNumber, messageId)
		return device.ID, topic, nil
	}

	// 网关子设备
	gatewayDevice, err := initialize.GetDeviceCacheById(*device.ParentID)
	if err != nil {
		return "", "", fmt.Errorf("gateway device not found: %w", err)
	}

	// 网关 Topic
	topic := a.buildTopic(config.MqttConfig.Attributes.GatewayPublishTopic, gatewayDevice.DeviceNumber, messageId)

	// 检查是否有协议插件前缀
	if gatewayDevice.DeviceConfigID != nil {
		protocolPlugin, err := dal.GetProtocolPluginByDeviceConfigID(*gatewayDevice.DeviceConfigID)
		if err == nil && protocolPlugin != nil && protocolPlugin.SubTopicPrefix != nil {
			topic = fmt.Sprintf("%s%s", *protocolPlugin.SubTopicPrefix, topic)
		}
	}

	return device.ID, topic, nil
}

// buildTopic 构建 Topic，兼容配置中缺少占位符的情况
func (a *AttributeData) buildTopic(topicTemplate, deviceNumber, messageId string) string {
	// 尝试使用 fmt.Sprintf（如果配置中有 %s）
	topic := fmt.Sprintf(topicTemplate, deviceNumber, messageId)

	topic = topicTemplate + deviceNumber + "/" + messageId

	return topic
}

// createAttributeLog 创建属性设置日志
func (a *AttributeData) createAttributeLog(device *model.Device, messageId, value, operationType string) error {
	status := "0" // pending
	log := &model.AttributeSetLog{
		ID:            uuid.New(),
		DeviceID:      device.ID,
		OperationType: &operationType,
		MessageID:     &messageId,
		Datum:         &value,
		Status:        &status,
		ErrorMessage:  nil,
		CreatedAt:     time.Now(),
	}

	return dal.CreateAttributeSetLog(log)
}

// getDeviceConfigID 获取设备配置ID
func (a *AttributeData) getDeviceConfigID(device *model.Device) string {
	if device.DeviceConfigID == nil {
		return ""
	}
	return *device.DeviceConfigID
}

func (*AttributeData) AttributeGetMessage(_ *utils.UserClaims, req *model.AttributeGetMessageReq) error {
	logrus.Debug("AttributeGetMessage")
	// 获取设备编码
	d, err := dal.GetDeviceByID(req.DeviceID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	if d.DeviceNumber == "" {
		// 没有设备编号，不支持获取属性
		return nil
	}
	// 组装payload{"keys":["temp","hum"]}||{}
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
		return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"system_error": err.Error(),
		})
	}
	// 发送获取属性请求
	err = publish.PublishGetAttributeMessage(d.DeviceNumber, payload)
	if err != nil {
		return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"system_error": err.Error(),
		})
	}
	return err
}

// findTopLevelGatewayForAttribute 递归查找顶层网关（用于属性设置）
func findTopLevelGatewayForAttribute(deviceInfo *model.Device, deviceType string) (*model.Device, error) {
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

// transformAttributeDataForMultiLevelGateway 为多层网关构建属性数据格式
func transformAttributeDataForMultiLevelGateway(param *model.AttributePutMessage, deviceInfo *model.Device, deviceType string) error {
	// 解析JSON
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(param.Value), &inputData); err != nil {
		return fmt.Errorf("解析输入JSON失败: %v", err)
	}

	// 根据设备类型和是否有父网关构建不同的输出数据结构
	var outputData map[string]interface{}
	if deviceType == "3" { // 子设备
		if deviceInfo.SubDeviceAddr == nil {
			return fmt.Errorf("子设备的SubDeviceAddr为空")
		}

		// 查找子设备的直接父网关（可能是子网关）
		parentGateway, err := initialize.GetDeviceCacheById(*deviceInfo.ParentID)
		if err != nil {
			return fmt.Errorf("获取父设备信息失败: %v", err)
		}

		// 如果父网关是子网关（有parent_id），需要嵌套结构
		if parentGateway.ParentID != nil {
			// 父网关是子网关，需要构建嵌套的sub_gateway_data结构
			if parentGateway.SubDeviceAddr == nil {
				return fmt.Errorf("父网关的SubDeviceAddr为空")
			}
			outputData = buildNestedSubGatewayDataForAttribute(parentGateway, *deviceInfo.SubDeviceAddr, inputData)
		} else {
			// 父网关是顶层网关，直接构建sub_device_data
			outputData = map[string]interface{}{
				"sub_device_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: inputData,
				},
			}
		}
	} else if deviceType == "2" { // 网关设备
		if deviceInfo.ParentID != nil {
			// 子网关：构建为sub_gateway_data格式
			if deviceInfo.SubDeviceAddr == nil {
				return fmt.Errorf("子网关的SubDeviceAddr为空")
			}
			outputData = map[string]interface{}{
				"sub_gateway_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: map[string]interface{}{
						"gateway_data": inputData,
					},
				},
			}
		} else {
			// 顶层网关：构建为gateway_data格式
			outputData = map[string]interface{}{
				"gateway_data": inputData,
			}
		}
	}

	// 重新构建payload
	output, err := json.Marshal(outputData)
	if err != nil {
		return fmt.Errorf("生成输出JSON失败: %v", err)
	}
	param.Value = string(output)

	return nil
}

// buildNestedSubGatewayDataForAttribute 递归构建多层子网关的嵌套属性数据结构
func buildNestedSubGatewayDataForAttribute(gateway *model.Device, subDeviceAddr string, inputData map[string]interface{}) map[string]interface{} {
	if gateway.ParentID == nil {
		// 到达顶层网关，构建最内层结构
		return map[string]interface{}{
			"sub_device_data": map[string]interface{}{
				subDeviceAddr: inputData,
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
						subDeviceAddr: inputData,
					},
				},
			},
		}
	}

	// 构建当前层级的嵌套结构
	innerData := buildNestedSubGatewayDataForAttribute(parentGateway, subDeviceAddr, inputData)

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
						subDeviceAddr: inputData,
					},
				},
			},
		}
	}
}

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
	model "project/internal/model"
	config "project/mqtt"
	"project/mqtt/publish"
	"project/pkg/common"
	"project/pkg/constant"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type AttributeData struct{}

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

func (*AttributeData) AttributePutMessage(ctx context.Context, userID string, param *model.AttributePutMessage, operationType string, fn ...config.MqttDirectResponseFunc) error {
	// 获取设备信息
	deviceInfo, err := initialize.GetDeviceCacheById(param.DeviceID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 获取设备类型和协议
	deviceType, protocolType := "1", "MQTT"
	if deviceInfo.DeviceConfigID != nil {
		deviceConfig, err := dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
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
				"system_error": "protocol_type is nil",
			})
		}
	}

	logrus.Info("protocolType:", protocolType)

	// 生成消息ID和主题
	messageID := common.GetMessageID()
	var topic string
	if deviceType == "1" {
		topic = fmt.Sprintf("%s%s/%s", config.MqttConfig.Attributes.PublishTopic, deviceInfo.DeviceNumber, messageID)
	} else {
		// 处理网关设备和子设备 - 需要递归查找顶层网关
		topGatewayInfo, err := findTopLevelGatewayForAttribute(deviceInfo, deviceType)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		topic = fmt.Sprintf(config.MqttConfig.Attributes.GatewayPublishTopic, topGatewayInfo.DeviceNumber, messageID)
	}
	// 执行数据脚本
	if deviceInfo.DeviceConfigID != nil && *deviceInfo.DeviceConfigID != "" {
		if newValue, err := GroupApp.DataScript.Exec(deviceInfo, "D", []byte(param.Value), topic); err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"system_error": err.Error(),
			})
		} else if newValue != nil {
			param.Value = string(newValue)
		}
	}

	// 构建多层网关格式的payload
	if deviceType == "3" || deviceType == "2" {
		if err := transformAttributeDataForMultiLevelGateway(param, deviceInfo, deviceType); err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"system_error": err.Error(),
			})
		}
	}
	// 发布消息
	err = publish.PublishAttributeMessage(topic, []byte(param.Value))
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
	description := "下发属性日志记录"
	logInfo := &model.AttributeSetLog{
		ID:            uuid.New(),
		DeviceID:      param.DeviceID,
		OperationType: &operationType,
		MessageID:     &messageID,
		Datum:         &(param.Value),
		Status:        &status,
		ErrorMessage:  &errorMessage,
		CreatedAt:     time.Now().UTC(),
		UserID:        &userID,
		Description:   &description,
	}
	_, err = dal.AttributeSetLogsQuery{}.Create(ctx, logInfo)
	if err != nil {
		logrus.Error(ctx, "创建日志失败", err)
	}

	// 处理响应
	config.MqttResponseFuncMap[messageID] = make(chan model.MqttResponse)
	go func() {
		select {
		case response := <-config.MqttResponseFuncMap[messageID]:
			fmt.Println("接收到数据:", response)
			if len(fn) > 0 {
				_ = fn[0](response)
			}
			dal.AttributeSetLogsQuery{}.SetAttributeResultUpdate(context.Background(), logInfo.ID, response)
			close(config.MqttResponseFuncMap[messageID])
			delete(config.MqttResponseFuncMap, messageID)
		case <-time.After(3 * time.Minute): // 设置超时时间为 3 分钟
			fmt.Println("超时，关闭通道")
			//log.SetAttributeResultUpdate(context.Background(), logInfo.ID, model.MqttResponse{
			//	Result:  1,
			//	Errcode: "timeout",
			//	Message: "设备响应超时",
			//	Ts:      time.Now().Unix(),
			//})
			close(config.MqttResponseFuncMap[messageID])
			delete(config.MqttResponseFuncMap, messageID)

			return
		}
	}()
	return err
}

// 属性对象转网关数据
func transformGatewayData(param *model.AttributePutMessage) error {
	// 解析原始JSON
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(param.Value), &inputData); err != nil {
		return fmt.Errorf("解析输入 JSON 失败: %v", err)
	}

	// 构建新的数据结构
	outputData := map[string]interface{}{
		"gateway_data": inputData,
	}

	// 将新结构转换回 JSON 字符串
	output, err := json.Marshal(outputData)
	if err != nil {
		return fmt.Errorf("生成输出 JSON 失败: %v", err)
	}

	// 更新 param.Value
	param.Value = string(output)

	return nil
}

// 子设备对象转网关数据
func transformSubDeviceData(param *model.AttributePutMessage, subDeviceAddr string) error {
	// 解析原始JSON
	var inputData map[string]interface{}
	if err := json.Unmarshal([]byte(param.Value), &inputData); err != nil {
		return fmt.Errorf("解析输入 JSON 失败: %v", err)
	}

	// 构建新的数据结构
	outputData := map[string]interface{}{
		"sub_device_data": map[string]interface{}{
			subDeviceAddr: inputData,
		},
	}

	// 将新结构转换回 JSON 字符串
	output, err := json.Marshal(outputData)
	if err != nil {
		return fmt.Errorf("生成输出 JSON 失败: %v", err)
	}

	// 更新 param.Value
	param.Value = string(output)

	return nil
}

// 发送获取属性请求
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
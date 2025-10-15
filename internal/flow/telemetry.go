package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/service"
	"project/internal/storage"
	"project/mqtt/publish"
	"project/mqtt/subscribe"

	"github.com/sirupsen/logrus"
)

// TelemetryFlow 遥测数据流处理器
type TelemetryFlow struct {
	// 依赖注入
	processor    processor.DataProcessor
	storageInput chan<- *storage.Message // 只写 channel
	logger       *logrus.Logger

	// 运行状态
	ctx    context.Context
	cancel context.CancelFunc
}

// TelemetryFlowConfig 遥测流程配置
type TelemetryFlowConfig struct {
	Processor    processor.DataProcessor
	StorageInput chan<- *storage.Message // 只写 channel
	Logger       *logrus.Logger
}

// NewTelemetryFlow 创建遥测数据流处理器
func NewTelemetryFlow(config TelemetryFlowConfig) *TelemetryFlow {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	return &TelemetryFlow{
		processor:    config.Processor,
		storageInput: config.StorageInput,
		logger:       config.Logger,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// DeviceMessage 设备消息（避免循环导入，在 flow 包内定义）
type DeviceMessage struct {
	Type      string
	DeviceID  string
	TenantID  string
	Timestamp int64
	Payload   []byte
	Metadata  map[string]interface{}
}

// GetMetadata 获取元数据
func (m *DeviceMessage) GetMetadata(key string) (interface{}, bool) {
	if m.Metadata == nil {
		return nil, false
	}
	val, ok := m.Metadata[key]
	return val, ok
}

// Start 启动遥测数据流处理
func (f *TelemetryFlow) Start(messageChan <-chan *DeviceMessage) {
	f.logger.Info("TelemetryFlow started")

	go func() {
		for {
			select {
			case msg, ok := <-messageChan:
				if !ok {
					f.logger.Info("TelemetryFlow message channel closed")
					return
				}
				f.processMessage(msg)

			case <-f.ctx.Done():
				f.logger.Info("TelemetryFlow stopped")
				return
			}
		}
	}()
}

// Stop 停止遥测数据流处理
func (f *TelemetryFlow) Stop() {
	f.cancel()
}

// processMessage 处理单条遥测消息
func (f *TelemetryFlow) processMessage(msg *DeviceMessage) {
	// 从 metadata 获取设备ID
	deviceIDObj, ok := msg.GetMetadata("device_id")
	if !ok {
		f.logger.Error("Device ID not found in message metadata")
		return
	}

	deviceID, ok := deviceIDObj.(string)
	if !ok {
		f.logger.Error("Invalid device ID type in metadata")
		return
	}

	// 从缓存获取设备信息
	device, err := initialize.GetDeviceCacheById(deviceID)
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"error":     err,
		}).Error("Failed to get device from cache")
		return
	}

	// 1. 数据脚本处理（如果配置了）
	processedPayload := msg.Payload
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		output, err := f.processor.Decode(f.ctx, &processor.DecodeInput{
			DeviceConfigID: *device.DeviceConfigID,
			Type:           processor.DataTypeTelemetry,
			RawData:        msg.Payload,
			Timestamp:      msg.Timestamp,
		})

		if err != nil {
			f.logger.WithFields(logrus.Fields{
				"device_id": device.ID,
				"error":     err,
			}).Error("Processor decode failed, terminate processing")
			return // 脚本失败直接终止
		}

		if !output.Success {
			f.logger.WithFields(logrus.Fields{
				"device_id": device.ID,
				"error":     output.Error,
			}).Error("Processor execution failed, terminate processing")
			return // 脚本失败直接终止
		}

		processedPayload = output.Data
	}

	// 2. 根据消息类型判断是否为网关消息
	// 使用 Type 字段而不是解析 Payload,支持协议无关的判断
	if msg.Type == "gateway_telemetry" {
		f.processGatewayMessage(device, processedPayload, msg)
	} else {
		// 直连设备消息
		f.processDirectDeviceMessage(device, processedPayload, msg)
	}
}

// processGatewayMessage 处理网关消息（拆分后递归处理）
func (f *TelemetryFlow) processGatewayMessage(device *model.Device, payload []byte, originalMsg *DeviceMessage) {
	var gatewayMsg model.GatewayPublish
	if err := json.Unmarshal(payload, &gatewayMsg); err != nil {
		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Failed to unmarshal gateway message")
		return
	}

	// 处理网关自身数据
	if gatewayMsg.GatewayData != nil {
		gatewayData, _ := json.Marshal(gatewayMsg.GatewayData)
		f.processDirectDeviceMessage(device, gatewayData, originalMsg)
	}

	// 处理子设备数据
	if gatewayMsg.SubDeviceData != nil {
		f.processSubDevices(device.ID, *gatewayMsg.SubDeviceData, originalMsg)
	}

	// 处理子网关数据（递归）
	if gatewayMsg.SubGatewayData != nil {
		f.processSubGateways(device.ID, *gatewayMsg.SubGatewayData, originalMsg, 1)
	}
}

// processSubDevices 处理子设备数据
// subDeviceData: map[设备地址]设备数据
func (f *TelemetryFlow) processSubDevices(parentID string, subDeviceData map[string]map[string]interface{}, originalMsg *DeviceMessage) {
	if len(subDeviceData) == 0 {
		return
	}

	// 获取所有子设备地址
	var subDeviceAddrs []string
	for addr := range subDeviceData {
		subDeviceAddrs = append(subDeviceAddrs, addr)
	}

	// 批量查询子设备信息
	subDevices, err := dal.GetDeviceBySubDeviceAddress(subDeviceAddrs, parentID)
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"parent_id": parentID,
			"error":     err,
		}).Error("Failed to get sub devices")
		return
	}

	// 处理每个子设备
	for addr, data := range subDeviceData {
		subDevice, ok := subDevices[addr]
		if !ok {
			f.logger.WithFields(logrus.Fields{
				"parent_id":   parentID,
				"device_addr": addr,
			}).Warn("Sub device not found")
			continue
		}

		subDeviceData, _ := json.Marshal(data)
		f.processDirectDeviceMessage(subDevice, subDeviceData, originalMsg)
	}
}

// processSubGateways 处理子网关数据（递归，最多5层）
func (f *TelemetryFlow) processSubGateways(parentID string, subGatewayData map[string]*model.GatewayPublish, originalMsg *DeviceMessage, depth int) {
	if depth > 5 {
		f.logger.Warn("Maximum gateway depth (5) exceeded")
		return
	}

	if len(subGatewayData) == 0 {
		return
	}

	// 获取所有子网关地址
	var subGatewayAddrs []string
	for addr := range subGatewayData {
		subGatewayAddrs = append(subGatewayAddrs, addr)
	}

	// 批量查询子网关信息
	subGateways, err := dal.GetDeviceBySubDeviceAddress(subGatewayAddrs, parentID)
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"parent_id": parentID,
			"error":     err,
		}).Error("Failed to get sub gateways")
		return
	}

	// 处理每个子网关
	for addr, gatewayMsg := range subGatewayData {
		subGateway, ok := subGateways[addr]
		if !ok {
			f.logger.WithFields(logrus.Fields{
				"parent_id":    parentID,
				"gateway_addr": addr,
			}).Warn("Sub gateway not found")
			continue
		}

		// 处理子网关自身数据
		if gatewayMsg.GatewayData != nil {
			gatewayData, _ := json.Marshal(gatewayMsg.GatewayData)
			f.processDirectDeviceMessage(subGateway, gatewayData, originalMsg)
		}

		// 处理子网关的子设备
		if gatewayMsg.SubDeviceData != nil {
			f.processSubDevices(subGateway.ID, *gatewayMsg.SubDeviceData, originalMsg)
		}

		// 递归处理更深层的子网关
		if gatewayMsg.SubGatewayData != nil {
			f.processSubGateways(subGateway.ID, *gatewayMsg.SubGatewayData, originalMsg, depth+1)
		}
	}
}

// processDirectDeviceMessage 处理单个设备的遥测数据
func (f *TelemetryFlow) processDirectDeviceMessage(device *model.Device, payload []byte, originalMsg *DeviceMessage) {
	// 1. 数据转发（同步执行）
	if err := publish.ForwardTelemetryMessage(device.ID, payload); err != nil {
		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Telemetry forward failed")
		// 转发失败不影响后续流程
	}

	// 2. 心跳处理（异步）
	go subscribe.HeartbeatDeal(device)

	// 3. 数据转换（map → []TelemetryDataPoint）
	telemetryPoints, triggerParam, triggerValues, err := f.convertToTelemetryPoints(payload, device)
	if err != nil {
		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Failed to convert telemetry data")
		return
	}

	// 4. 发送到 Storage（同步发送到 channel）
	f.storageInput <- &storage.Message{
		DeviceID:  device.ID,
		TenantID:  device.TenantID,
		DataType:  storage.DataTypeTelemetry,
		Timestamp: time.Now().UnixMilli(),
		Data:      telemetryPoints,
	}

	// 5. 场景联动（异步）
	go func() {
		err := service.GroupApp.Execute(device, service.AutomateFromExt{
			TriggerParamType: model.TRIGGER_PARAM_TYPE_TEL,
			TriggerParam:     triggerParam,
			TriggerValues:    triggerValues,
		})
		if err != nil {
			f.logger.WithFields(logrus.Fields{
				"device_id": device.ID,
				"error":     err,
			}).Error("Automation execute failed")
		}
	}()
}

// convertToTelemetryPoints 将 JSON 数据转换为 TelemetryDataPoint 列表
// 返回: (telemetryPoints, triggerParam, triggerValues, error)
func (f *TelemetryFlow) convertToTelemetryPoints(payload []byte, device *model.Device) ([]storage.TelemetryDataPoint, []string, map[string]interface{}, error) {
	// 解析 JSON
	var dataMap map[string]interface{}
	if err := json.Unmarshal(payload, &dataMap); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// 转换为 TelemetryDataPoint
	var points []storage.TelemetryDataPoint
	var triggerParam []string
	triggerValues := make(map[string]interface{})

	for key, value := range dataMap {
		points = append(points, storage.TelemetryDataPoint{
			Key:   key,
			Value: value,
		})

		triggerParam = append(triggerParam, key)
		triggerValues[key] = value
	}

	return points, triggerParam, triggerValues, nil
}

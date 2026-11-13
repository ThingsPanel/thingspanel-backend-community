package uplink

import (
	"context"
	"encoding/json"
	"fmt"
	"project/initialize"
	"time"

	"gorm.io/gorm"
	"project/internal/dal"
	"project/internal/diagnostics"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/service"
	"project/internal/storage"
	"project/internal/uplinkbinding"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
)

// AttributeUplink 属性数据流处理器
type AttributeUplink struct {
	db        *gorm.DB
	contracts uplinkbinding.ContractReader
	// 依赖注入
	processor        processor.DataProcessor
	storageInput     chan<- *storage.Message // Storage输入channel
	heartbeatService *service.HeartbeatService
	logger           *logrus.Logger

	// 运行状态
	ctx    context.Context
	cancel context.CancelFunc
}

// AttributeUplinkConfig 属性流程配置
type AttributeUplinkConfig struct {
	DB               *gorm.DB
	Contracts        uplinkbinding.ContractReader
	Processor        processor.DataProcessor
	StorageInput     chan<- *storage.Message
	HeartbeatService *service.HeartbeatService
	Logger           *logrus.Logger
}

// NewAttributeUplink 创建属性数据流处理器
func NewAttributeUplink(config AttributeUplinkConfig) *AttributeUplink {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	if config.DB == nil {
		config.DB = global.DB
	}
	return &AttributeUplink{
		db: config.DB, contracts: config.Contracts,
		processor:        config.Processor,
		storageInput:     config.StorageInput,
		heartbeatService: config.HeartbeatService,
		logger:           config.Logger,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start 启动属性数据流处理
func (f *AttributeUplink) Start(messageChan <-chan *DeviceMessage) {
	f.logger.Info("AttributeUplink started")

	go func() {
		for {
			select {
			case msg, ok := <-messageChan:
				if !ok {
					f.logger.Info("AttributeUplink message channel closed")
					return
				}
				f.processMessage(msg)

			case <-f.ctx.Done():
				f.logger.Info("AttributeUplink stopped")
				return
			}
		}
	}()
}

// Stop 停止属性数据流处理
func (f *AttributeUplink) Stop() {
	f.cancel()
}

// processMessage 处理单条属性消息
func (f *AttributeUplink) processMessage(msg *DeviceMessage) {
	batch, err := prepareBusMessage(f.ctx, f.db, f.contracts, f.processor, msg, f.logger)
	if err != nil {

		f.logger.WithError(err).Warn("uplink validation rejected")
		return
	}
	for _, entry := range batch.entries {
		prepared := preparedBusMessage(msg, batch, entry.device.ID)
		f.processDirectDeviceMessage(&entry.device, entry.payload, prepared)
	}
}

// processGatewayMessage 处理网关消息（拆分后递归处理）
func (f *AttributeUplink) processGatewayMessage(device *model.Device, payload []byte, originalMsg *DeviceMessage) {
	var gatewayMsg model.GatewayPublish
	if err := json.Unmarshal(payload, &gatewayMsg); err != nil {
		// 记录诊断：网关消息格式错误
		diagnostics.GetInstance().RecordUplinkFailed(device.ID, diagnostics.StageProcessor, fmt.Sprintf("网关消息格式错误：%v", err))
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
func (f *AttributeUplink) processSubDevices(parentID string, subDeviceData map[string]map[string]interface{}, originalMsg *DeviceMessage) {
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
func (f *AttributeUplink) processSubGateways(parentID string, subGatewayData map[string]*model.GatewayPublish, originalMsg *DeviceMessage, depth int) {
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

// processDirectDeviceMessage 处理单个设备的属性数据
func (f *AttributeUplink) processDirectDeviceMessage(device *model.Device, payload []byte, originalMsg *DeviceMessage) {
	// 1. 心跳刷新(最优先,确保设备活跃性)
	f.refreshHeartbeat(device)

	// 2. 解析数据
	var dataMap map[string]interface{}
	if err := json.Unmarshal(payload, &dataMap); err != nil {
		// JSON 解析失败，包装成 {"_raw": 原始值}
		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"payload":   string(payload),
			"error":     err,
		}).Warn("【属性上行】payload is not valid JSON object, wrapping as {\"_raw\": ...}")

		// 尝试将 payload 作为 JSON 值解析（可能是字符串、数字、布尔等）
		var rawValue interface{}
		if jsonErr := json.Unmarshal(payload, &rawValue); jsonErr != nil {
			// 如果连 JSON 值都不是，直接作为字符串包装
			rawValue = string(payload)
		}

		// 构造包装后的数据
		dataMap = map[string]interface{}{
			"_raw": rawValue,
		}
	}

	// 3. 转换为 AttributeDataPoint 列表
	var points []storage.AttributeDataPoint
	var triggerParam []string
	triggerValues := make(map[string]interface{})

	for key, value := range dataMap {
		points = append(points, storage.AttributeDataPoint{
			Key:   key,
			Value: value,
		})

		triggerParam = append(triggerParam, key)
		triggerValues[key] = value
	}

	// 4. 发送到 Storage（通过channel）
	stored := &storage.Message{
		Bindings:    originalMsg.preparedBindings,
		Observation: &diagnostics.LogicalMessage{TenantID: device.TenantID, DeviceID: device.ID, ID: originalMsg.observationID, Family: "attribute"},
		DeviceID:    device.ID,
		TenantID:    device.TenantID,
		DataType:    storage.DataTypeAttribute,
		Timestamp:   time.Now().UnixMilli(),
		Data:        points,
	}

	// 5. 场景联动（异步）
	stored.AfterCommit = func() {
		go func() {
			err := DispatchAutomation(device, service.AutomateFromExt{
				TriggerParamType: model.TRIGGER_PARAM_TYPE_ATTR,
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
	select {
	case f.storageInput <- stored:
	case <-f.ctx.Done():
		return
	}

}

// refreshHeartbeat 刷新设备心跳
func (f *AttributeUplink) refreshHeartbeat(device *model.Device) {
	// 如果没有 HeartbeatService,跳过
	if f.heartbeatService == nil {
		return
	}

	// 获取心跳配置
	config, err := f.heartbeatService.GetConfig(device)
	if err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Debug("Failed to get heartbeat config")
		return
	}

	// 无心跳配置,不处理
	if config == nil {
		return
	}

	// 检查是否需要自动上线
	if device.IsOnline != 1 {
		// 设备当前离线,收到消息后自动上线
		statusChanged, err := dal.UpdateDeviceStatus(device.ID, 1)
		if err != nil {
			f.logger.WithError(err).WithField("device_id", device.ID).Error("Failed to auto online device")
			return
		}

		// 只有当状态真的从离线变为在线时，才执行上线通知流程
		if statusChanged {
			f.logger.WithField("device_id", device.ID).Info("Device auto online by business message")

			// 清理缓存
			initialize.DelDeviceCache(device.ID)

			// 获取最新设备信息
			updatedDevice, err := initialize.GetDeviceCacheById(device.ID)
			if err != nil {
				f.logger.WithError(err).Error("Failed to get updated device")
				return
			}

			// SSE通知、自动化、预期数据(异步)
			go f.notifyDeviceOnline(updatedDevice)
		}
	}

	// 刷新心跳 key (优先级: heartbeat > online_timeout)
	if err := f.heartbeatService.RefreshHeartbeat(device, config); err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Error("Failed to refresh heartbeat")
	}
}

// notifyDeviceOnline 通知设备上线(SSE + 自动化 + 预期数据)
func (f *AttributeUplink) notifyDeviceOnline(device *model.Device) {
	// SSE通知
	var deviceName string
	if device.Name != nil {
		deviceName = *device.Name
	} else {
		deviceName = device.DeviceNumber
	}

	messageData := map[string]interface{}{
		"device_id":   device.DeviceNumber,
		"device_name": deviceName,
		"is_online":   true,
	}

	jsonBytes, _ := json.Marshal(messageData)
	sseEvent := global.SSEEvent{
		Type:     "device_online",
		TenantID: device.TenantID,
		Message:  string(jsonBytes),
	}
	global.TPSSEManager.BroadcastEventToTenant(device.TenantID, sseEvent)

	// 触发自动化
	err := DispatchAutomation(device, service.AutomateFromExt{
		TriggerParamType: model.TRIGGER_PARAM_TYPE_STATUS,
		TriggerParam:     []string{},
		TriggerValues: map[string]interface{}{
			"login": "ON-LINE",
		},
	})
	if err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Warn("Automation execution failed")
	}

	// 发送预期数据(延迟3秒)
	time.Sleep(3 * time.Second)
	err = service.GroupApp.ExpectedData.Send(context.Background(), device.ID)
	if err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Debug("Failed to send expected data")
	}
}

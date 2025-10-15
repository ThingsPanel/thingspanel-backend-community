package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/global"
)

// StatusFlow 设备状态流处理器
type StatusFlow struct {
	// 依赖注入
	heartbeatService *service.HeartbeatService
	logger           *logrus.Logger

	// 运行状态
	ctx    context.Context
	cancel context.CancelFunc
}

// StatusFlowConfig 状态流程配置
type StatusFlowConfig struct {
	HeartbeatService *service.HeartbeatService
	Logger           *logrus.Logger
}

// NewStatusFlow 创建状态流处理器
func NewStatusFlow(config StatusFlowConfig) *StatusFlow {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	return &StatusFlow{
		heartbeatService: config.HeartbeatService,
		logger:           config.Logger,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start 启动状态流处理
func (f *StatusFlow) Start(input <-chan *DeviceMessage) error {
	f.logger.Info("🚀 StatusFlow starting...")

	go func() {
		f.logger.Info("✅ StatusFlow message loop started")
		for {
			select {
			case <-f.ctx.Done():
				f.logger.Info("StatusFlow stopped")
				return
			case msg := <-input:
				if msg == nil {
					f.logger.Warn("Received nil message, skipping")
					continue
				}
				f.logger.WithField("device_id", msg.DeviceID).Info("📨 StatusFlow received message from channel")
				f.processMessage(msg)
			}
		}
	}()

	f.logger.Info("✅ StatusFlow started successfully")
	return nil
}

// Stop 停止状态流处理
func (f *StatusFlow) Stop() error {
	f.cancel()
	return nil
}

// processMessage 处理状态消息
func (f *StatusFlow) processMessage(msg *DeviceMessage) {
	f.logger.WithFields(logrus.Fields{
		"device_id": msg.DeviceID,
		"payload":   string(msg.Payload),
		"metadata":  msg.Metadata,
	}).Info("🟢 StatusFlow: processMessage called")

	// 1. 解析状态 (0=离线, 1=在线)
	status, err := f.parseStatus(msg.Payload)
	if err != nil {
		f.logger.WithError(err).WithFields(logrus.Fields{
			"device_id": msg.DeviceID,
			"payload":   string(msg.Payload),
		}).Error("Invalid status value")
		return
	}

	f.logger.WithFields(logrus.Fields{
		"device_id": msg.DeviceID,
		"status":    status,
	}).Info("📊 Parsed status")

	// 2. 获取设备信息
	device, err := initialize.GetDeviceCacheById(msg.DeviceID)
	if err != nil {
		f.logger.WithError(err).WithField("device_id", msg.DeviceID).Error("Device not found")
		return
	}

	f.logger.WithField("device_id", device.ID).Info("✅ Device found")

	// 3. 检查心跳配置
	config, err := f.heartbeatService.GetConfig(device)

	// 3.1 心跳模式: 只处理来自 HeartbeatMonitor 的离线消息
	if err == nil && config != nil && config.Heartbeat > 0 {
		source, _ := msg.Metadata["source"].(string)

		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"heartbeat": config.Heartbeat,
			"source":    source,
			"status":    status,
		}).Debug("Device in heartbeat mode")

		// 只有来自 HeartbeatMonitor 的心跳过期消息才处理
		if source != "heartbeat_expired" {
			f.logger.Debug("Ignoring status message from device (heartbeat mode)")
			return
		}
	}

	// 3.2 超时模式: 处理状态消息,并设置/保留TTL
	if err == nil && config != nil && config.OnlineTimeout > 0 {
		// 上线时设置TTL
		if status == 1 {
			if err := f.heartbeatService.SetTimeout(device.ID, config.OnlineTimeout); err != nil {
				f.logger.WithError(err).Error("Failed to set timeout key")
			}
		}
		// 离线时保留TTL key(不删除),等待自然过期或业务消息刷新
	}

	// 4. 更新数据库状态
	if err := dal.UpdateDeviceStatus(device.ID, status); err != nil {
		f.logger.WithError(err).WithFields(logrus.Fields{
			"device_id": device.ID,
			"status":    status,
		}).Error("Failed to update device status")
		return
	}

	f.logger.WithFields(logrus.Fields{
		"device_id": device.ID,
		"status":    status,
		"source":    msg.Metadata["source"],
	}).Info("Device status updated")

	// 5. 清理设备缓存
	initialize.DelDeviceCache(device.ID)

	// 6. SSE 通知客户端
	go f.notifyClients(device, status)

	// 7. 触发自动化
	go f.triggerAutomation(device, status)

	// 8. 预期数据发送(上线时)
	if status == 1 {
		go f.sendExpectedData(device)
	}
}

// parseStatus 解析状态值
func (f *StatusFlow) parseStatus(payload []byte) (int16, error) {
	str := string(payload)
	switch str {
	case "0":
		return 0, nil
	case "1":
		return 1, nil
	default:
		return 0, fmt.Errorf("invalid status value: %s (expected 0 or 1)", str)
	}
}

// notifyClients SSE通知客户端设备状态变更
func (f *StatusFlow) notifyClients(device *model.Device, status int16) {
	// 构造设备名称
	var deviceName string
	if device.Name != nil {
		deviceName = *device.Name
	} else {
		deviceName = device.DeviceNumber
	}

	// 构造SSE消息
	var messageData map[string]interface{}
	if status == 1 {
		messageData = map[string]interface{}{
			"device_id":   device.DeviceNumber,
			"device_name": deviceName,
			"is_online":   true,
		}
	} else {
		messageData = map[string]interface{}{
			"device_id":   device.DeviceNumber,
			"device_name": deviceName,
			"is_online":   false,
		}
	}

	jsonBytes, err := json.Marshal(messageData)
	if err != nil {
		f.logger.WithError(err).Error("Failed to marshal SSE message")
		return
	}

	sseEvent := global.SSEEvent{
		Type:     "device_online",
		TenantID: device.TenantID,
		Message:  string(jsonBytes),
	}

	// 发送到SSE
	global.TPSSEManager.BroadcastEventToTenant(device.TenantID, sseEvent)

	f.logger.WithFields(logrus.Fields{
		"device_id": device.ID,
		"tenant_id": device.TenantID,
		"status":    status,
	}).Debug("SSE notification sent")
}

// triggerAutomation 触发自动化场景
func (f *StatusFlow) triggerAutomation(device *model.Device, status int16) {
	// 设备状态变更触发自动化
	var loginStatus string
	if status == 1 {
		loginStatus = "ON-LINE"
	} else {
		loginStatus = "OFF-LINE"
	}

	err := service.GroupApp.Execute(device, service.AutomateFromExt{
		TriggerParamType: model.TRIGGER_PARAM_TYPE_STATUS,
		TriggerParam:     []string{},
		TriggerValues: map[string]interface{}{
			"login": loginStatus,
		},
	})

	if err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Warn("Automation execution failed")
	} else {
		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"status":    loginStatus,
		}).Debug("Automation triggered")
	}
}

// sendExpectedData 发送预期数据
func (f *StatusFlow) sendExpectedData(device *model.Device) {
	// 延迟3秒发送预期数据(与原有逻辑保持一致)
	time.Sleep(3 * time.Second)

	err := service.GroupApp.ExpectedData.Send(context.Background(), device.ID)
	if err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Debug("Failed to send expected data")
	} else {
		f.logger.WithField("device_id", device.ID).Debug("Expected data sent")
	}
}

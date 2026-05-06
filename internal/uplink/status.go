package uplink

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

// StatusUplink 设备状态流处理器
type StatusUplink struct {
	// 依赖注入
	heartbeatService *service.HeartbeatService
	logger           *logrus.Logger

	// 运行状态
	ctx    context.Context
	cancel context.CancelFunc
}

// StatusUplinkConfig 状态流程配置
type StatusUplinkConfig struct {
	HeartbeatService *service.HeartbeatService
	Logger           *logrus.Logger
}

// NewStatusUplink 创建状态流处理器
func NewStatusUplink(config StatusUplinkConfig) *StatusUplink {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	return &StatusUplink{
		heartbeatService: config.HeartbeatService,
		logger:           config.Logger,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start 启动状态流处理
func (f *StatusUplink) Start(input <-chan *DeviceMessage) error {
	f.logger.Info("🚀 StatusUplink starting...")

	go func() {
		f.logger.Info("✅ StatusUplink message loop started")
		for {
			select {
			case <-f.ctx.Done():
				f.logger.Info("StatusUplink stopped")
				return
			case msg := <-input:
				if msg == nil {
					f.logger.Warn("Received nil message, skipping")
					continue
				}
				f.logger.WithField("device_id", msg.DeviceID).Debug("【设备上下线】StatusUplink received message from channel")
				f.processMessage(msg)
			}
		}
	}()

	f.logger.Info("✅ StatusUplink started successfully")
	return nil
}

// Stop 停止状态流处理
func (f *StatusUplink) Stop() error {
	f.cancel()
	return nil
}

// processMessage 处理状态消息
func (f *StatusUplink) processMessage(msg *DeviceMessage) {
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
	}).Debug("【设备上下线】Parsed status")

	// 2. 获取设备信息
	device, err := initialize.GetDeviceCacheById(msg.DeviceID)
	if err != nil {
		f.logger.WithError(err).WithField("device_id", msg.DeviceID).Error("Device not found")
		return
	}

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
	statusChanged, err := dal.UpdateDeviceStatus(device.ID, status)
	if err != nil {
		f.logger.WithError(err).WithFields(logrus.Fields{
			"device_id": device.ID,
			"status":    status,
		}).Error("Failed to update device status")
		return
	}

	// 如果状态没有变化，直接返回（避免重复通知）
	if !statusChanged {
		f.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"status":    status,
			"source":    msg.Metadata["source"],
		}).Debug("【设备上下线】Device status unchanged, skipping notification")
		return
	}

	f.logger.WithFields(logrus.Fields{
		"device_id": device.ID,
		"status":    status,
		"source":    msg.Metadata["source"],
	}).Debug("【设备上下线】Device status updated")

	// 5. 清理设备缓存
	initialize.DelDeviceCache(device.ID)

	// 6. 发布到 Redis Pub/Sub (供 WebSocket 订阅)
	go f.publishToRedis(device, status, msg.Metadata)

	// 7. SSE 通知客户端
	go f.notifyClients(device, status)

	// 8. 触发自动化
	go f.triggerAutomation(device, status)

	// 9. 预期数据发送(上线时)
	if status == 1 {
		go f.sendExpectedData(device)
	}
}

// parseStatus 解析状态值
func (f *StatusUplink) parseStatus(payload []byte) (int16, error) {
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
func (f *StatusUplink) notifyClients(device *model.Device, status int16) {
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
	logrus.Infof("准备发送SSE事件: %v", sseEvent)
	global.TPSSEManager.BroadcastEventToTenant(device.TenantID, sseEvent)
}

// triggerAutomation 触发自动化场景
func (f *StatusUplink) triggerAutomation(device *model.Device, status int16) {
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
		}).Debug("【自动化】Automation triggered")
	}
}

// sendExpectedData 发送预期数据
func (f *StatusUplink) sendExpectedData(device *model.Device) {
	// 延迟3秒发送预期数据(与原有逻辑保持一致)
	time.Sleep(3 * time.Second)

	err := service.GroupApp.ExpectedData.Send(context.Background(), device.ID)
	if err != nil {
		f.logger.WithError(err).WithField("device_id", device.ID).Debug("Failed to send expected data")
	} else {
		f.logger.WithField("device_id", device.ID).Debug("【期望消息】Expected data sent")
	}
}

// publishToRedis 发布设备状态到 Redis Pub/Sub (供 WebSocket 订阅)
func (f *StatusUplink) publishToRedis(device *model.Device, status int16, metadata map[string]interface{}) {
	// 构造设备名称
	var deviceName string
	if device.Name != nil {
		deviceName = *device.Name
	} else {
		deviceName = device.DeviceNumber
	}

	// 获取来源信息
	source, _ := metadata["source"].(string)
	if source == "" {
		source = "unknown"
	}

	// 构造消息 (保持原有 WebSocket 接口格式: is_online 为整数 0/1)
	messageData := map[string]interface{}{
		"is_online": int(status), // 0 或 1
	}

	jsonBytes, err := json.Marshal(messageData)
	if err != nil {
		f.logger.WithError(err).Error("Failed to marshal Redis message")
		return
	}

	// 发布到设备专属通道: device:{device_id}:status
	channel := fmt.Sprintf("device:%s:status", device.ID)
	if err := global.REDIS.Publish(f.ctx, channel, string(jsonBytes)).Err(); err != nil {
		f.logger.WithError(err).WithFields(logrus.Fields{
			"device_id": device.ID,
		}).Debug("Status publish failed")
		return
	}

	f.logger.WithFields(logrus.Fields{
		"device_id":   device.ID,
		"device_name": deviceName,
		"channel":     channel,
		"status":      status,
		"source":      source,
	}).Debug("【设备上下线】Status published to Redis")
}

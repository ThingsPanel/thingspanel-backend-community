package uplink

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"project/initialize"
	"project/internal/brokerstate"
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
	brokerState      *brokerstate.Store
	brokerProjection bool

	// 运行状态
	ctx         context.Context
	cancel      context.CancelFunc
	lifecycleMu sync.Mutex
	done        chan struct{}
}

// StatusUplinkConfig 状态流程配置
type StatusUplinkConfig struct {
	HeartbeatService *service.HeartbeatService
	Logger           *logrus.Logger
	BrokerStateStore *brokerstate.Store
	InputProfile     string
}

// NewStatusUplink 创建状态流处理器
func NewStatusUplink(config StatusUplinkConfig) *StatusUplink {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}
	if config.InputProfile == "" {
		config.InputProfile = viper.GetString("mqtt.input_profile")
	}
	if config.BrokerStateStore == nil && config.InputProfile == "ingress_v1" && global.DB != nil {
		config.BrokerStateStore = brokerstate.New(global.DB)
	}

	return &StatusUplink{
		heartbeatService: config.HeartbeatService,
		logger:           config.Logger,
		brokerState:      config.BrokerStateStore,
		brokerProjection: config.InputProfile == "ingress_v1" && config.BrokerStateStore != nil,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start 启动状态流处理
func (f *StatusUplink) Start(input <-chan *DeviceMessage) error {
	f.lifecycleMu.Lock()
	if err := f.ctx.Err(); err != nil {
		f.lifecycleMu.Unlock()
		return err
	}
	if f.done != nil {
		f.lifecycleMu.Unlock()
		return nil
	}
	done := make(chan struct{})
	f.done = done
	f.lifecycleMu.Unlock()
	f.logger.Info("🚀 StatusUplink starting...")
	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if f.brokerProjection {
		ticker = time.NewTicker(time.Second)
		tickerC = ticker.C
	}

	go func() {
		defer close(done)
		if ticker != nil {
			defer ticker.Stop()
		}
		f.logger.Info("✅ StatusUplink message loop started")
		for {
			select {
			case <-f.ctx.Done():
				f.logger.Info("StatusUplink stopped")
				return
			case <-tickerC:
				f.retryBrokerProjections()
			case msg, ok := <-input:
				if !ok {
					f.logger.Info("StatusUplink input closed")
					return
				}
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
	f.lifecycleMu.Lock()
	f.cancel()
	done := f.done
	f.lifecycleMu.Unlock()
	if done != nil {
		<-done
	}
	return nil
}

// processMessage 处理状态消息
func (f *StatusUplink) processMessage(msg *DeviceMessage) {
	if f.brokerProjection && msg != nil && sourceOf(msg) == "broker_connection" {
		f.processBrokerProjection(msg)
		return
	}
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

	if f.brokerProjection && f.brokerState != nil {
		owned, lookupErr := f.brokerState.OwnsDevice(f.ctx, device.TenantID, device.ID)
		if lookupErr != nil {
			f.logger.WithError(lookupErr).Warn("broker state ownership unavailable; legacy status write paused")
			return
		}
		if owned {
			f.processBrokerProjection(&DeviceMessage{TenantID: device.TenantID, DeviceID: device.ID, Metadata: map[string]interface{}{"source": "broker_connection"}})
			return
		}
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
			"source":    sourceOf(msg),
		}).Debug("【设备上下线】Device status unchanged, skipping notification")
		return
	}
	f.afterStatusChange(device, status, msg.Metadata)
}

func (f *StatusUplink) afterStatusChange(device *model.Device, status int16, metadata map[string]interface{}) {
	if device == nil {
		return
	}

	f.logger.WithFields(logrus.Fields{
		"device_id": device.ID,
		"status":    status,
		"source":    metadata["source"],
	}).Debug("【设备上下线】Device status updated")

	// 5. 清理设备缓存
	initialize.DelDeviceCache(device.ID)

	// 6. 发布到 Redis Pub/Sub (供 WebSocket 订阅)
	go f.publishToRedis(device, status, metadata)

	// 7. SSE 通知客户端
	go f.notifyClients(device, status)

	// 8. 触发自动化
	if !f.brokerProjection || metadata["source"] != "broker_connection" {
		go f.triggerAutomation(device, status)
	}

	// 9. 预期数据发送(上线时)
	if status == 1 && (!f.brokerProjection || metadata["source"] != "broker_connection") {
		go f.sendExpectedData(device)
	}
}

func sourceOf(msg *DeviceMessage) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	source, _ := msg.Metadata["source"].(string)
	return source
}

func (f *StatusUplink) processBrokerProjection(msg *DeviceMessage) {
	if f.brokerState == nil || msg.TenantID == "" || msg.DeviceID == "" {
		f.logger.Error("broker state projection missing store or scope")
		return
	}
	ctx := f.ctx
	projection, err := f.brokerState.ProjectCurrent(ctx, msg.TenantID, msg.DeviceID)
	if err != nil {
		f.deferBrokerProjection(msg, err, "broker state projection failed")
		return
	}
	if !projection.Changed {
		return
	}
	status := int16(0)
	if projection.Status == "online" {
		status = 1
	}
	f.afterStatusChange(&projection.Device, status, msg.Metadata)
}

func (f *StatusUplink) deferBrokerProjection(msg *DeviceMessage, cause error, message string) {
	fields := logrus.Fields{"tenant_id": msg.TenantID, "device_id": msg.DeviceID}
	if err := f.brokerState.DeferCurrentProjection(f.ctx, msg.TenantID, msg.DeviceID); err != nil {
		f.logger.WithError(err).WithFields(fields).Warn(message + "; retry key remains in place")
		return
	}
	f.logger.WithError(cause).WithFields(fields).Warn(message + "; retry key deferred")
}

func (f *StatusUplink) retryBrokerProjections() {
	if !f.brokerProjection || f.brokerState == nil || f.ctx.Err() != nil {
		return
	}
	keys, err := f.brokerState.PendingCurrent(f.ctx, 256)
	if err != nil {
		f.logger.WithError(err).Error("failed to read broker state projection queue")
		return
	}
	for _, key := range keys {
		if f.ctx.Err() != nil {
			return
		}
		f.processMessage(&DeviceMessage{Type: MessageTypeStatus, TenantID: key.TenantID, DeviceID: key.DeviceID, Metadata: map[string]interface{}{"source": "broker_connection"}})
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
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-f.ctx.Done():
		return
	case <-timer.C:
	}

	err := service.GroupApp.ExpectedData.Send(f.ctx, device.ID)
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

package uplink

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// UplinkManager uplink 流程管理器
// 负责启动和停止所有 Flow 处理器
type UplinkManager struct {
	bus             *Bus
	telemetryUplink *TelemetryUplink
	attributeUplink *AttributeUplink
	eventUplink     *EventUplink
	statusUplink    *StatusUplink
	responseUplink  *ResponseUplink // ✨ 新增

	logger *logrus.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// UplinkManagerConfig UplinkManager 配置
type UplinkManagerConfig struct {
	Bus             *Bus
	TelemetryUplink *TelemetryUplink
	AttributeUplink *AttributeUplink
	EventUplink     *EventUplink
	StatusUplink    *StatusUplink
	ResponseUplink  *ResponseUplink // ✨ 新增
	Logger          *logrus.Logger
}

// NewUplinkManager 创建 Flow 管理器
func NewUplinkManager(config UplinkManagerConfig) *UplinkManager {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	return &UplinkManager{
		bus:             config.Bus,
		telemetryUplink: config.TelemetryUplink,
		attributeUplink: config.AttributeUplink,
		eventUplink:     config.EventUplink,
		statusUplink:    config.StatusUplink,
		responseUplink:  config.ResponseUplink, // ✨ 新增
		logger:          config.Logger,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start 启动所有 Flow
func (m *UplinkManager) Start() error {
	m.logger.Info("UplinkManager starting...")

	// 启动 TelemetryUplink
	if m.telemetryUplink != nil {
		telemetryChan := m.bus.SubscribeTelemetry()
		m.telemetryUplink.Start(telemetryChan)
		m.logger.Info("TelemetryUplink started")
	}

	// 启动 AttributeUplink
	if m.attributeUplink != nil {
		attributeChan := m.bus.SubscribeAttribute()
		m.attributeUplink.Start(attributeChan)
		m.logger.Info("AttributeUplink started")
	}

	// 启动 EventUplink
	if m.eventUplink != nil {
		eventChan := m.bus.SubscribeEvent()
		m.eventUplink.Start(eventChan)
		m.logger.Info("EventUplink started")
	}

	// 启动 StatusUplink
	if m.statusUplink != nil {
		statusChan := m.bus.SubscribeStatus()
		if err := m.statusUplink.Start(statusChan); err != nil {
			m.logger.WithError(err).Error("Failed to start StatusUplink")
			return err
		}
		m.logger.Info("StatusUplink started")
	}

	// ✨ 启动 ResponseUplink
	if m.responseUplink != nil {
		responseChan := m.bus.SubscribeResponse()
		if err := m.responseUplink.Start(responseChan); err != nil {
			m.logger.WithError(err).Error("Failed to start ResponseUplink")
			return err
		}
		m.logger.Info("ResponseUplink started")
	}

	m.logger.Info("UplinkManager started successfully")
	return nil
}

// Stop 停止所有 Flow
func (m *UplinkManager) Stop(timeout time.Duration) error {
	m.logger.Info("UplinkManager stopping...")

	// 创建超时 context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 停止所有 Flow
	if m.telemetryUplink != nil {
		m.telemetryUplink.Stop()
	}
	if m.attributeUplink != nil {
		m.attributeUplink.Stop()
	}
	if m.eventUplink != nil {
		m.eventUplink.Stop()
	}
	if m.responseUplink != nil {
		m.responseUplink.Stop()
	}

	// TODO: 停止其他 Flow

	// 关闭 Bus
	m.bus.Close()

	// 等待停止完成或超时
	select {
	case <-ctx.Done():
		m.logger.Warn("UplinkManager stop timeout")
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		// 给一点时间让 goroutine 清理
		m.logger.Info("UplinkManager stopped successfully")
		return nil
	}
}

// GetBusStats 获取 Bus 统计信息
func (m *UplinkManager) GetBusStats() map[string]interface{} {
	return m.bus.GetChannelStats()
}

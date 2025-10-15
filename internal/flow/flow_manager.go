package flow

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// FlowManager Flow 流程管理器
// 负责启动和停止所有 Flow 处理器
type FlowManager struct {
	bus           *Bus
	telemetryFlow *TelemetryFlow
	attributeFlow *AttributeFlow
	eventFlow     *EventFlow
	statusFlow    *StatusFlow
	// TODO: 后续添加其他 Flow
	// commandFlow *CommandFlow

	logger *logrus.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// FlowManagerConfig FlowManager 配置
type FlowManagerConfig struct {
	Bus           *Bus
	TelemetryFlow *TelemetryFlow
	AttributeFlow *AttributeFlow
	EventFlow     *EventFlow
	StatusFlow    *StatusFlow
	Logger        *logrus.Logger
}

// NewFlowManager 创建 Flow 管理器
func NewFlowManager(config FlowManagerConfig) *FlowManager {
	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = logrus.StandardLogger()
	}

	return &FlowManager{
		bus:           config.Bus,
		telemetryFlow: config.TelemetryFlow,
		attributeFlow: config.AttributeFlow,
		eventFlow:     config.EventFlow,
		statusFlow:    config.StatusFlow,
		logger:        config.Logger,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start 启动所有 Flow
func (m *FlowManager) Start() error {
	m.logger.Info("FlowManager starting...")

	// 启动 TelemetryFlow
	if m.telemetryFlow != nil {
		telemetryChan := m.bus.SubscribeTelemetry()
		m.telemetryFlow.Start(telemetryChan)
		m.logger.Info("TelemetryFlow started")
	}

	// 启动 AttributeFlow
	if m.attributeFlow != nil {
		attributeChan := m.bus.SubscribeAttribute()
		m.attributeFlow.Start(attributeChan)
		m.logger.Info("AttributeFlow started")
	}

	// 启动 EventFlow
	if m.eventFlow != nil {
		eventChan := m.bus.SubscribeEvent()
		m.eventFlow.Start(eventChan)
		m.logger.Info("EventFlow started")
	}

	// 启动 StatusFlow
	if m.statusFlow != nil {
		statusChan := m.bus.SubscribeStatus()
		if err := m.statusFlow.Start(statusChan); err != nil {
			m.logger.WithError(err).Error("Failed to start StatusFlow")
			return err
		}
		m.logger.Info("StatusFlow started")
	}

	// TODO: 启动其他 Flow
	// if m.commandFlow != nil {
	//     commandChan := m.bus.SubscribeCommand()
	//     m.commandFlow.Start(commandChan)
	// }

	m.logger.Info("FlowManager started successfully")
	return nil
}

// Stop 停止所有 Flow
func (m *FlowManager) Stop(timeout time.Duration) error {
	m.logger.Info("FlowManager stopping...")

	// 创建超时 context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 停止所有 Flow
	if m.telemetryFlow != nil {
		m.telemetryFlow.Stop()
	}
	if m.attributeFlow != nil {
		m.attributeFlow.Stop()
	}
	if m.eventFlow != nil {
		m.eventFlow.Stop()
	}

	// TODO: 停止其他 Flow

	// 关闭 Bus
	m.bus.Close()

	// 等待停止完成或超时
	select {
	case <-ctx.Done():
		m.logger.Warn("FlowManager stop timeout")
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		// 给一点时间让 goroutine 清理
		m.logger.Info("FlowManager stopped successfully")
		return nil
	}
}

// GetBusStats 获取 Bus 统计信息
func (m *FlowManager) GetBusStats() map[string]interface{} {
	return m.bus.GetChannelStats()
}

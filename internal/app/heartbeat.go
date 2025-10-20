package app

import (
	"fmt"

	"project/internal/service"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// HeartbeatMonitorWrapper 包装 HeartbeatMonitor 为 Service
type HeartbeatMonitorWrapper struct {
	monitor   *service.HeartbeatMonitor
	isEnabled bool
	logger    *logrus.Logger
}

// Name 返回服务名称
func (h *HeartbeatMonitorWrapper) Name() string {
	return "Heartbeat Monitor 服务"
}

// Start 启动心跳监控服务
func (h *HeartbeatMonitorWrapper) Start() error {
	if !h.isEnabled {
		h.logger.Info("Heartbeat monitor is disabled, skipping...")
		return nil
	}

	if err := h.monitor.Start(); err != nil {
		return fmt.Errorf("failed to start heartbeat monitor: %w", err)
	}

	h.logger.Info("Heartbeat monitor started successfully")
	return nil
}

// Stop 停止心跳监控服务
func (h *HeartbeatMonitorWrapper) Stop() error {
	if !h.isEnabled {
		return nil
	}

	h.logger.Info("Stopping heartbeat monitor...")

	if err := h.monitor.Stop(); err != nil {
		return fmt.Errorf("failed to stop heartbeat monitor: %w", err)
	}

	h.logger.Info("Heartbeat monitor stopped")
	return nil
}

// WithHeartbeatMonitor 添加心跳监控服务
// 依赖: Flow 服务(需要 Flow Bus 作为 StatusPublisher)
func WithHeartbeatMonitor() Option {
	return func(a *Application) error {
		// 检查是否启用 Flow 和心跳监控
		flowEnabled := viper.GetBool("uplink.enable")
		if !flowEnabled {
			logrus.Info("Uplink service is disabled, heartbeat monitor will not start")
			wrapper := &HeartbeatMonitorWrapper{
				isEnabled: false,
				logger:    a.Logger,
			}
			a.RegisterService(wrapper)
			return nil
		}

		// ✨ 获取 Flow Bus（实现了 StatusPublisher 接口）
		flowBus := a.GetUplinkBus()
		if flowBus == nil {
			return fmt.Errorf("uplink bus not initialized, please add WithFlowService() before WithHeartbeatMonitor()")
		}

		// 创建 HeartbeatMonitor（注入 Flow Bus 作为 StatusPublisher）
		monitor := service.NewHeartbeatMonitor(
			global.STATUS_REDIS,
			flowBus, // ✨ Bus 实现了 StatusPublisher 接口
			a.Logger,
		)

		// 创建服务包装器
		wrapper := &HeartbeatMonitorWrapper{
			monitor:   monitor,
			isEnabled: true,
			logger:    a.Logger,
		}

		// 注册到服务管理器
		a.RegisterService(wrapper)

		logrus.Info("Heartbeat monitor registered")
		return nil
	}
}

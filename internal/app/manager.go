package app

import (
	"project/mqtt/device"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
)

// Manager 服务管理器
type Manager struct {
	deviceListener *device.DeviceListener
}

// NewManager 创建服务管理器
func NewManager() *Manager {
	return &Manager{
		deviceListener: device.NewDeviceListener(global.STATUS_REDIS),
	}
}

// Start 启动所有服务
func (m *Manager) Start() error {
	// 启动设备状态监听器
	if err := m.deviceListener.Start(); err != nil {
		return err
	}

	logrus.Info("所有服务启动完成")
	return nil
}

// Stop 停止所有服务
func (m *Manager) Stop() {
	logrus.Info("正在停止所有服务...")

	if err := m.deviceListener.Stop(); err != nil {
		logrus.WithError(err).Error("停止设备监听器失败")
	}

	logrus.Info("所有服务已停止")
}

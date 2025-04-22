package app

import (
	"project/mqtt"
	"project/mqtt/device"
	"project/mqtt/publish"
	"project/mqtt/subscribe"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// MQTTService 实现MQTT相关服务
type MQTTService struct {
	initialized bool
}

// NewMQTTService 创建MQTT服务实例
func NewMQTTService() *MQTTService {
	return &MQTTService{
		initialized: false,
	}
}

// Name 返回服务名称
func (s *MQTTService) Name() string {
	return "MQTT服务"
}

// Start 启动MQTT服务
func (s *MQTTService) Start() error {
	// 检查是否启用MQTT
	if !viper.GetBool("mqtt.enabled") {
		logrus.Info("MQTT服务已被禁用，跳过初始化")
		return nil
	}

	logrus.Info("正在启动MQTT服务...")

	// 初始化MQTT客户端
	if err := mqtt.MqttInit(); err != nil {
		return err
	}

	// 初始化设备状态
	go device.InitDeviceStatus()

	// 初始化订阅
	if err := subscribe.SubscribeInit(); err != nil {
		return err
	}

	// 初始化发布
	publish.PublishInit()

	s.initialized = true
	logrus.Info("MQTT服务启动完成")
	return nil
}

// Stop 停止MQTT服务
func (s *MQTTService) Stop() error {
	if !s.initialized {
		return nil
	}

	logrus.Info("正在停止MQTT服务...")
	// 这里可以添加停止MQTT客户端的逻辑
	// 如果mqtt包提供了关闭方法，可以在这里调用

	logrus.Info("MQTT服务已停止")
	return nil
}

// WithMQTTService 将MQTT服务添加到应用
func WithMQTTService() Option {
	return func(app *Application) error {
		service := NewMQTTService()
		app.RegisterService(service)
		return nil
	}
}

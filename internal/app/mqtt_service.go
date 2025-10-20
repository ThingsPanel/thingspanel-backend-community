package app

import (
	"fmt"

	"project/initialize"
	"project/internal/adapter/mqttadapter"
	"project/mqtt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// MQTTService 实现MQTT相关服务
type MQTTService struct {
	app         *Application
	initialized bool
	mqttAdapter *mqttadapter.Adapter
}

// 全局 Adapter 实例（供其他模块调用）
var globalMQTTAdapter *mqttadapter.Adapter

// GetGlobalMQTTAdapter 获取全局 MQTT Adapter 实例
func GetGlobalMQTTAdapter() *mqttadapter.Adapter {
	return globalMQTTAdapter
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
	logrus.Info("正在启动MQTT服务...")

	// 初始化MQTT配置（只加载配置，不创建客户端）
	if err := mqtt.MqttInit(); err != nil {
		return err
	}

	// 初始化限流器
	initialize.NewAutomateLimiter()

	// 注意: 设备状态监控已由 Flow 层的 HeartbeatMonitor 和 StatusUplink 接管
	// 不再使用 device.InitDeviceStatus()

	// ⚠️ 旧的订阅流程已废弃，不再调用 subscribe.SubscribeInit()
	// ⚠️ 旧的发布流程已废弃，不再调用 publish.PublishInit()
	// 所有 MQTT 操作（订阅+发布）现在由 MQTTAdapter 统一管理

	// ✨ 创建 MQTT Adapter 并订阅所有 Topic
	if err := s.initMQTTAdapter(); err != nil {
		logrus.WithError(err).Error("Failed to initialize MQTT Adapter")
		return err
	}

	s.initialized = true
	logrus.Info("MQTT服务启动完成")
	return nil
}

// initMQTTAdapter 初始化 MQTT Adapter（创建独立的 MQTT 客户端）
func (s *MQTTService) initMQTTAdapter() error {
	// 1. 获取 Flow Bus
	bus := s.app.GetUplinkBus()
	if bus == nil {
		return fmt.Errorf("uplink bus not initialized, cannot create MQTT Adapter")
	}

	// 2. 创建 Adapter 专用的 MQTT 客户端（不依赖 mqtt/publish/）
	broker := viper.GetString("mqtt.broker")
	username := viper.GetString("mqtt.user")
	password := viper.GetString("mqtt.pass")

	mqttConfig := mqttadapter.MQTTConfig{
		Broker:   broker,
		Username: username,
		Password: password,
		// ClientID 会自动生成
	}

	mqttClient, err := mqttadapter.CreateMQTTClient(mqttConfig, s.app.Logger)
	if err != nil {
		return fmt.Errorf("failed to create MQTT client for Adapter: %w", err)
	}

	// 3. 创建 MQTT Adapter
	s.mqttAdapter = mqttadapter.NewAdapter(bus, mqttClient, s.app.Logger)
	globalMQTTAdapter = s.mqttAdapter // 设置全局实例
	logrus.Info("MQTT Adapter created with independent client")

	// 4. 订阅响应 Topic（命令响应、属性设置响应）
	if err := s.mqttAdapter.SubscribeResponseTopics(mqttClient); err != nil {
		return fmt.Errorf("failed to subscribe response topics: %w", err)
	}

	// 5. 订阅设备上行 Topic（遥测、属性、事件、状态）
	if err := s.mqttAdapter.SubscribeDeviceTopics(mqttClient); err != nil {
		return fmt.Errorf("failed to subscribe device topics: %w", err)
	}

	// 6. 订阅网关上行 Topic（网关遥测、属性、事件）
	if err := s.mqttAdapter.SubscribeGatewayTopics(mqttClient); err != nil {
		return fmt.Errorf("failed to subscribe gateway topics: %w", err)
	}

	logrus.Info("MQTT Adapter initialized successfully - all subscriptions active")
	logrus.Info("📌 Old mqtt/subscribe/ flow is now completely bypassed")
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
		service.app = app // ✨ 设置 Application 引用
		app.RegisterService(service)
		app.mqttService = service // ✨ 保存服务引用
		return nil
	}
}

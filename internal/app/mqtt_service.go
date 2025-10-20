package app

import (
	"fmt"

	"project/internal/adapter/mqttadapter"
	"project/mqtt"
	"project/mqtt/publish"
	"project/mqtt/subscribe"

	"github.com/sirupsen/logrus"
)

// MQTTService 实现MQTT相关服务
type MQTTService struct {
	app         *Application
	initialized bool
	mqttAdapter *mqttadapter.Adapter
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
	// if !viper.GetBool("mqtt.enabled") {
	// 	logrus.Info("MQTT服务已被禁用,跳过初始化")
	// 	return nil
	// }

	logrus.Info("正在启动MQTT服务...")

	// 初始化MQTT客户端
	if err := mqtt.MqttInit(); err != nil {
		return err
	}

	// 注意: 设备状态监控已由 Flow 层的 HeartbeatMonitor 和 StatusFlow 接管
	// 不再使用 device.InitDeviceStatus()

	// 初始化订阅
	if err := subscribe.SubscribeInit(); err != nil {
		return err
	}

	// 初始化发布
	publish.PublishInit()

	// ✨ 创建 MQTT Adapter 并订阅响应 Topic
	if err := s.initMQTTAdapter(); err != nil {
		logrus.WithError(err).Warn("Failed to initialize MQTT Adapter, response topics may not work")
		// 不阻塞启动，继续运行
	}

	s.initialized = true
	logrus.Info("MQTT服务启动完成")
	return nil
}

// initMQTTAdapter 初始化 MQTT Adapter（在 MQTT 连接成功后调用）
func (s *MQTTService) initMQTTAdapter() error {
	// 1. 获取 Flow Bus
	bus := s.app.GetFlowBus()
	if bus == nil {
		return fmt.Errorf("Flow Bus not initialized, cannot create MQTT Adapter")
	}

	// 2. 获取 MQTT 客户端
	mqttClient := publish.GetMQTTClient()
	if mqttClient == nil || !mqttClient.IsConnected() {
		return fmt.Errorf("MQTT client not connected")
	}

	// 3. 创建 MQTT Adapter（注入 MQTT 客户端）
	s.mqttAdapter = mqttadapter.NewAdapter(bus, mqttClient, s.app.Logger)
	logrus.Info("MQTT Adapter created")

	// 4. 订阅响应 Topic
	if err := s.mqttAdapter.SubscribeResponseTopics(mqttClient); err != nil {
		return fmt.Errorf("failed to subscribe response topics: %w", err)
	}

	// 5. 注册 Adapter 到订阅层（用于上行数据）
	subscribe.SetMQTTAdapter(s.mqttAdapter)

	logrus.Info("MQTT Adapter initialized and response topics subscribed successfully")
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

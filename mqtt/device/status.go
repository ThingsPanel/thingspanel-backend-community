package device

import (
	config "project/mqtt"
	"project/mqtt/subscribe"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

// StatusManager 设备状态管理器
type StatusManager struct {
	mqttClient     mqtt.Client
	subscribeTopic string
	subscribeQos   byte
	messageHandler mqtt.MessageHandler
	// 重试配置
	retryInterval time.Duration
	maxRetries    int
}

// StatusConfig MQTT配置
type StatusConfig struct {
	Broker        string
	ClientID      string
	Username      string
	Password      string
	RetryInterval time.Duration // 重试间隔
	MaxRetries    int           // 最大重试次数，0表示无限重试
}

// InitDeviceStatus 初始化设备状态监控
func InitDeviceStatus() error {
	uuid := uuid.New()
	// 配置MQTT连接
	config := StatusConfig{
		Broker:        config.MqttConfig.Broker,
		ClientID:      "device-status-" + uuid[0:10],
		Username:      config.MqttConfig.User,
		Password:      config.MqttConfig.Pass,
		RetryInterval: 5 * time.Second, // 默认5秒重试一次
		MaxRetries:    0,               // 默认无限重试
	}

	// 创建状态管理器
	manager, err := NewStatusManager(config)
	if err != nil {
		logrus.WithError(err).Error("创建状态管理器失败")
		return err
	}

	// 优雅关闭
	defer manager.Stop()

	// 启动监控
	if err := manager.Start(); err != nil {
		logrus.WithError(err).Error("启动状态监控失败")
		return err
	}

	logrus.Info("设备状态监控已启动")

	// 保持程序运行
	select {}
}

// NewStatusManager 创建状态管理器
func NewStatusManager(config StatusConfig) (*StatusManager, error) {
	messageHandler := func(_ mqtt.Client, msg mqtt.Message) {
		logrus.WithFields(logrus.Fields{
			"topic":   msg.Topic(),
			"payload": string(msg.Payload()),
		}).Debug("收到设备状态消息")

		subscribe.DeviceOnline(msg.Payload(), msg.Topic())
	}

	manager := &StatusManager{
		subscribeTopic: "devices/status/+",
		subscribeQos:   byte(1),
		messageHandler: messageHandler,
		retryInterval:  config.RetryInterval,
		maxRetries:     config.MaxRetries,
	}

	opts := mqtt.NewClientOptions().
		AddBroker(config.Broker).
		SetClientID(config.ClientID).
		SetUsername(config.Username).
		SetPassword(config.Password).
		SetAutoReconnect(true).
		SetCleanSession(false)

	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		logrus.Info("Connected to MQTT broker")
		if err := manager.subscribe(); err != nil {
			logrus.WithError(err).Error("重新订阅失败")
		}
	})

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logrus.WithError(err).Warn("Lost connection to MQTT broker")
	})

	client := mqtt.NewClient(opts)
	manager.mqttClient = client

	// 执行首次连接（带重试）
	if err := manager.connectWithRetry(); err != nil {
		return nil, err
	}

	return manager, nil
}

// connectWithRetry 带重试的连接方法
func (sm *StatusManager) connectWithRetry() error {
	retryCount := 0
	for {
		logrus.WithFields(logrus.Fields{
			"retry_count": retryCount,
			"max_retries": sm.maxRetries,
		}).Info("Attempting to connect to MQTT broker")

		token := sm.mqttClient.Connect()
		if token.WaitTimeout(10*time.Second) && token.Error() == nil {
			return nil
		}

		if sm.maxRetries > 0 && retryCount >= sm.maxRetries {
			return token.Error()
		}

		retryCount++
		logrus.WithFields(logrus.Fields{
			"retry_count": retryCount,
			"interval":    sm.retryInterval,
			"error":       token.Error(),
		}).Warn("Connection failed, retrying...")

		time.Sleep(sm.retryInterval)
	}
}

// subscribe 订阅主题
func (sm *StatusManager) subscribe() error {
	logrus.WithField("topic", sm.subscribeTopic).Info("订阅设备状态主题")

	if token := sm.mqttClient.Subscribe(sm.subscribeTopic, sm.subscribeQos, sm.messageHandler); token.Wait() && token.Error() != nil {
		logrus.WithError(token.Error()).Error("订阅主题失败")
		return token.Error()
	}
	return nil
}

// Start 开始监听设备状态
func (sm *StatusManager) Start() error {
	return sm.subscribe()
}

// Stop 停止监听
func (sm *StatusManager) Stop() {
	if sm.mqttClient != nil && sm.mqttClient.IsConnected() {
		logrus.Info("正在断开MQTT连接")
		sm.mqttClient.Disconnect(250)
	}
}

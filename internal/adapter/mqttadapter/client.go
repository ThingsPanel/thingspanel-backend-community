package mqttadapter

import (
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

// MQTTConfig MQTT 客户端配置
type MQTTConfig struct {
	Broker   string
	Username string
	Password string
	ClientID string // 可选，不提供则自动生成
}

// CreateMQTTClient 创建 MQTT 客户端（Adapter 专用）
func CreateMQTTClient(config MQTTConfig, logger *logrus.Logger) (mqtt.Client, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.Broker)
	opts.SetUsername(config.Username)
	opts.SetPassword(config.Password)

	// 客户端 ID
	clientID := config.ClientID
	if clientID == "" {
		clientID = "thingspanel-adapter-" + uuid.New()[0:8]
	}
	opts.SetClientID(clientID)

	// 干净会话
	opts.SetCleanSession(true)
	// 恢复客户端订阅，需要 broker 支持
	opts.SetResumeSubs(true)
	// 自动重连
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(200 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)

	// 连接成功回调
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		logger.WithField("client_id", clientID).Info("MQTT Adapter client connected")
	})

	// 断线重连回调
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logger.WithError(err).Warn("MQTT Adapter connection lost, reconnecting...")
		client.Disconnect(250)

		// 等待连接成功，失败重新连接
		for {
			token := client.Connect()
			if token.Wait() && token.Error() == nil {
				logger.Info("MQTT Adapter reconnected successfully")
				break
			}
			logger.WithError(token.Error()).Error("MQTT Adapter reconnect failed, retrying...")
			time.Sleep(5 * time.Second)
		}
	})

	// 创建客户端
	client := mqtt.NewClient(opts)

	// 等待连接成功，失败重新连接
	for {
		token := client.Connect()
		if token.Wait() && token.Error() != nil {
			logger.WithError(token.Error()).Error("MQTT Adapter connection failed, retrying...")
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	logger.WithField("client_id", clientID).Info("MQTT Adapter client created and connected")
	return client, nil
}

// DisconnectMQTTClient 断开 MQTT 客户端连接
func DisconnectMQTTClient(client mqtt.Client, logger *logrus.Logger) {
	if client != nil && client.IsConnected() {
		client.Disconnect(250)
		if logger != nil {
			logger.Info("MQTT Adapter client disconnected")
		}
	}
}

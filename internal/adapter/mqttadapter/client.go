package mqttadapter

import (
	"context"
	"errors"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

const (
	connectAttemptTimeout = 5 * time.Second
	connectRetryInterval  = 5 * time.Second
	connectPollInterval   = 50 * time.Millisecond
	connectStartupLimit   = 30 * time.Second
)

// MQTTConfig MQTT 客户端配置
type MQTTConfig struct {
	Broker            string
	Username          string
	Password          string
	ClientID          string                   // 可选，不提供则自动生成
	ManualAck         bool                     // ingress_v1 保留失败消息供 broker 重投
	OnConnectCallback func(client mqtt.Client) // 连接成功回调（用于重新订阅）
}

// CreateMQTTClient 创建 MQTT 客户端（Adapter 专用）。
// 为兼容现有调用方保留此入口；新的生命周期代码应传递明确的 context。
func CreateMQTTClient(config MQTTConfig, logger *logrus.Logger) (mqtt.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectStartupLimit)
	defer cancel()
	return CreateMQTTClientContext(ctx, config, logger)
}

// CreateMQTTClientContext 创建 MQTT 客户端，并在 ctx 取消时终止启动重试。
// 每次连接等待都有上限；整体启动期限由调用方 ctx 决定。
func CreateMQTTClientContext(ctx context.Context, config MQTTConfig, logger *logrus.Logger) (mqtt.Client, error) {
	if ctx == nil {
		return nil, errors.New("MQTT client context is nil")
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, connectStartupLimit)
	defer cancel()

	opts := newMQTTClientOptions(config, logger)
	clientID := opts.ClientID
	client := mqtt.NewClient(opts)
	for {
		if err := ctx.Err(); err != nil {
			client.Disconnect(0)
			return nil, err
		}

		token := client.Connect()
		if err := WaitTokenContext(ctx, token, connectAttemptTimeout); err == nil {
			logger.WithField("client_id", clientID).Info("MQTT Adapter client created and connected")
			return client, nil
		} else if ctx.Err() != nil {
			client.Disconnect(0)
			return nil, ctx.Err()
		} else {
			logger.WithError(err).WithField("client_id", clientID).Warn("MQTT Adapter connection failed; retrying")
			// A timed-out Paho token can still complete in the background. Disconnect
			// before creating another attempt so failed startup cannot leave a live client.
			client.Disconnect(0)
		}

		timer := time.NewTimer(connectRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			client.Disconnect(0)
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func newMQTTClientOptions(config MQTTConfig, logger *logrus.Logger) *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.Broker)
	opts.SetUsername(config.Username)
	opts.SetPassword(config.Password)

	clientID := config.ClientID
	if clientID == "" {
		clientID = "thingspanel-adapter-default"
	}
	opts.SetClientID(clientID)

	// 持久会话用于保留 broker 端订阅和离线 QoS 消息。
	opts.SetCleanSession(false)
	opts.SetResumeSubs(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(false) // 首次启动重试受 ctx 管理，连接后由 AutoReconnect 管理。
	opts.SetConnectTimeout(connectAttemptTimeout)
	opts.SetConnectRetryInterval(connectRetryInterval)
	opts.SetMaxReconnectInterval(200 * time.Second)
	opts.SetAutoAckDisabled(config.ManualAck)
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		logger.WithField("client_id", clientID).Info("MQTT Adapter client connected")
		if config.OnConnectCallback != nil {
			config.OnConnectCallback(client)
		}
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logger.WithError(err).Warn("MQTT Adapter connection lost, auto-reconnect will handle it...")
	})
	opts.SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
		logger.Info("MQTT Adapter reconnecting...")
	})
	return opts
}

// WaitTokenContext 等待 Paho token 完成，直到它完成、ctx 取消或 timeout 到期。
// timeout 为 0 时只受 ctx 控制。调用方在取消后仍需关闭所属 MQTT client。
func WaitTokenContext(ctx context.Context, token mqtt.Token, timeout time.Duration) error {
	if ctx == nil {
		return errors.New("MQTT token context is nil")
	}
	if token == nil {
		return errors.New("MQTT token is nil")
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	for {
		if token.WaitTimeout(connectPollInterval) {
			return token.Error()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// DisconnectMQTTClient 断开 MQTT 客户端连接
func DisconnectMQTTClient(client mqtt.Client, logger *logrus.Logger) {
	if client != nil {
		client.Disconnect(250)
		if logger != nil {
			logger.Info("MQTT Adapter client disconnected")
		}
	}
}

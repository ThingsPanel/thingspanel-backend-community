package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"project/initialize"
	"project/internal/adapter/mqttadapter"
	"project/mqtt"

	mqtt_client "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var errMQTTServiceStopping = errors.New("MQTT service is stopping")

// MQTTService 实现MQTT相关服务
type MQTTService struct {
	app *Application

	mu           sync.Mutex
	initialized  bool
	starting     bool
	startCancel  context.CancelFunc
	startDone    chan struct{}
	startErr     error
	generation   uint64
	mqttAdapter  *mqttadapter.Adapter
	mqttClient   mqtt_client.Client
	brokerWorker *brokerIngressWorker

	clientFactory func(context.Context, mqttadapter.MQTTConfig, *logrus.Logger) (mqtt_client.Client, error)
	subscribe     func(context.Context, *mqttadapter.Adapter, mqtt_client.Client) error
}

var globalMQTTAdapterMu sync.RWMutex

// 全局 Adapter 实例（供其他模块调用）
var globalMQTTAdapter *mqttadapter.Adapter

// GetGlobalMQTTAdapter 获取全局 MQTT Adapter 实例
func GetGlobalMQTTAdapter() *mqttadapter.Adapter {
	globalMQTTAdapterMu.RLock()
	defer globalMQTTAdapterMu.RUnlock()
	return globalMQTTAdapter
}

// NewMQTTService 创建MQTT服务实例
func NewMQTTService() *MQTTService {
	return &MQTTService{}
}

// Name 返回服务名称
func (s *MQTTService) Name() string {
	return "MQTT服务"
}

// Start 兼容 Service 接口；独立取消由 Stop 提供。
func (s *MQTTService) Start() error {
	return s.StartContext(context.Background())
}

// StartContext 启动 MQTT 服务，并响应调用者的 ctx 取消。
func (s *MQTTService) StartContext(parent context.Context) error {
	if parent == nil {
		return errors.New("MQTT service context is nil")
	}
	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		return nil
	}
	if s.starting {
		done := s.startDone
		s.mu.Unlock()
		select {
		case <-done:
			s.mu.Lock()
			err := s.startErr
			if err == nil && s.initialized {
				s.mu.Unlock()
				return nil
			}
			s.mu.Unlock()
			return err
		case <-parent.Done():
			return parent.Err()
		}
	}
	ctx, cancel := context.WithCancel(parent)
	s.generation++
	generation := s.generation
	done := make(chan struct{})
	s.starting = true
	s.startCancel = cancel
	s.startDone = done
	s.mu.Unlock()

	err := s.start(ctx)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		s.cleanupPartialStart()
		cancel()
	}

	s.mu.Lock()
	if err == nil {
		s.initialized = true
	}
	s.starting = false
	s.startDone = nil
	s.startErr = err
	if err != nil {
		s.startCancel = nil
	}
	close(done)
	s.mu.Unlock()
	if err == nil {
		// Keep the caller context alive for reconnect callbacks. If the parent
		// cancels after startup, stop the connected client as part of its lifecycle.
		go func() {
			<-ctx.Done()
			s.stopGeneration(generation)
		}()
	}
	return err
}

func (s *MQTTService) start(ctx context.Context) error {
	logrus.Info("正在启动MQTT服务...")
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := mqtt.MqttInit(); err != nil {
		return err
	}
	initialize.NewAutomateLimiter()
	if err := s.initMQTTAdapter(ctx); err != nil {
		logrus.WithError(err).Error("Failed to initialize MQTT Adapter")
		return err
	}
	logrus.Info("MQTT服务启动完成")
	return nil
}

// initMQTTAdapter 初始化 MQTT Adapter（创建独立的 MQTT 客户端）
func (s *MQTTService) initMQTTAdapter(ctx context.Context) error {
	if s.app == nil {
		return errors.New("application is nil")
	}
	bus := s.app.GetUplinkBus()
	if bus == nil {
		return fmt.Errorf("uplink bus not initialized, cannot create MQTT Adapter")
	}

	broker := viper.GetString("mqtt.broker")
	username := viper.GetString("mqtt.user")
	password := viper.GetString("mqtt.pass")
	clientID := viper.GetString("mqtt.client_id")
	var adapterRef atomic.Pointer[mqttadapter.Adapter]
	prepareAdapter := func(client mqtt_client.Client) error {
		adapter := mqttadapter.NewAdapter(bus, client, s.logger())
		adapter.SetContext(ctx)
		if err := adapter.ConfigureIngressFromConfig(); err != nil {
			return fmt.Errorf("failed to configure MQTT ingress: %w", err)
		}
		if err := s.configureBrokerIngress(ctx, adapter); err != nil {
			return fmt.Errorf("failed to initialize reliable MQTT ingress: %w", err)
		}
		adapter.RegisterIngressRoutes(client)
		adapterRef.Store(adapter)
		return nil
	}
	mqttConfig := mqttadapter.MQTTConfig{
		Broker:        broker,
		Username:      username,
		Password:      password,
		ClientID:      clientID,
		ManualAck:     viper.GetString("mqtt.input_profile") == "ingress_v1",
		BeforeConnect: prepareAdapter,
		OnConnectCallback: func(client mqtt_client.Client) {
			if ctx.Err() != nil {
				return
			}
			adapter := adapterRef.Load()
			if adapter == nil {
				return // 首次连接可能早于 Adapter 构造；启动流程会执行首轮订阅。
			}
			if err := s.subscribeAdapter(ctx, adapter, client); err != nil {
				logrus.WithError(err).Error("Failed to re-subscribe MQTT Adapter topics")
			}
		},
	}
	client, err := s.createClient(ctx, mqttConfig)
	if err != nil {
		return fmt.Errorf("failed to create MQTT client for Adapter: %w", err)
	}
	adapter := adapterRef.Load()
	if adapter == nil {
		// Test factories must honor BeforeConnect for persistent-session tests.
		// This fallback also preserves legacy injectable factories.
		if err := prepareAdapter(client); err != nil {
			mqttadapter.DisconnectMQTTClient(client, s.logger())
			return err
		}
		adapter = adapterRef.Load()
	}
	if err := ctx.Err(); err != nil {
		mqttadapter.DisconnectMQTTClient(client, s.logger())
		return err
	}
	if err := s.subscribeAdapter(ctx, adapter, client); err != nil {
		mqttadapter.DisconnectMQTTClient(client, s.logger())
		return fmt.Errorf("failed to subscribe MQTT Adapter topics: %w", err)
	}
	if err := ctx.Err(); err != nil {
		mqttadapter.DisconnectMQTTClient(client, s.logger())
		return err
	}

	s.mu.Lock()
	if !s.starting || ctx.Err() != nil {
		s.mu.Unlock()
		mqttadapter.DisconnectMQTTClient(client, s.logger())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errMQTTServiceStopping
	}
	s.mqttAdapter = adapter
	s.mqttClient = client
	globalMQTTAdapterMu.Lock()
	globalMQTTAdapter = adapter
	globalMQTTAdapterMu.Unlock()
	s.mu.Unlock()
	logrus.Info("MQTT Adapter initialized successfully - all subscriptions active")
	return nil
}

func (s *MQTTService) createClient(ctx context.Context, config mqttadapter.MQTTConfig) (mqtt_client.Client, error) {
	if s.clientFactory != nil {
		return s.clientFactory(ctx, config, s.logger())
	}
	return mqttadapter.CreateMQTTClientContext(ctx, config, s.logger())
}

func (s *MQTTService) subscribeAdapter(ctx context.Context, adapter *mqttadapter.Adapter, client mqtt_client.Client) error {
	if s.subscribe != nil {
		return s.subscribe(ctx, adapter, client)
	}
	return adapter.SubscribeAllContext(ctx, client)
}

func (s *MQTTService) logger() *logrus.Logger {
	if s.app != nil && s.app.Logger != nil {
		return s.app.Logger
	}
	return logrus.StandardLogger()
}

func (s *MQTTService) cleanupPartialStart() {
	s.mu.Lock()
	client := s.mqttClient
	adapter := s.mqttAdapter
	worker := s.brokerWorker
	s.mqttClient = nil
	s.mqttAdapter = nil
	s.brokerWorker = nil
	globalMQTTAdapterMu.Lock()
	if globalMQTTAdapter == adapter {
		globalMQTTAdapter = nil
	}
	globalMQTTAdapterMu.Unlock()
	s.mu.Unlock()
	if worker != nil {
		worker.Stop()
	}
	if client != nil {
		mqttadapter.DisconnectMQTTClient(client, s.logger())
	}
}

// Stop 停止 MQTT 服务。调用期间可取消正在进行的 StartContext。
func (s *MQTTService) Stop() error {
	return s.stopGeneration(0)
}

func (s *MQTTService) stopGeneration(generation uint64) error {
	s.mu.Lock()
	if generation != 0 && s.generation != generation {
		s.mu.Unlock()
		return nil
	}
	cancel := s.startCancel
	done := s.startDone
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}

	s.mu.Lock()
	if generation != 0 && s.generation != generation {
		s.mu.Unlock()
		return nil
	}
	worker := s.brokerWorker
	s.mu.Unlock()
	if worker != nil {
		worker.Stop()
	}

	s.mu.Lock()
	if generation != 0 && s.generation != generation {
		s.mu.Unlock()
		return nil
	}
	client := s.mqttClient
	adapter := s.mqttAdapter
	s.mqttClient = nil
	s.mqttAdapter = nil
	s.brokerWorker = nil
	s.initialized = false
	s.startCancel = nil
	globalMQTTAdapterMu.Lock()
	if globalMQTTAdapter == adapter {
		globalMQTTAdapter = nil
	}
	globalMQTTAdapterMu.Unlock()
	s.mu.Unlock()

	if client != nil {
		mqttadapter.DisconnectMQTTClient(client, s.logger())
	}
	logrus.Info("MQTT服务已停止")
	return nil
}

// WithMQTTService 将MQTT服务添加到应用
func WithMQTTService() Option {
	return func(app *Application) error {
		service := NewMQTTService()
		service.app = app
		app.RegisterService(service)
		app.mqttService = service
		return nil
	}
}

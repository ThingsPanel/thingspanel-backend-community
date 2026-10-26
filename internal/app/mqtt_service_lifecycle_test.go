package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"project/internal/adapter/mqttadapter"
	"project/internal/uplink"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

type lifecycleMQTTClient struct {
	mqtt.Client
	disconnects atomic.Int32
}

func (c *lifecycleMQTTClient) Disconnect(uint)   { c.disconnects.Add(1) }
func (c *lifecycleMQTTClient) IsConnected() bool { return true }

func lifecycleService() *MQTTService {
	return &MQTTService{
		app: &Application{
			Logger:        logrus.New(),
			uplinkService: &UplinkServiceWrapper{bus: uplink.NewBus(uplink.BusConfig{BufferSize: 1}, logrus.New())},
		},
	}
}

func resetGlobalAdapter() {
	globalMQTTAdapterMu.Lock()
	globalMQTTAdapter = nil
	globalMQTTAdapterMu.Unlock()
}

func TestMQTTServiceSubscriptionFailureDisconnectsAndDoesNotPublishAdapter(t *testing.T) {
	resetGlobalAdapter()
	client := &lifecycleMQTTClient{}
	service := lifecycleService()
	service.starting = true
	service.clientFactory = func(context.Context, mqttadapter.MQTTConfig, *logrus.Logger) (mqtt.Client, error) {
		return client, nil
	}
	service.subscribe = func(context.Context, *mqttadapter.Adapter, mqtt.Client) error {
		return errors.New("subscription rejected")
	}

	err := service.initMQTTAdapter(context.Background())
	if err == nil {
		t.Fatal("expected subscription error")
	}
	if got := client.disconnects.Load(); got != 1 {
		t.Fatalf("disconnect count = %d, want 1", got)
	}
	if got := GetGlobalMQTTAdapter(); got != nil {
		t.Fatalf("global adapter published after failed startup: %p", got)
	}
}

func TestMQTTServiceReconnectCallbackResubscribesAfterAdapterIsReady(t *testing.T) {
	resetGlobalAdapter()
	client := &lifecycleMQTTClient{}
	service := lifecycleService()
	service.starting = true
	var onConnect func(mqtt.Client)
	service.clientFactory = func(_ context.Context, config mqttadapter.MQTTConfig, _ *logrus.Logger) (mqtt.Client, error) {
		onConnect = config.OnConnectCallback
		return client, nil
	}
	var subscriptions atomic.Int32
	service.subscribe = func(ctx context.Context, _ *mqttadapter.Adapter, _ mqtt.Client) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		subscriptions.Add(1)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.initMQTTAdapter(ctx); err != nil {
		t.Fatalf("initialize adapter: %v", err)
	}
	if onConnect == nil {
		t.Fatal("connect callback was not configured")
	}
	if got := subscriptions.Load(); got != 1 {
		t.Fatalf("initial subscription count = %d, want 1", got)
	}
	onConnect(client)
	if got := subscriptions.Load(); got != 2 {
		t.Fatalf("subscription count after reconnect = %d, want 2", got)
	}

	service.initialized = true
	if err := service.Stop(); err != nil {
		t.Fatalf("stop service: %v", err)
	}
	if got := client.disconnects.Load(); got != 1 {
		t.Fatalf("disconnect count after Stop = %d, want 1", got)
	}
	if got := GetGlobalMQTTAdapter(); got != nil {
		t.Fatalf("global adapter retained after Stop: %p", got)
	}
}

func TestMQTTServiceStopCancelsConcurrentStartup(t *testing.T) {
	resetGlobalAdapter()
	service := lifecycleService()
	factoryStarted := make(chan struct{})
	service.clientFactory = func(ctx context.Context, _ mqttadapter.MQTTConfig, _ *logrus.Logger) (mqtt.Client, error) {
		close(factoryStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	service.subscribe = func(context.Context, *mqttadapter.Adapter, mqtt.Client) error { return nil }

	startResult := make(chan error, 1)
	go func() {
		// The factory is entered after MqttInit and after StartContext publishes
		// its cancel function, so Stop must be able to interrupt it.
		startResult <- service.StartContext(context.Background())
	}()
	select {
	case <-factoryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("service did not enter its cancellable client factory")
	}
	if err := service.Stop(); err != nil {
		t.Fatalf("stop service during startup: %v", err)
	}
	select {
	case err := <-startResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("startup error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not return after Stop canceled it")
	}
	if got := GetGlobalMQTTAdapter(); got != nil {
		t.Fatalf("global adapter retained after canceled startup: %p", got)
	}
}

package app

import (
	"context"

	"project/internal/adapter"
	"project/internal/downlink"
	"project/internal/processor"
	"project/internal/service"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// DownlinkServiceWrapper 下行服务包装器
type DownlinkServiceWrapper struct {
	bus     *downlink.Bus
	handler *downlink.Handler
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *logrus.Logger
}

// Name 服务名称
func (s *DownlinkServiceWrapper) Name() string {
	return "Downlink 下行服务"
}

// Start 启动服务
func (s *DownlinkServiceWrapper) Start() error {
	s.bus.Start(s.ctx, s.handler)
	s.logger.Info("Downlink service started successfully")
	return nil
}

// Stop 停止服务
func (s *DownlinkServiceWrapper) Stop() error {
	s.logger.Info("Stopping downlink service...")
	s.cancel()
	s.bus.Close()
	s.logger.Info("Downlink service stopped")
	return nil
}

// GetBus 获取消息总线（供 Service 层调用）
func (s *DownlinkServiceWrapper) GetBus() *downlink.Bus {
	return s.bus
}

// WithDownlinkService 创建下行服务
func WithDownlinkService() Option {
	return func(a *Application) error {
		// 1. 读取配置
		bufferSize := viper.GetInt("downlink.buffer_size")
		if bufferSize <= 0 {
			bufferSize = 1000 // 默认值
		}

		// 2. 创建消息总线
		bus := downlink.NewBus(bufferSize)

		// 3. ✨ 不再获取 MQTT 客户端（延迟到发布时）

		// 4. 创建 MQTT Publisher 适配器（不再注入 client）
		mqttPublisher := adapter.NewMQTTPublisher(a.Logger)

		// 5. 创建 Processor
		dataProcessor := processor.NewScriptProcessor()

		// 6. 创建处理器
		handler := downlink.NewHandler(mqttPublisher, dataProcessor, a.Logger)

		// 7. 创建 context
		ctx, cancel := context.WithCancel(context.Background())

		// 8. 创建服务包装器
		wrapper := &DownlinkServiceWrapper{
			bus:     bus,
			handler: handler,
			ctx:     ctx,
			cancel:  cancel,
			logger:  a.Logger,
		}

		// 9. 注册到 ServiceManager
		a.RegisterService(wrapper)

		// 10. 保存到 Application
		a.downlinkService = wrapper

		// ✨ 11. 注入到 Service 层
		service.GroupApp.CommandData.SetDownlinkBus(bus)
		service.GroupApp.AttributeData.SetDownlinkBus(bus) // ✨ 新增

		a.Logger.WithFields(logrus.Fields{
			"module":      "downlink",
			"buffer_size": bufferSize,
		}).Info("Downlink service initialized and injected into services")

		return nil
	}
}

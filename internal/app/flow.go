package app

import (
	"fmt"
	"time"

	"project/internal/adapter"
	"project/internal/flow"
	"project/internal/processor"
	"project/internal/service"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// FlowServiceWrapper 包装 FlowManager 为 Service
type FlowServiceWrapper struct {
	flowManager *flow.FlowManager
	bus         *flow.Bus
	isEnabled   bool
	logger      *logrus.Logger
}

// Name 返回服务名称
func (f *FlowServiceWrapper) Name() string {
	return "Flow 流程服务"
}

// Start 启动 Flow 服务
func (f *FlowServiceWrapper) Start() error {
	if !f.isEnabled {
		f.logger.Info("Flow service is disabled, skipping...")
		return nil
	}

	if err := f.flowManager.Start(); err != nil {
		return fmt.Errorf("failed to start flow manager: %w", err)
	}

	f.logger.Info("Flow service started successfully")
	return nil
}

// Stop 停止 Flow 服务
func (f *FlowServiceWrapper) Stop() error {
	if !f.isEnabled {
		return nil
	}

	f.logger.Info("Stopping flow service...")

	// 停止 FlowManager（30秒超时）
	if err := f.flowManager.Stop(30 * time.Second); err != nil {
		return fmt.Errorf("failed to stop flow manager: %w", err)
	}

	f.logger.Info("Flow service stopped")
	return nil
}

// GetBus 获取 Flow Bus（供 Application 调用）
func (f *FlowServiceWrapper) GetBus() *flow.Bus {
	return f.bus
}

// WithFlowService 添加 Flow 服务
func WithFlowService() Option {
	return func(a *Application) error {
		// 检查是否启用 Flow
		isEnabled := viper.GetBool("flow.enable")
		if !isEnabled {
			logrus.Info("Flow service is disabled in config")
			// 即使禁用也要注册服务（但 Start 时会跳过）
			wrapper := &FlowServiceWrapper{
				isEnabled: false,
				logger:    a.Logger,
			}
			a.RegisterService(wrapper)
			return nil
		}

		// 读取配置
		busBufferSize := viper.GetInt("flow.bus_buffer_size")
		if busBufferSize <= 0 {
			busBufferSize = 10000 // 默认值
		}

		logrus.Infof("Flow config: enabled=%v, bus_buffer_size=%d",
			isEnabled, busBufferSize)

		// 1. 创建 Bus
		bus := flow.NewBus(flow.BusConfig{
			BufferSize: busBufferSize,
		}, a.Logger)

		// 2. 创建 Processor
		dataProcessor := processor.NewScriptProcessor()

		// 3. 创建 HeartbeatService
		heartbeatService := service.NewHeartbeatService(global.STATUS_REDIS, a.Logger)

		// 4. 确保 Storage 服务已启动，获取 inputChan
		storageInputChan := a.GetStorageInputChan()
		if storageInputChan == nil {
			return fmt.Errorf("storage service not initialized, please add WithStorageService() before WithFlowService()")
		}

		// 5. 创建 TelemetryFlow
		telemetryFlow := flow.NewTelemetryFlow(flow.TelemetryFlowConfig{
			Processor:        dataProcessor,
			StorageInput:     storageInputChan,
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// 6. 创建 AttributeFlow
		attributeFlow := flow.NewAttributeFlow(flow.AttributeFlowConfig{
			Processor:        dataProcessor,
			StorageInput:     storageInputChan,
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// 7. 创建 EventFlow
		eventFlow := flow.NewEventFlow(flow.EventFlowConfig{
			Processor:        dataProcessor,
			StorageInput:     storageInputChan,
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// 8. 创建 StatusFlow
		statusFlow := flow.NewStatusFlow(flow.StatusFlowConfig{
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// ✨ 9. 创建 ResponseFlow
		responseFlow := flow.NewResponseFlow(flow.ResponseFlowConfig{
			Logger: a.Logger,
		})

		// 10. 创建 FlowManager
		flowManager := flow.NewFlowManager(flow.FlowManagerConfig{
			Bus:           bus,
			TelemetryFlow: telemetryFlow,
			AttributeFlow: attributeFlow,
			EventFlow:     eventFlow,
			StatusFlow:    statusFlow,
			ResponseFlow:  responseFlow, // ✨ 新增
			Logger:        a.Logger,
		})

		// 10. 创建服务包装器（不再创建 Adapter，由 MQTT 服务负责）
		wrapper := &FlowServiceWrapper{
			flowManager: flowManager,
			bus:         bus,
			isEnabled:   true,
			logger:      a.Logger,
		}

		// 13. 注册到服务管理器
		a.RegisterService(wrapper)

		// 14. 保存到 Application（供外部使用）
		a.flowService = wrapper

		logrus.Info("Flow service registered")
		return nil
	}
}

// GetMQTTAdapter 获取 MQTT Adapter（供 MQTT 订阅层使用）
func (a *Application) GetMQTTAdapter() *adapter.MQTTAdapter {
	if a.mqttService == nil {
		return nil
	}
	return a.mqttService.mqttAdapter
}

// GetFlowManager 获取 FlowManager（用于监控）
func (a *Application) GetFlowManager() *flow.FlowManager {
	if a.flowService == nil {
		return nil
	}
	return a.flowService.flowManager
}

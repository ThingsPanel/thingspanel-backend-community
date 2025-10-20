package app

import (
	"fmt"
	"time"

	"project/internal/processor"
	"project/internal/service"
	"project/internal/uplink"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// UplinkServiceWrapper 包装 UplinkManager 为 Service
type UplinkServiceWrapper struct {
	UplinkManager *uplink.UplinkManager
	bus           *uplink.Bus
	isEnabled     bool
	logger        *logrus.Logger
}

// Name 返回服务名称
func (f *UplinkServiceWrapper) Name() string {
	return "uplink 流程服务"
}

// Start 启动 Flow 服务
func (f *UplinkServiceWrapper) Start() error {
	if !f.isEnabled {
		f.logger.Info("Uplink service is disabled, skipping...")
		return nil
	}

	if err := f.UplinkManager.Start(); err != nil {
		return fmt.Errorf("failed to start uplink manager: %w", err)
	}

	f.logger.Info("Uplink service started successfully")
	return nil
}

// Stop 停止 Flow 服务
func (f *UplinkServiceWrapper) Stop() error {
	if !f.isEnabled {
		return nil
	}

	f.logger.Info("Stopping Uplink service...")

	// 停止 UplinkManager（30秒超时）
	if err := f.UplinkManager.Stop(30 * time.Second); err != nil {
		return fmt.Errorf("failed to stop uplink manager: %w", err)
	}

	f.logger.Info("Uplink service stopped")
	return nil
}

// GetBus 获取 Flow Bus（供 Application 调用）
func (f *UplinkServiceWrapper) GetBus() *uplink.Bus {
	return f.bus
}

// WithFlowService 添加 Flow 服务
func WithFlowService() Option {
	return func(a *Application) error {
		// 检查是否启用 Flow
		isEnabled := viper.GetBool("uplink.enable")
		if !isEnabled {
			logrus.Info("Uplink service is disabled in config")
			// 即使禁用也要注册服务（但 Start 时会跳过）
			wrapper := &UplinkServiceWrapper{
				isEnabled: false,
				logger:    a.Logger,
			}
			a.RegisterService(wrapper)
			return nil
		}

		// 读取配置
		busBufferSize := viper.GetInt("uplink.bus_buffer_size")
		if busBufferSize <= 0 {
			busBufferSize = 10000 // 默认值
		}

		logrus.Infof("Flow config: enabled=%v, bus_buffer_size=%d",
			isEnabled, busBufferSize)

		// 1. 创建 Bus
		bus := uplink.NewBus(uplink.BusConfig{
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

		// 5. 创建 TelemetryUplink
		telemetryUplink := uplink.NewTelemetryUplink(uplink.TelemetryUplinkConfig{
			Processor:        dataProcessor,
			StorageInput:     storageInputChan,
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// 6. 创建 AttributeUplink
		attributeUplink := uplink.NewAttributeUplink(uplink.AttributeUplinkConfig{
			Processor:        dataProcessor,
			StorageInput:     storageInputChan,
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// 7. 创建 EventUplink
		eventUplink := uplink.NewEventUplink(uplink.EventUplinkConfig{
			Processor:        dataProcessor,
			StorageInput:     storageInputChan,
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// 8. 创建 StatusUplink
		statusUplink := uplink.NewStatusUplink(uplink.StatusUplinkConfig{
			HeartbeatService: heartbeatService,
			Logger:           a.Logger,
		})

		// ✨ 9. 创建 ResponseUplink
		responseUplink := uplink.NewResponseUplink(uplink.ResponseUplinkConfig{
			Logger: a.Logger,
		})

		// 10. 创建 UplinkManager
		UplinkManager := uplink.NewUplinkManager(uplink.UplinkManagerConfig{
			Bus:             bus,
			TelemetryUplink: telemetryUplink,
			AttributeUplink: attributeUplink,
			EventUplink:     eventUplink,
			StatusUplink:    statusUplink,
			ResponseUplink:  responseUplink, // ✨ 新增
			Logger:          a.Logger,
		})

		// 10. 创建服务包装器（不再创建 Adapter，由 MQTT 服务负责）
		wrapper := &UplinkServiceWrapper{
			UplinkManager: UplinkManager,
			bus:           bus,
			isEnabled:     true,
			logger:        a.Logger,
		}

		// 13. 注册到服务管理器
		a.RegisterService(wrapper)

		// 14. 保存到 Application（供外部使用）
		a.uplinkService = wrapper

		logrus.Info("Uplink service registered")
		return nil
	}
}

// GetUplinkManager 获取 UplinkManager（用于监控）
func (a *Application) GetUplinkManager() *uplink.UplinkManager {
	if a.uplinkService == nil {
		return nil
	}
	return a.uplinkService.UplinkManager
}

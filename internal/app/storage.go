package app

import (
	"context"
	"fmt"
	"time"

	"project/internal/storage"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// StorageServiceWrapper 包装 Storage 为 Service
type StorageServiceWrapper struct {
	storage           storage.Storage
	inputChan         chan *storage.Message
	ctx               context.Context
	cancel            context.CancelFunc
	channelBufferSize int
}

// Name 返回服务名称
func (s *StorageServiceWrapper) Name() string {
	return "存储服务"
}

// Start 启动存储服务
func (s *StorageServiceWrapper) Start() error {
	if err := s.storage.Start(s.ctx, s.inputChan); err != nil {
		return fmt.Errorf("failed to start storage service: %w", err)
	}
	return nil
}

// Stop 停止存储服务
func (s *StorageServiceWrapper) Stop() error {
	logrus.Info("Stopping storage service...")

	// 关闭输入 channel
	if s.inputChan != nil {
		close(s.inputChan)
	}

	// 取消上下文
	if s.cancel != nil {
		s.cancel()
	}

	// 停止 storage 服务（30秒超时）
	if err := s.storage.Stop(30 * time.Second); err != nil {
		return fmt.Errorf("failed to stop storage service: %w", err)
	}

	logrus.Info("Storage service stopped")
	return nil
}

// WithStorageService 添加存储服务
func WithStorageService() Option {
	return func(a *Application) error {
		// 从 viper 读取配置
		config := storage.DefaultConfig()

		if viper.IsSet("storage.channel_buffer_size") {
			config.ChannelBufferSize = viper.GetInt("storage.channel_buffer_size")
		}
		if viper.IsSet("storage.telemetry_batch_size") {
			config.TelemetryBatchSize = viper.GetInt("storage.telemetry_batch_size")
		}
		if viper.IsSet("storage.telemetry_flush_interval") {
			config.TelemetryFlushInterval = viper.GetInt("storage.telemetry_flush_interval")
		}
		if viper.IsSet("storage.enable_metrics") {
			config.EnableMetrics = viper.GetBool("storage.enable_metrics")
		}

		logrus.Infof("Storage config: buffer=%d, batch=%d, flush=%dms, metrics=%v",
			config.ChannelBufferSize,
			config.TelemetryBatchSize,
			config.TelemetryFlushInterval,
			config.EnableMetrics,
		)

		// 创建输入 channel
		inputChan := make(chan *storage.Message, config.ChannelBufferSize)

		// 创建 Storage 实例
		storageService := storage.New(a.DB, a.Logger, config)

		// 创建上下文
		ctx, cancel := context.WithCancel(context.Background())

		// 创建服务包装器
		wrapper := &StorageServiceWrapper{
			storage:           storageService,
			inputChan:         inputChan,
			ctx:               ctx,
			cancel:            cancel,
			channelBufferSize: config.ChannelBufferSize,
		}

		// 注册到服务管理器
		a.RegisterService(wrapper)

		// 保存到 Application（用于获取）
		a.storageService = storageService
		a.storageInputChan = inputChan

		logrus.Info("Storage service registered")
		return nil
	}
}

// GetStorageService 获取 Storage 服务实例
func (a *Application) GetStorageService() storage.Storage {
	return a.storageService
}

// GetStorageInputChan 获取 Storage 输入通道
func (a *Application) GetStorageInputChan() chan<- *storage.Message {
	return a.storageInputChan
}

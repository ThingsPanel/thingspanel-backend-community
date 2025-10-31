package app

import (
	"time"

	"project/internal/diagnostics"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// WithDiagnostics 初始化诊断服务
func WithDiagnostics() Option {
	return func(a *Application) error {
		// 从 viper 读取配置
		config := diagnostics.DefaultConfig()

		if viper.IsSet("diagnostics.enabled") {
			config.Enabled = viper.GetBool("diagnostics.enabled")
		}
		if viper.IsSet("diagnostics.max_failures") {
			config.MaxFailures = viper.GetInt("diagnostics.max_failures")
		}
		if viper.IsSet("diagnostics.batch_flush_size") {
			config.BatchFlushSize = viper.GetInt("diagnostics.batch_flush_size")
		}
		if viper.IsSet("diagnostics.batch_flush_interval") {
			// 支持字符串格式（如 "1s"）和整数（毫秒）
			if intervalStr := viper.GetString("diagnostics.batch_flush_interval"); intervalStr != "" {
				if duration, err := time.ParseDuration(intervalStr); err == nil {
					config.BatchFlushInterval = duration
				}
			} else if intervalMs := viper.GetInt("diagnostics.batch_flush_interval"); intervalMs > 0 {
				config.BatchFlushInterval = time.Duration(intervalMs) * time.Millisecond
			}
		}

		// 初始化诊断收集器
		collector := diagnostics.GetInstance()
		if err := collector.Init(config, a.RedisClient); err != nil {
			return err
		}

		logrus.Infof("Diagnostics config: enabled=%v, max_failures=%d, flush_size=%d, flush_interval=%v",
			config.Enabled,
			config.MaxFailures,
			config.BatchFlushSize,
			config.BatchFlushInterval,
		)

		return nil
	}
}

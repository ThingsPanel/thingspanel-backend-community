package utils

import (
	"go.uber.org/zap"
)

var Logger *zap.Logger

// InitLogger 初始化日志
func InitLogger(level string) error {
	var config zap.Config

	if level == "debug" {
		config = zap.NewDevelopmentConfig()
	} else {
		config = zap.NewProductionConfig()
	}

	var err error
	Logger, err = config.Build()
	if err != nil {
		return err
	}

	return nil
}

// Sync 刷新日志缓冲
func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
}

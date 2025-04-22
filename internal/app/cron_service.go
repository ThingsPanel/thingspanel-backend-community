package app

import (
	"project/initialize/croninit"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// CronService 实现定时任务服务
type CronService struct {
	initialized bool
}

// NewCronService 创建定时任务服务实例
func NewCronService() *CronService {
	return &CronService{
		initialized: false,
	}
}

// Name 返回服务名称
func (s *CronService) Name() string {
	return "定时任务服务"
}

// Start 启动定时任务服务
func (s *CronService) Start() error {
	// 检查是否启用定时任务
	if !viper.GetBool("cron.enabled") {
		logrus.Info("定时任务服务已被禁用，跳过初始化")
		return nil
	}

	logrus.Info("正在启动定时任务服务...")

	// 初始化定时任务
	croninit.CronInit()

	s.initialized = true
	logrus.Info("定时任务服务启动完成")
	return nil
}

// Stop 停止定时任务服务
func (s *CronService) Stop() error {
	if !s.initialized {
		return nil
	}

	logrus.Info("正在停止定时任务服务...")
	// 如果croninit提供了停止方法，可以在这里调用

	logrus.Info("定时任务服务已停止")
	return nil
}

// WithCronService 将定时任务服务添加到应用
func WithCronService() Option {
	return func(app *Application) error {
		service := NewCronService()
		app.RegisterService(service)
		return nil
	}
}

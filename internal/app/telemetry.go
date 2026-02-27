package app

import (
	"project/internal/dal"
	"project/pkg/metrics"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// TelemetryService 包装器，用于启动时上报
type TelemetryService struct {
	logger *logrus.Logger
}

func NewTelemetryService() *TelemetryService {
	return &TelemetryService{
		logger: logrus.StandardLogger(),
	}
}

func (s *TelemetryService) Name() string {
	return "Telemetry 服务"
}

func (s *TelemetryService) Start() error {
	if !viper.GetBool("telemetry.enabled") {
		return nil
	}

	// 异步上报，不阻塞启动过程
	go func() {
		// 等待一段时间确保数据库已经完全就绪并完成迁移
		time.Sleep(5 * time.Second)

		s.logger.Info("Executing startup telemetry report...")
		ins := metrics.NewInstance()
		ins.Instan()
		ins.DeviceCount = dal.GetDevicesCount()
		ins.UserCount = dal.GetUsersCount()
		ins.SendToPostHog()
		s.logger.Info("Startup telemetry report sent.")
	}()

	return nil
}

func (s *TelemetryService) Stop() error {
	return nil
}

// WithTelemetry 注册安装量监测服务
func WithTelemetry() Option {
	return func(a *Application) error {
		a.RegisterService(NewTelemetryService())
		return nil
	}
}

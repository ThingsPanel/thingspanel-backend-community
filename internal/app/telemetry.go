package app

import (
	"project/internal/dal"
	"project/pkg/metrics"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TelemetryService 包装器，用于启动时上报
type TelemetryService struct {
	logger *logrus.Logger
	stopCh chan struct{}
	runMu  sync.Mutex
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
	if !metrics.TelemetryEnabled() {
		return nil
	}

	s.stopCh = make(chan struct{})

	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
			s.reportTelemetry("startup")
		case <-s.stopCh:
			return
		}

		ticker := time.NewTicker(metrics.HeartbeatInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.reportTelemetry("heartbeat")
			case <-s.stopCh:
				return
			}
		}
	}()

	return nil
}

func (s *TelemetryService) Stop() error {
	if s.stopCh != nil {
		close(s.stopCh)
	}
	return nil
}

// WithTelemetry 注册安装量监测服务
func WithTelemetry() Option {
	return func(a *Application) error {
		a.RegisterService(NewTelemetryService())
		return nil
	}
}

func (s *TelemetryService) reportTelemetry(trigger string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	ins := metrics.NewInstance()
	ins.Instan()
	ins.DeviceCount = dal.GetDevicesCount()
	ins.UserCount = dal.GetUsersCount()

	if err := metrics.ReportTelemetryCycle(ins, trigger); err != nil {
		s.logger.Debugf("Telemetry report skipped or failed: %v", err)
		return
	}

	s.logger.Infof("Telemetry report sent: trigger=%s", trigger)
}

package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"project/internal/dal"
	"project/internal/service"

	"github.com/sirupsen/logrus"
)

type NotificationSourceRelayService struct {
	bridge  *service.SourceBridge
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	started bool
}

func NewNotificationSourceRelayService(bridge *service.SourceBridge) *NotificationSourceRelayService {
	return &NotificationSourceRelayService{bridge: bridge, done: make(chan struct{})}
}

func (s *NotificationSourceRelayService) Name() string { return "notification source relay" }

func (s *NotificationSourceRelayService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("notification source relay already started")
	}
	if s.bridge == nil {
		close(s.done)
		return errors.New("notification source bridge missing")
	}
	if err := dal.ValidateSourceBridgeStartup(s.bridge.Enabled(), s.bridge.DeploymentID()); err != nil {
		close(s.done)
		return errors.New("notification source bridge schema/configuration unavailable")
	}
	s.started = true
	service.SetActiveSourceBridge(s.bridge)
	if !s.bridge.Enabled() {
		close(s.done)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.bridge.PollInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.bridge.RunOnce(ctx, 16, 8); err != nil {
					logrus.Warn("notification source relay poll failed")
				}
			}
		}
	}()
	return nil
}

func (s *NotificationSourceRelayService) Stop() error {
	s.mu.Lock()
	started := s.started
	if !started {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if started {
		<-s.done
	}
	if s.bridge != nil {
		s.bridge.Close()
	}
	service.SetActiveSourceBridge(nil)
	return nil
}

// WithNotificationSourceRelay wires the source bridge into the existing
// application service lifecycle. Root startup owns calling this option.
func WithNotificationSourceRelay(config service.SourceBridgeConfig, checker service.SourceCompatibilityChecker) Option {
	return func(app *Application) error {
		bridge, err := service.NewSourceBridge(config, checker)
		if err != nil {
			return err
		}
		// Install the routing mode during composition so an alarm arriving before
		// worker Start cannot mistake an existing Encore route for legacy.
		service.SetActiveSourceBridge(bridge)
		app.RegisterService(NewNotificationSourceRelayService(bridge))
		return nil
	}
}

package app

import (
	"context"
	"errors"
	"github.com/sirupsen/logrus"
	"project/internal/automationbridge"
	"sync"
	"time"
)

const nativeAutomationTickInterval = 5 * time.Second

type nativeAutomationScheduler struct {
	client   *automationbridge.Client
	logger   *logrus.Logger
	interval time.Duration
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
}

func (*nativeAutomationScheduler) Name() string   { return "Native automation scheduler" }
func (s *nativeAutomationScheduler) Start() error { return s.StartContext(context.Background()) }
func (s *nativeAutomationScheduler) StartContext(parent context.Context) error {
	if parent == nil {
		return errors.New("native scheduler context is nil")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if s.client == nil || s.interval <= 0 {
		return errors.New("native scheduler is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		return errors.New("native scheduler already started")
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.done = make(chan struct{})
	go s.run(ctx, s.done)
	return nil
}
func (s *nativeAutomationScheduler) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		receipt, err := s.client.Tick(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.WithError(err).Warn("native automation scheduler pass failed; retrying on next interval")
		} else {
			s.logger.WithFields(logrus.Fields{"replayed": receipt.Replayed, "schedule_executed": receipt.Counters["schedule_executed"], "outbox_drained": receipt.Counters["outbox_drained"]}).Debug("native automation scheduler pass completed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *nativeAutomationScheduler) Stop() error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	s.mu.Lock()
	if s.done == done {
		s.done = nil
		s.cancel = nil
	}
	s.mu.Unlock()
	return nil
}

func registerNativeAutomationScheduler(app *Application) error {
	config, err := automationbridge.ReadConfig()
	if err != nil {
		return err
	}
	if config.Mode != "native" {
		return nil
	}
	client, err := automationbridge.NewClient()
	if err != nil {
		return err
	}
	logger := app.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	app.RegisterService(&nativeAutomationScheduler{client: client, logger: logger, interval: nativeAutomationTickInterval})
	return nil
}

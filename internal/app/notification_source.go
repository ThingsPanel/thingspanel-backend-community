package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

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

const (
	notificationSourceBridgeEnabledEnv = "NOTIFICATION_SOURCE_BRIDGE_ENABLED"
	notificationEncoreBaseURLEnv       = "NOTIFICATION_ENCORE_BASE_URL"
	notificationSourceDeploymentIDEnv  = "NOTIFICATION_SOURCE_DEPLOYMENT_ID"
	notificationSourceBearerTokenEnv   = "NOTIFICATION_SOURCE_BEARER_TOKEN"
	notificationProjectionBearerEnv    = "NOTIFICATION_PROJECTION_BEARER_TOKEN"
)

// WithNotificationSourceRelayFromEnv always installs a startup guard, even
// when the bridge is disabled. That guard prevents an Encore-owned route from
// silently falling back to the legacy sender after configuration is removed.
func WithNotificationSourceRelayFromEnv() Option {
	return func(app *Application) error {
		bridge, err := NewNotificationSourceBridgeFromEnv()
		if err != nil {
			return err
		}
		service.SetActiveSourceBridge(bridge)
		app.RegisterService(NewNotificationSourceRelayService(bridge))
		return nil
	}
}

// NewNotificationSourceBridgeFromEnv constructs the bridge without starting
// its relay worker. The standalone operator uses this to validate and apply a
// single route while leaving queued events untouched.
func NewNotificationSourceBridgeFromEnv() (*service.SourceBridge, error) {
	enabled, err := sourceBridgeEnabledFromEnv()
	if err != nil {
		return nil, errors.New("notification source bridge configuration invalid")
	}
	config := service.SourceBridgeConfig{
		Enabled:               enabled,
		BaseURL:               os.Getenv(notificationEncoreBaseURLEnv),
		DeploymentID:          os.Getenv(notificationSourceDeploymentIDEnv),
		SourceBearerToken:     os.Getenv(notificationSourceBearerTokenEnv),
		ProjectionBearerToken: os.Getenv(notificationProjectionBearerEnv),
	}
	if !enabled {
		return service.NewSourceBridge(config, nil)
	}
	if !validSourceBridgeToken(config.SourceBearerToken) || !validSourceBridgeDeploymentID(config.DeploymentID) {
		return nil, errors.New("notification source bridge configuration incomplete")
	}
	checker, err := service.NewEmailSourceCompatibilityChecker(config.BaseURL, config.ProjectionBearerToken)
	if err != nil {
		return nil, errors.New("notification source bridge configuration invalid")
	}
	bridge, err := service.NewSourceBridge(config, checker)
	if err != nil {
		checker.Close()
		return nil, errors.New("notification source bridge configuration invalid")
	}
	return bridge, nil
}

func sourceBridgeEnabledFromEnv() (bool, error) {
	raw, configured := os.LookupEnv(notificationSourceBridgeEnabledEnv)
	if !configured {
		return false, nil
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("notification source bridge enabled value invalid")
	}
}

func validSourceBridgeToken(token string) bool {
	if len(token) < 32 || len(token) > 8192 {
		return false
	}
	for _, char := range token {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validSourceBridgeDeploymentID(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

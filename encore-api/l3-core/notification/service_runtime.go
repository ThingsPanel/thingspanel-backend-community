package notification

import (
	"context"
	"encoding/json"
	"encore.app/internal/authcore"
	"errors"
	"net/netip"
	"os"
	"strings"
)

// initService fails before the service can accept credential-bearing traffic.
// Ordinary Encore development tracing is not a safe profile for notification
// credentials; use the pinned native self-hosted infra profile instead.
func initService() (*Service, error) {
	privacy, err := NewPrivacyGuardFromEnv()
	if err != nil {
		return nil, errors.New("notification privacy configuration is unavailable")
	}
	protector, err := NewAESGCMProtectorFromEnv()
	if err != nil {
		return nil, errors.New("notification snapshot keyring is unavailable")
	}
	store, err := NewStore(StoreOptions{DB: notificationDB.Stdlib(), Protector: protector, RetryPolicy: NewJitteredRetryPolicy()})
	if err != nil {
		return nil, errors.New("notification store is unavailable")
	}
	fixture := os.Getenv("NOTIFICATION_FIXTURE_MODE") == "1"
	base := strings.TrimSpace(os.Getenv("NOTIFICATION_LEGACY_BASE_URL"))
	var sessions UserSessionChecker
	if authcore.Enabled() {
		sessions = nativeSessionChecker{}
	} else {
		sessions, err = NewLegacySessionChecker(base, LegacySessionOptions{TestMode: fixture, AllowHTTPTestEndpoint: fixture})
	}
	if err != nil {
		return nil, errors.New("notification session endpoint is unavailable")
	}
	var directory *LegacyDirectoryReader
	readerToken := os.Getenv("NOTIFICATION_LEGACY_READER_TOKEN")
	readerDeployment := os.Getenv("NOTIFICATION_SOURCE_DEPLOYMENT_ID")
	if readerToken != "" || readerDeployment != "" {
		directory, err = NewLegacyDirectoryReader(base, readerToken, readerDeployment, LegacySessionOptions{TestMode: fixture, AllowHTTPTestEndpoint: fixture})
		if err != nil {
			return nil, errors.New("notification legacy directory configuration is unavailable")
		}
	}
	var origins []string
	if raw := os.Getenv("NOTIFICATION_PLUGIN_ALLOWED_ORIGINS"); raw != "" {
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &origins) != nil || len(origins) > 128 {
			return nil, errors.New("notification plugin allowlist is invalid")
		}
	}
	var cidrs []netip.Prefix
	if raw := os.Getenv("NOTIFICATION_PLUGIN_ALLOWED_CIDRS"); raw != "" {
		var values []string
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &values) != nil || len(values) > 64 {
			return nil, errors.New("notification plugin network policy is invalid")
		}
		for _, value := range values {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, errors.New("notification plugin network policy is invalid")
			}
			cidrs = append(cidrs, prefix)
		}
	}
	pluginHTTP, err := NewSafePluginClient(PluginHTTPOptions{TestMode: fixture, AllowHTTPTestOrigin: fixture, AllowedOrigins: origins, AllowedCIDRs: cidrs})
	if err != nil {
		return nil, errors.New("notification plugin transport is unavailable")
	}
	options := ManagementOptions{TestMode: fixture, AllowHTTPTestOrigin: fixture}
	if directory != nil {
		options.TenantDirectory = directory
	}
	management, err := NewManagementService(store, store, store, store, store, sessions, pluginHTTP, protector, privacy, options)
	if err != nil {
		return nil, errors.New("notification management is unavailable")
	}
	bootstrap, err := defaultSMTPBootstrapConfigFromEnv(os.Getenv)
	if err != nil {
		return nil, errors.New("notification default SMTP bootstrap configuration is invalid")
	}
	if err := management.BootstrapDefaultSMTP(context.Background(), bootstrap); err != nil {
		return nil, errors.New("notification default SMTP bootstrap failed")
	}
	validator := &TargetValidator{Instances: store, Plugins: store}
	planner := &SourceResolver{Groups: store, Projections: store, ValidateTarget: validator.Validate}
	if directory != nil {
		planner.Members = directory
	}
	core := &Core{Store: store, Planner: planner, ValidateTarget: validator.Validate}
	api := &NotificationHTTPHandler{Verifier: management, Core: core, Store: store, Management: NewManagementHTTPHandler(management), Ready: privacy.Check}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: pluginHTTP, Runtimes: store}})
	if err != nil {
		return nil, errors.New("notification worker is unavailable")
	}
	if err := worker.Start(context.Background()); err != nil {
		return nil, errors.New("notification worker could not start")
	}
	return &Service{api: api, store: store, worker: worker}, nil
}

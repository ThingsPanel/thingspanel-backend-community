package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func setSourceBridgeEnv(t *testing.T, enabled, baseURL string) {
	t.Helper()
	t.Setenv(notificationSourceBridgeEnabledEnv, enabled)
	t.Setenv(notificationEncoreBaseURLEnv, baseURL)
	t.Setenv(notificationSourceDeploymentIDEnv, "deployment-env-test")
	t.Setenv(notificationSourceBearerTokenEnv, strings.Repeat("s", 32))
	t.Setenv(notificationProjectionBearerEnv, strings.Repeat("p", 32))
}

func stopSourceRelayOption(t *testing.T, application *Application) {
	t.Helper()
	if application == nil || application.ServiceManager == nil || len(application.ServiceManager.services) != 1 {
		t.Fatal("notification source relay was not registered")
	}
	service, ok := application.ServiceManager.services[0].(*NotificationSourceRelayService)
	if !ok {
		t.Fatal("unexpected registered source relay service")
	}
	if err := service.Stop(); err != nil {
		t.Fatalf("stop source relay option: %v", err)
	}
}

func TestNotificationSourceRelayFromEnvDefaultsDisabledAndStillRegistersGuard(t *testing.T) {
	previous, hadPrevious := os.LookupEnv(notificationSourceBridgeEnabledEnv)
	if err := os.Unsetenv(notificationSourceBridgeEnabledEnv); err != nil {
		t.Fatal("unset bridge flag")
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv(notificationSourceBridgeEnabledEnv, previous)
		} else {
			_ = os.Unsetenv(notificationSourceBridgeEnabledEnv)
		}
	})
	application, err := NewApplication(WithNotificationSourceRelayFromEnv())
	if err != nil {
		t.Fatalf("unconfigured old installation failed composition: %v", err)
	}
	stopSourceRelayOption(t, application)
}

func TestNotificationSourceRelayFromEnvRequiresStrictFlagAndTLSOrigins(t *testing.T) {
	for _, raw := range []string{"TRUE", " true", "1", ""} {
		t.Run("flag_"+strings.ReplaceAll(raw, " ", "space"), func(t *testing.T) {
			setSourceBridgeEnv(t, raw, "https://core.example.test")
			if _, err := NewApplication(WithNotificationSourceRelayFromEnv()); err == nil {
				t.Fatal("invalid enabled flag was accepted")
			}
		})
	}
	setSourceBridgeEnv(t, "true", "http://127.0.0.1:8080")
	if _, err := NewApplication(WithNotificationSourceRelayFromEnv()); err == nil {
		t.Fatal("production source bridge accepted HTTP origin")
	}
}

func TestNotificationSourceRelayFromEnvConstructsWithSeparateCredentials(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	setSourceBridgeEnv(t, "true", server.URL)
	application, err := NewApplication(WithNotificationSourceRelayFromEnv())
	if err != nil {
		t.Fatalf("configure TLS source bridge: %v", err)
	}
	defer stopSourceRelayOption(t, application)
	registered, ok := application.ServiceManager.services[0].(*NotificationSourceRelayService)
	if !ok || registered.bridge == nil || !registered.bridge.Enabled() {
		t.Fatal("source bridge was not constructed and enabled")
	}
	if registered.bridge.DeploymentID() != "deployment-env-test" {
		t.Fatal("source deployment identity was not applied")
	}
}

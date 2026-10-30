package pluginmgr

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestK8SRunner_ReconcileNginxSmoke(t *testing.T) {
	if os.Getenv("CONNECTOR_E2E_USE_K8S") != "1" {
		t.Skip("set CONNECTOR_E2E_USE_K8S=1 to run the real K8S smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ns := os.Getenv("CONNECTOR_E2E_K8S_NAMESPACE")
	if ns == "" {
		ns = "connector-k8s-smoke"
	}

	image := os.Getenv("CONNECTOR_E2E_K8S_IMAGE")
	if image == "" {
		image = "nginx:latest"
	}

	runner := NewK8SRunner(os.Getenv("CONNECTOR_E2E_KUBECONFIG"))
	runner.DefaultNamespace = ns
	runner.StatusPollInterval = time.Second
	runner.StatusPollTimeout = 2 * time.Minute

	instance := ConnectorInstance{
		ID:                 newID(),
		ConnectorRuntimeID: newID(),
		DeviceConnectorID:  newID(),
		Namespace:          ns,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	runtime := ConnectorRuntime{
		ID:                instance.ConnectorRuntimeID,
		DeviceConnectorID: instance.DeviceConnectorID,
		Image:             image,
		HTTPPort:          80,
		HealthCheckPath:   "/",
		EnvVars: map[string]string{
			"CONNECTOR_SMOKE": "true",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if ns == "connector-k8s-smoke" {
		defer func() {
			_, _ = runner.kubectl(context.Background(), "delete", "namespace", ns, "--ignore-not-found=true")
		}()
	}
	defer func() {
		_ = runner.Delete(context.Background(), instance.ID)
	}()

	status, err := runner.Reconcile(ctx, instance, runtime)
	if err != nil {
		t.Fatalf("reconcile nginx connector runtime: %v; status=%+v", err, status)
	}
	if status.Phase != PhaseRunning {
		t.Fatalf("expected phase %s, got %s: %+v", PhaseRunning, status.Phase, status)
	}
	if !status.ProcessHealthy {
		t.Fatalf("expected processHealthy=true: %+v", status)
	}
	if !strings.Contains(status.Message, "httpAddress=") {
		t.Fatalf("expected status message to include httpAddress: %q", status.Message)
	}
}

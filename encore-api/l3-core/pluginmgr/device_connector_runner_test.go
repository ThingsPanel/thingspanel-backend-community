package pluginmgr

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ─── FakeRunner tests ────────────────────────────────────────────────────────

func TestFakeRunner_Reconcile_Success(t *testing.T) {
	runner := NewFakeRunner()

	instance := ConnectorInstance{
		ID:                 "inst-001",
		ConnectorRuntimeID: "rt-001",
		DeviceConnectorID:  "dc-001",
		Namespace:          "connector-instances",
	}
	runtime := ConnectorRuntime{
		ID:                "rt-001",
		DeviceConnectorID: "dc-001",
		Image:             "registry.example.com/xiaomi-plug:1.0.0",
		HTTPPort:          9001,
	}

	status, err := runner.Reconcile(context.Background(), instance, runtime)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("expected phase %q, got %q", PhaseRunning, status.Phase)
	}
	if !status.ProcessHealthy {
		t.Error("expected processHealthy=true")
	}
	if status.ConnectorInstanceID != instance.ID {
		t.Errorf("expected connectorInstanceID=%q, got %q", instance.ID, status.ConnectorInstanceID)
	}
	if status.UpdatedAt.IsZero() {
		t.Error("expected non-zero updatedAt")
	}
}

func TestFakeRunner_Reconcile_FailImage(t *testing.T) {
	runner := NewFakeRunner()
	runner.FailImage = "bad-image:latest"

	instance := ConnectorInstance{ID: "inst-002"}
	runtime := ConnectorRuntime{
		ID:    "rt-002",
		Image: "bad-image:latest",
	}

	status, err := runner.Reconcile(context.Background(), instance, runtime)
	if err == nil {
		t.Fatal("expected error for bad image, got nil")
	}
	if status.Phase != PhaseError {
		t.Errorf("expected phase %q, got %q", PhaseError, status.Phase)
	}
	if status.ProcessHealthy {
		t.Error("expected processHealthy=false on error")
	}
	if status.Message == "" {
		t.Error("expected non-empty error message in status")
	}
}

func TestFakeRunner_Reconcile_DifferentImages_Selective(t *testing.T) {
	runner := NewFakeRunner()
	runner.FailImage = "bad-image:latest"

	// Good image should succeed
	good := ConnectorInstance{ID: "inst-good"}
	goodRuntime := ConnectorRuntime{ID: "rt-good", Image: "good-image:1.0"}
	statusGood, err := runner.Reconcile(context.Background(), good, goodRuntime)
	if err != nil {
		t.Fatalf("good image: unexpected error: %v", err)
	}
	if statusGood.Phase != PhaseRunning {
		t.Errorf("good image: expected Running, got %q", statusGood.Phase)
	}

	// Bad image should fail
	bad := ConnectorInstance{ID: "inst-bad"}
	badRuntime := ConnectorRuntime{ID: "rt-bad", Image: "bad-image:latest"}
	statusBad, err := runner.Reconcile(context.Background(), bad, badRuntime)
	if err == nil {
		t.Fatal("bad image: expected error, got nil")
	}
	if statusBad.Phase != PhaseError {
		t.Errorf("bad image: expected Error, got %q", statusBad.Phase)
	}
}

func TestFakeRunner_Delete(t *testing.T) {
	runner := NewFakeRunner()

	instance := ConnectorInstance{ID: "inst-003"}
	runtime := ConnectorRuntime{ID: "rt-003", Image: "img:1.0"}

	// First reconcile (start)
	_, err := runner.Reconcile(context.Background(), instance, runtime)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Then delete
	if err := runner.Delete(context.Background(), instance.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Logs should show Stopped phase
	logs, err := runner.Logs(context.Background(), instance.ID, LogOptions{})
	if err != nil {
		t.Fatalf("logs after delete: %v", err)
	}
	if len(logs) == 0 {
		t.Error("expected at least one log line after delete")
	}
}

func TestFakeRunner_Logs_UnknownInstance(t *testing.T) {
	runner := NewFakeRunner()

	_, err := runner.Logs(context.Background(), "nonexistent-instance", LogOptions{})
	if err == nil {
		t.Error("expected error for unknown instance, got nil")
	}
}

func TestFakeRunner_StartupDelay(t *testing.T) {
	runner := NewFakeRunner()
	runner.StartupDelay = 5 * time.Millisecond

	instance := ConnectorInstance{ID: "inst-delay"}
	runtime := ConnectorRuntime{ID: "rt-delay", Image: "img:1.0"}

	start := time.Now()
	status, err := runner.Reconcile(context.Background(), instance, runtime)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("reconcile with delay: %v", err)
	}
	if elapsed < runner.StartupDelay {
		t.Errorf("expected at least %v delay, elapsed %v", runner.StartupDelay, elapsed)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("expected Running after delay, got %q", status.Phase)
	}
}

func TestFakeRunner_Reconcile_CancelledContext(t *testing.T) {
	runner := NewFakeRunner()
	runner.StartupDelay = 10 * time.Second // long delay

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	instance := ConnectorInstance{ID: "inst-cancel"}
	runtime := ConnectorRuntime{ID: "rt-cancel", Image: "img:1.0"}

	_, err := runner.Reconcile(ctx, instance, runtime)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestFakeRunner_FakeHTTPAddress(t *testing.T) {
	runner := NewFakeRunner()
	runner.FakeHTTPAddress = "10.0.0.5:9001"

	addr := runner.FakeHTTPAddressFor("any-instance-id")
	if addr != "10.0.0.5:9001" {
		t.Errorf("expected custom address, got %q", addr)
	}
}

func TestFakeRunner_DefaultFakeHTTPAddress(t *testing.T) {
	runner := NewFakeRunner()
	// FakeHTTPAddress is already set to "127.0.0.1:9001" by NewFakeRunner

	addr := runner.FakeHTTPAddressFor("any")
	if addr == "" {
		t.Error("expected non-empty default HTTP address")
	}
}

// ─── Phase constant coverage ─────────────────────────────────────────────────

func TestPhaseConstants(t *testing.T) {
	phases := []string{PhasePending, PhaseStarting, PhaseRunning, PhaseStopped, PhaseError}
	for _, p := range phases {
		if p == "" {
			t.Errorf("phase constant must not be empty")
		}
	}
}

// ─── K8SRunner manifest tests ───────────────────────────────────────────────

func TestK8SRunner_ManifestsUseInstanceIDForSelectorsAndLabels(t *testing.T) {
	runner := NewK8SRunner("")
	instance := ConnectorInstance{
		ID:                "inst-1234567890abcdef",
		DeviceConnectorID: "dc-001",
	}
	runtime := ConnectorRuntime{
		DeviceConnectorID: "dc-001",
		Image:             "registry.example.com/xiaomi-plug:1.0.0",
		HTTPPort:          9001,
		EnvVars:           map[string]string{"LOG_LEVEL": "info"},
	}
	name := runner.resourceName(instance.ID)
	ns := "connector-instances"

	cm, err := runner.buildConfigMapManifest(name, ns, instance, runtime)
	if err != nil {
		t.Fatalf("build ConfigMap manifest: %v", err)
	}
	secret := runner.buildSecretManifest(name, ns, instance)
	deploy, err := runner.buildDeploymentManifest(name, ns, instance, runtime)
	if err != nil {
		t.Fatalf("build Deployment manifest: %v", err)
	}
	svc := runner.buildServiceManifest(name, ns, instance, runtime)

	for label, manifest := range map[string]string{
		"configmap":  string(cm),
		"secret":     string(secret),
		"deployment": string(deploy),
		"service":    string(svc),
	} {
		if !strings.Contains(manifest, "device-connector-instance: "+instance.ID) {
			t.Fatalf("%s manifest must label/select by instance ID %q; manifest:\n%s", label, instance.ID, manifest)
		}
	}
	if strings.Contains(string(svc), "device-connector-instance: "+name) {
		t.Fatalf("service selector must not use resource name %q as instance label; manifest:\n%s", name, string(svc))
	}
}

// ─── DeviceConnectorService + FakeRunner integration ─────────────────────────

func TestDeviceConnectorService_CreateInstance_FakeRunner_Success(t *testing.T) {
	repo := newNoopRepository()
	bridge := newNoopBridge()
	fakeRunner := NewFakeRunner()
	svc := NewDeviceConnectorService(repo, bridge, fakeRunner)

	ctx := context.Background()

	// Verify service is constructed with correct dependencies
	if svc.repo == nil {
		t.Fatal("expected non-nil repo")
	}
	if svc.bridge == nil {
		t.Fatal("expected non-nil bridge")
	}
	if svc.runner == nil {
		t.Fatal("expected non-nil runner")
	}

	// Verify runner is the fake runner
	if _, ok := svc.runner.(*FakeRunner); !ok {
		t.Error("expected runner to be *FakeRunner")
	}

	// Verify FakeRunner is callable without a K8S cluster
	instance := ConnectorInstance{ID: "e2e-inst-001", ConnectorRuntimeID: "rt-001", DeviceConnectorID: "dc-001"}
	runtime := ConnectorRuntime{ID: "rt-001", DeviceConnectorID: "dc-001", Image: "xiaomi-plug:1.0", HTTPPort: 9001}

	status, err := svc.runner.Reconcile(ctx, instance, runtime)
	if err != nil {
		t.Fatalf("FakeRunner.Reconcile: %v", err)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("expected Running, got %q", status.Phase)
	}
}

func TestDeviceConnectorService_CreateInstance_FakeRunner_Failure(t *testing.T) {
	fakeRunner := NewFakeRunner()
	fakeRunner.FailImage = "broken:latest"
	svc := NewDeviceConnectorService(newNoopRepository(), newNoopBridge(), fakeRunner)

	instance := ConnectorInstance{ID: "fail-inst"}
	runtime := ConnectorRuntime{ID: "fail-rt", Image: "broken:latest"}

	status, err := svc.runner.Reconcile(context.Background(), instance, runtime)
	if err == nil {
		t.Fatal("expected error from broken image")
	}
	if status.Phase != PhaseError {
		t.Errorf("expected Error phase, got %q", status.Phase)
	}
	if status.Message == "" {
		t.Error("expected non-empty failure message")
	}
}

// ─── ConnectorEvent reason constants coverage ─────────────────────────────────

func TestEventReasonConstants(t *testing.T) {
	reasons := []string{
		ReasonStarted,
		ReasonStopped,
		ReasonImagePullFailed,
		ReasonHeartbeatMissed,
		ReasonBridgeProjectionFailed,
		ReasonDisconnectCallbackSent,
		ReasonReconcileError,
	}
	for _, r := range reasons {
		if r == "" {
			t.Errorf("event reason constant must not be empty")
		}
	}
}

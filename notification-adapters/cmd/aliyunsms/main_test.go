package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adapter "encore.app/notification-adapters/aliyunsms"
)

func TestLoadAppConfigDefaultsReportWorkerOff(t *testing.T) {
	env := map[string]string{"NOTIFICATION_PLUGIN_TOKEN": "fixture-plugin-token"}
	config, err := loadAppConfig(envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if config.reportWorker || config.pluginListen != defaultPluginListen || config.internalListen != defaultInternalListen {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	components, err := prepareRuntime(config)
	if err != nil || components.worker != nil || components.ledger != nil || components.provider == nil {
		t.Fatalf("components=%+v err=%v", components, err)
	}
	if components.provider.Manifest().Capabilities.DeliveryReceipts {
		t.Fatal("receipt capability must remain disabled")
	}
}

func TestLoadAndPrepareConfiguredWorkerUsesPrivateFixtureFiles(t *testing.T) {
	env := workerEnv(t)
	config, err := loadAppConfig(envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	components, err := prepareRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	if !config.reportWorker || components.provider == nil || components.worker == nil || components.ledger == nil {
		t.Fatalf("configured worker did not prepare: config=%+v components=%+v", config, components)
	}
	if components.provider.Manifest().Capabilities.DeliveryReceipts {
		t.Fatal("worker configuration alone must not advertise unverified queue payload support")
	}
	key, err := readPrivateFile(env["ALIYUN_SMS_LEDGER_KEY_FILE"], 32)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	if _, err := adapter.OpenSendLedger(config.ledgerPath, key); err == nil {
		t.Fatal("runtime ledger was not process exclusive")
	}
	if info, err := os.Stat(filepath.Dir(config.ledgerPath)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("ledger parent mode=%v err=%v", info.Mode().Perm(), err)
	}
	if err := components.ledger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReportWorkerConfigurationFailsClosedBeforeServing(t *testing.T) {
	env := workerEnv(t)
	cases := []struct {
		name   string
		change func(map[string]string)
	}{
		{name: "bad HTTPS endpoint", change: func(e map[string]string) { e["ALIYUN_SMS_MNS_ENDPOINT"] = "http://123456.mns.cn-hangzhou.aliyuncs.com" }},
		{name: "host outside MNS allowlist", change: func(e map[string]string) { e["ALIYUN_SMS_MNS_ENDPOINT"] = "https://example.invalid" }},
		{name: "core origin outside allowlist", change: func(e map[string]string) { e["ALIYUN_SMS_CORE_ALLOWED_ORIGINS"] = "https://other.example" }},
		{name: "missing queue auth", change: func(e map[string]string) { e["ALIYUN_SMS_MNS_ACCESS_KEY_SECRET"] = "" }},
		{name: "readiness not loopback", change: func(e map[string]string) { e["ALIYUN_SMS_INTERNAL_LISTEN"] = "0.0.0.0:5005" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copyEnv := cloneEnv(env)
			tc.change(copyEnv)
			if _, err := loadAppConfig(envLookup(copyEnv)); err == nil {
				t.Fatal("invalid worker configuration was accepted")
			}
		})
	}
}

func TestPrivateConfigFilesRequireRegular0600AndNoSymlinks(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(loose, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(loose, 32); err == nil {
		t.Fatal("mode 0644 key file was accepted")
	}
	target := filepath.Join(dir, "target")
	writePrivate(t, target, []byte("fixture"))
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(link, 32); err == nil {
		t.Fatal("symlink secret file was accepted")
	}
}

func TestStaticReceiptRouteMustMatchExactLedgerInstance(t *testing.T) {
	routes, err := adapter.NewStaticReceiptRoutes([]adapter.ReceiptRoute{{TenantID: "tenant-a", InstanceID: "instance-a", CoreURL: "https://core.example", ReceiptToken: "token-a"}})
	if err != nil {
		t.Fatal(err)
	}
	record := adapter.SendLedgerRecord{TenantID: "tenant-a", InstanceID: "instance-a"}
	if route, err := routes.ResolveReceiptRoute(context.Background(), record); err != nil || route.ReceiptToken != "token-a" {
		t.Fatalf("route=%+v err=%v", route, err)
	}
	record.InstanceID = "instance-b"
	if _, err := routes.ResolveReceiptRoute(context.Background(), record); err == nil {
		t.Fatal("receipt route crossed instance scope")
	}
}

func TestReceiptRouteConfigRejectsDuplicateKeysAndInvalidScope(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "receipt.token")
	writePrivate(t, tokenPath, []byte("fixture-token"))
	routesPath := filepath.Join(dir, "routes.json")
	allowed := map[string]bool{"https://core.example": true}
	for _, body := range []string{
		`[{"tenant_id":"tenant-a","tenant_id":"tenant-b","instance_id":"instance-a","core_origin":"https://core.example","receipt_token_file":"` + tokenPath + `"}]`,
		`[{"tenant_id":"tenant-a","instance_id":"` + strings.Repeat("i", 129) + `","core_origin":"https://core.example","receipt_token_file":"` + tokenPath + `"}]`,
	} {
		writePrivate(t, routesPath, []byte(body))
		if _, err := readOperatorRoutes(routesPath, allowed); err == nil {
			t.Fatalf("invalid route file accepted: %s", body)
		}
	}
}

func TestPrivateReadinessAndShutdownOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pluginListener := listenLoopback(t)
	internalListener := listenLoopback(t)
	worker := &blockingWorker{entered: make(chan struct{}), stopped: make(chan struct{})}
	ledger := &trackedLedger{}
	config := appConfig{pluginToken: "fixture-token", reportWorker: true, pollInterval: time.Second}
	components := runtimeComponents{provider: adapter.New(), worker: worker, ledger: ledger}
	done := make(chan error, 1)
	go func() {
		done <- runServersWithHandlers(ctx, config, components, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), pluginListener, internalListener, time.Second, time.Second)
	}()
	readyURL := "http://" + internalListener.Addr().String() + "/internal/ready"
	deadline := time.Now().Add(2 * time.Second)
	var ready map[string]bool
	for time.Now().Before(deadline) {
		response, err := http.Get(readyURL)
		if err == nil {
			_ = json.NewDecoder(response.Body).Decode(&ready)
			_ = response.Body.Close()
			if ready["ready"] {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready["ready"] || !ready["report_worker_configured"] {
		t.Fatalf("private readiness response=%v", ready)
	}
	select {
	case <-worker.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not shut down")
	}
	select {
	case <-worker.stopped:
	default:
		t.Fatal("worker did not stop before return")
	}
	if !ledger.closed.Load() {
		t.Fatal("ledger was not closed after worker and HTTP drain")
	}
}

func TestHTTPDrainFailureDoesNotCloseLedger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pluginListener := listenLoopback(t)
	internalListener := listenLoopback(t)
	ledger := &trackedLedger{}
	entered := make(chan struct{})
	release := make(chan struct{})
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := http.Get("http://" + pluginListener.Addr().String() + "/block")
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	components := runtimeComponents{provider: adapter.New(), ledger: ledger}
	config := appConfig{pluginToken: "fixture-token"}
	done := make(chan error, 1)
	go func() {
		done <- runServersWithHandlers(ctx, config, components, handler, pluginListener, internalListener, 100*time.Millisecond, 20*time.Millisecond)
	}()
	// Ensure the request reaches the handler after the listener is served.
	select {
	case <-entered:
	case <-time.After(time.Second):
		cancel()
		close(release)
		t.Fatal("blocking request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failed HTTP drain returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("bounded shutdown did not return")
	}
	if ledger.closed.Load() {
		t.Fatal("ledger closed while an HTTP handler could still use it")
	}
	close(release)
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("fixture request did not finish")
	}
}

func TestWorkerLoopBacksOffAfterErrorAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &countingWorker{err: errors.New("fixture")}
	done := make(chan struct{})
	go func() {
		reportWorkerLoop(ctx, worker, time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for worker.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(40 * time.Millisecond)
	if worker.calls.Load() != 1 {
		t.Fatalf("worker busy-looped: calls=%d", worker.calls.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker loop did not stop")
	}
}

type blockingWorker struct {
	entered chan struct{}
	stopped chan struct{}
}

func (w *blockingWorker) RunOnce(ctx context.Context) error {
	select {
	case <-w.entered:
	default:
		close(w.entered)
	}
	<-ctx.Done()
	close(w.stopped)
	return ctx.Err()
}

type countingWorker struct {
	calls atomic.Int32
	err   error
}

func (w *countingWorker) RunOnce(context.Context) error {
	w.calls.Add(1)
	return w.err
}

type trackedLedger struct{ closed atomic.Bool }

func (l *trackedLedger) Close() error {
	l.closed.Store(true)
	return nil
}

func workerEnv(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ledger.key")
	writePrivate(t, keyPath, []byte(strings.Repeat("k", 32)))
	tokenPath := filepath.Join(dir, "receipt.token")
	writePrivate(t, tokenPath, []byte("fixture-receipt-token"))
	routesPath := filepath.Join(dir, "routes.json")
	routes, err := json.Marshal([]map[string]string{{
		"tenant_id": "tenant-1", "instance_id": "instance-1",
		"core_origin": "https://core.example", "receipt_token_file": tokenPath,
	}})
	if err != nil {
		t.Fatal(err)
	}
	writePrivate(t, routesPath, routes)
	return map[string]string{
		"NOTIFICATION_PLUGIN_TOKEN":               "fixture-plugin-token",
		"ALIYUN_SMS_REPORT_WORKER_ENABLED":        "true",
		"ALIYUN_SMS_LEDGER_PATH":                  filepath.Join(dir, "private", "send-ledger.bin"),
		"ALIYUN_SMS_LEDGER_KEY_FILE":              keyPath,
		"ALIYUN_SMS_ACCOUNT_ID":                   "123456",
		"ALIYUN_SMS_REGION":                       "cn-hangzhou",
		"ALIYUN_SMS_MNS_QUEUE_NAME":               "Alicom-Queue-1-SmsReport",
		"ALIYUN_SMS_MNS_ENDPOINT":                 "https://123456.mns.cn-hangzhou.aliyuncs.com",
		"ALIYUN_SMS_MNS_ACCESS_KEY_ID":            "fixture-ak",
		"ALIYUN_SMS_MNS_ACCESS_KEY_SECRET":        "fixture-mns-secret",
		"ALIYUN_SMS_RECEIPT_ROUTES_FILE":          routesPath,
		"ALIYUN_SMS_CORE_ALLOWED_ORIGINS":         "https://core.example",
		"ALIYUN_SMS_REPORT_POLL_INTERVAL_SECONDS": "1",
		"ALIYUN_SMS_INTERNAL_LISTEN":              "127.0.0.1:5005",
	}
}

func writePrivate(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
}

func envLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func cloneEnv(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

package notification

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestWorkerCrashChild(t *testing.T) {
	if os.Getenv("NOTIFICATION_WORKER_CHILD") != "1" {
		return
	}
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.Host != "127.0.0.1" || cfg.Port != 25543 || cfg.Database != "notification_test" || cfg.User != "notification_test" {
		t.Fatal("worker child requires the isolated loopback PostgreSQL fixture")
	}
	schema := os.Getenv("NOTIFICATION_WORKER_SCHEMA")
	if len(schema) != len("notification_test_")+32 || schema[:len("notification_test_")] != "notification_test_" {
		t.Fatal("worker child requires a parent-owned isolated notification schema")
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal("worker child could not connect to the isolated schema")
	}
	store, err := NewStore(StoreOptions{DB: db, Protector: newTestSnapshotProtector()})
	if err != nil {
		t.Fatal("worker child could not construct Store")
	}
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("worker child could not construct fixture plugin client")
	}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Concurrency: 1, BatchSize: 1, PluginTimeout: 20 * time.Second, FinishTimeout: time.Second, LeaseFor: 22 * time.Second})
	if err != nil {
		t.Fatal("worker child could not construct worker")
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal("worker child cycle failed")
	}
}

func TestWorkerProcessKillAfterProviderSideEffectRecoversUnknownWithoutResend(t *testing.T) {
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN is required for isolated PostgreSQL process recovery test")
	}
	store, db := newMigratedTestStore(t)
	schema := ""
	if err := db.QueryRowContext(context.Background(), `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal("read test-owned schema")
	}
	requestSeen := make(chan struct{})
	release := make(chan struct{})
	var httpCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls.Add(1)
		select {
		case <-requestSeen:
		default:
			close(requestSeen)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delivery_id":"ignored","outcome":"accepted","receipt_expected":false}`))
	}))
	defer server.Close()
	defer close(release)
	instanceID := createWorkerFixtureInstance(t, store, "worker-process-crash", server.URL)
	accepted := submitWorkerFixtures(t, store, "worker-process-crash", instanceID, 1)

	command := exec.Command(os.Args[0], "-test.run=^TestWorkerCrashChild$")
	command.Env = append(os.Environ(), "NOTIFICATION_WORKER_CHILD=1", "NOTIFICATION_WORKER_SCHEMA="+schema)
	var childOutput bytes.Buffer
	command.Stdout = &childOutput
	command.Stderr = &childOutput
	if err := command.Start(); err != nil {
		t.Fatal("start isolated worker child")
	}
	childDone := make(chan error, 1)
	go func() { childDone <- command.Wait() }()
	select {
	case <-requestSeen:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		<-childDone
		t.Fatalf("child worker did not reach fixture HTTP side effect: %s", childOutput.String())
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("force-stop worker after provider side effect: %v", err)
	}
	if err := <-childDone; err == nil {
		t.Fatal("force-stopped child exited successfully")
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE notification_deliveries SET lease_expires_at=$3 WHERE tenant_id=$1 AND id=$2 AND dispatch_status='sending'`, "worker-process-crash", accepted.DeliveryIDs[0], time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatal("expire only the process fixture delivery lease")
	}
	if recovered, err := store.RecoverExpiredClaimsBounded(context.Background(), time.Now().UTC(), 10); err != nil || recovered != 1 {
		t.Fatalf("recover killed worker claim: count=%d err=%v", recovered, err)
	}
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal("construct fixture plugin client")
	}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Concurrency: 1, BatchSize: 1, PluginTimeout: time.Second, FinishTimeout: time.Second, LeaseFor: 3 * time.Second})
	if err != nil {
		t.Fatal("construct recovery worker")
	}
	if cycle, err := worker.RunOnce(context.Background()); err != nil || cycle.Claimed != 0 {
		t.Fatalf("recovered unknown was claimed again: cycle=%+v err=%v", cycle, err)
	}
	var dispatchStatus, deliveryStatus string
	if err := db.QueryRowContext(context.Background(), `SELECT dispatch_status,delivery_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "worker-process-crash", accepted.DeliveryIDs[0]).Scan(&dispatchStatus, &deliveryStatus); err != nil {
		t.Fatal("read recovered killed-worker delivery")
	}
	if dispatchStatus != "unknown" || deliveryStatus != "unknown" || httpCalls.Load() != 1 {
		t.Fatalf("process crash was retried or not marked unknown: dispatch=%q delivery=%q HTTP calls=%d", dispatchStatus, deliveryStatus, httpCalls.Load())
	}
}

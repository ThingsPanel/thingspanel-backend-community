package app

import (
	"context"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"project/internal/automationbridge"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type schedulerLogObserver struct {
	warnings atomic.Int32
	leaked   atomic.Bool
	token    string
}

func (*schedulerLogObserver) Levels() []logrus.Level { return logrus.AllLevels }
func (o *schedulerLogObserver) Fire(e *logrus.Entry) error {
	if e.Level <= logrus.WarnLevel {
		o.warnings.Add(1)
	}
	text, _ := e.String()
	if strings.Contains(text, o.token) {
		o.leaked.Store(true)
	}
	return nil
}

func TestNativeAutomationSchedulerLifecycle(t *testing.T) {
	mode := os.Getenv("TP_TEST_NATIVE_SCHEDULER")
	if mode == "" {
		for _, mode := range []string{"legacy", "native", "native-external", "invalid"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestNativeAutomationSchedulerLifecycle$")
			cmd.Env = append(os.Environ(), "TP_TEST_NATIVE_SCHEDULER="+mode)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", mode, err, out)
			}
		}
		return
	}
	token := strings.Repeat("t", 32)
	var calls atomic.Int32
	third, canceled := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/internal/automation/tick" || string(body) != "{}" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get(automationbridge.DeploymentHeader) != "deployment-a" || r.Header.Get(automationbridge.TenantHeader) != "" {
			t.Error("native scheduler lost deployment-scoped HTTP contract")
		}
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(503)
			return
		}
		if n == 3 {
			once.Do(func() { close(third) })
			<-r.Context().Done()
			close(canceled)
			return
		}
		_, _ = w.Write([]byte(`{"replayed":0,"tick":{"schedule_materialized":0,"schedule_executed":0,"outbox_drained":0}}`))
	}))
	defer server.Close()
	executionMode := mode
	if mode == "native-external" || mode == "invalid" {
		executionMode = "native"
	}
	t.Setenv("GOTP_AUTOMATION_MODE", executionMode)
	source := "process"
	if mode == "native-external" {
		source = ""
	}
	if mode == "invalid" {
		source = "invalid"
	}
	t.Setenv("GOTP_AUTOMATION_SCHEDULER", source)
	t.Setenv("GOTP_AUTOMATION_BASE_URL", server.URL)
	t.Setenv("GOTP_AUTOMATION_TOKEN", token)
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment-a")
	app, err := NewApplication()
	if mode == "invalid" {
		if err == nil {
			t.Fatal("invalid scheduler source accepted")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	observer := &schedulerLogObserver{token: token}
	app.Logger.AddHook(observer)
	workers := 0
	for _, service := range app.ServiceManager.services {
		if scheduler, ok := service.(*nativeAutomationScheduler); ok {
			workers++
			scheduler.interval = 10 * time.Millisecond
		}
	}
	if mode == "legacy" || mode == "native-external" {
		if workers != 0 {
			t.Fatal("external/legacy mode started native scheduler")
		}
		return
	}
	if workers != 1 {
		t.Fatalf("registered schedulers=%d", workers)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := app.StartContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-third:
	case <-time.After(3 * time.Second):
		app.Shutdown()
		t.Fatal("scheduler did not retry failed HTTP tick and continue")
	}
	if observer.warnings.Load() < 1 {
		t.Fatal("tick HTTP failure was not observable")
	}
	cancel()
	stopped := make(chan struct{})
	go func() { app.Shutdown(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown failed to cancel in-flight tick")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP tick context not canceled")
	}
	before := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != before {
		t.Fatal("scheduler continued after shutdown")
	}
	if observer.leaked.Load() {
		t.Fatal("scheduler logged service credential")
	}
}

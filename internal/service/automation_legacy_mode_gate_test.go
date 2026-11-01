package service

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/initialize"
	"project/internal/automationbridge"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
)

type automationGateSQLCounter struct {
	logger.Interface
	calls atomic.Int64
}

func (c *automationGateSQLCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	c.calls.Add(1)
	c.Interface.Trace(ctx, begin, fc, err)
}

type automationGateRedisCounter struct{ calls atomic.Int64 }

func (*automationGateRedisCounter) DialHook(next redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("fixture network disabled")
	}
}
func (c *automationGateRedisCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(context.Context, redis.Cmder) error {
		c.calls.Add(1)
		return errors.New("fixture Redis operation blocked")
	}
}
func (c *automationGateRedisCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error {
		c.calls.Add(1)
		return errors.New("fixture Redis pipeline blocked")
	}
}

func TestLegacyAutomationModeGateSubprocess(t *testing.T) {
	scenario := os.Getenv("TP_TEST_LEGACY_GATE")
	if scenario == "" {
		for _, name := range []string{"native", "invalid-native", "unknown", "legacy"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestLegacyAutomationModeGateSubprocess$")
			cmd.Env = append(os.Environ(), "TP_TEST_LEGACY_GATE="+name)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", name, err, out)
			}
		}
		return
	}
	t.Setenv("GOTP_AUTOMATION_MODE", scenario)
	t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("s", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "fixture")
	if scenario == "invalid-native" {
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "")
	}
	if scenario == "legacy" {
		if err := requireLegacyAutomation(); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GOTP_AUTOMATION_MODE", "native")
		if err := requireLegacyAutomation(); err != nil {
			t.Fatal("frozen legacy mode changed")
		}
		_, err := (&Automate{}).AutomateActionExecute("", nil, nil, "")
		if err == nil {
			t.Fatal("legacy action semantics changed")
		}
		return
	}
	// All global query bindings live only in this subprocess. The Redis hook
	// counts calls without connecting to any real server.
	sqlCounter := &automationGateSQLCounter{Interface: logger.Discard}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: sqlCounter})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	query.SetDefault(db)
	global.DB = db
	redisCounter := &automationGateRedisCounter{}
	global.REDIS = redis.NewClient(&redis.Options{Addr: "fixture.invalid:1", MaxRetries: 0})
	global.REDIS.AddHook(redisCounter)
	defer global.REDIS.Close()
	if err := db.Exec("SELECT 1").Error; err != nil || sqlCounter.calls.Load() == 0 {
		t.Fatal("SQL observer did not see positive control")
	}
	_ = global.REDIS.Ping(context.Background()).Err()
	if redisCounter.calls.Load() == 0 {
		t.Fatal("Redis observer did not see positive control")
	}
	sqlCounter.calls.Store(0)
	redisCounter.calls.Store(0)
	var output bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&output)
	defer logrus.SetOutput(oldOutput)
	a, task := &Automate{}, &AutomateTask{}
	assertUnavailable := func(err error) {
		t.Helper()
		var code *errcode.Error
		if !errors.As(err, &code) || code.Code != 100001 {
			t.Fatalf("expected unavailable, got %v", err)
		}
	}
	assertUnavailable(a.Execute(nil, AutomateFromExt{}))
	assertUnavailable(a.ExecuteRun(initialize.AutomateExecteParams{}))
	assertUnavailable(a.SceneAutomateExecute("", nil, nil))
	assertUnavailable(a.ActiveSceneExecute("", ""))
	_, err = a.AutomateActionExecute("", nil, nil, "")
	assertUnavailable(err)
	assertUnavailable(task.OnceTaskExecute())
	assertUnavailable(task.PeriodicTaskExecute())
	assertUnavailable(task.TaskAutomationExecute(""))
	if sqlCounter.calls.Load() != 0 || redisCounter.calls.Load() != 0 {
		t.Fatalf("blocked path touched SQL=%d Redis=%d", sqlCounter.calls.Load(), redisCounter.calls.Load())
	}
	if strings.Contains(output.String(), strings.Repeat("s", 32)) {
		t.Fatal("configuration secret leaked")
	}
	if scenario == "native" {
		if output.Len() != 0 {
			t.Fatalf("native gate logged: %s", output.String())
		}
		t.Setenv("GOTP_AUTOMATION_MODE", "legacy")
		assertUnavailable(task.OnceTaskExecute())
	} else {
		if strings.Count(output.String(), "invalid automation configuration") != 1 {
			t.Fatalf("invalid config log: %s", output.String())
		}
		t.Setenv("GOTP_AUTOMATION_MODE", "legacy")
		if _, err := automationbridge.ReadConfig(); err != nil {
			t.Fatal("corrected configuration not accepted")
		}
		if err := requireLegacyAutomation(); err != nil {
			t.Fatal(err)
		}
	}
}

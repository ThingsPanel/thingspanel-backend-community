package croninit

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
	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
)

type cronGateSQLCounter struct {
	logger.Interface
	calls atomic.Int64
}

func (c *cronGateSQLCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	c.calls.Add(1)
	c.Interface.Trace(ctx, begin, fc, err)
}

type cronGateRedisCounter struct{ calls atomic.Int64 }

func (*cronGateRedisCounter) DialHook(next redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("fixture network disabled")
	}
}
func (c *cronGateRedisCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(context.Context, redis.Cmder) error {
		c.calls.Add(1)
		return errors.New("fixture Redis operation blocked")
	}
}
func (c *cronGateRedisCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error {
		c.calls.Add(1)
		return errors.New("fixture Redis pipeline blocked")
	}
}

func TestRegisteredAutomationCallbacksFailClosedSubprocess(t *testing.T) {
	mode := os.Getenv("TP_TEST_CRON_GATE")
	if mode == "" {
		for _, scenario := range []string{"native", "invalid"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestRegisteredAutomationCallbacksFailClosedSubprocess$")
			cmd.Env = append(os.Environ(), "TP_TEST_CRON_GATE="+scenario)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", scenario, err, out)
			}
		}
		return
	}
	t.Setenv("GOTP_AUTOMATION_MODE", "native")
	t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("s", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "fixture")
	if mode == "invalid" {
		t.Setenv("GOTP_AUTOMATION_BASE_URL", "")
	}
	sqlCounter := &cronGateSQLCounter{Interface: logger.Discard}
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
	redisCounter := &cronGateRedisCounter{}
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
	old := logrus.StandardLogger().Out
	logrus.SetOutput(&output)
	defer logrus.SetOutput(old)
	scheduler := cron.New()
	registerAutomationJobs(scheduler)
	entries := scheduler.Entries()
	if len(entries) != 2 {
		t.Fatalf("registered jobs=%d", len(entries))
	}
	for _, entry := range entries {
		entry.Job.Run()
	}
	if sqlCounter.calls.Load() != 0 || redisCounter.calls.Load() != 0 {
		t.Fatalf("cron claimed SQL=%d Redis=%d", sqlCounter.calls.Load(), redisCounter.calls.Load())
	}
	if strings.Contains(output.String(), strings.Repeat("s", 32)) {
		t.Fatal("credential leaked in log")
	}
	if mode == "native" && output.Len() != 0 {
		t.Fatalf("native cron logged: %s", output.String())
	}
	if mode == "invalid" && strings.Count(output.String(), "invalid automation configuration") != 1 {
		t.Fatalf("invalid cron log: %s", output.String())
	}
}

func TestAutomationCronErrorReporting(t *testing.T) {
	var output bytes.Buffer
	old := logrus.StandardLogger().Out
	logrus.SetOutput(&output)
	defer logrus.SetOutput(old)
	reportAutomationTaskError("once", nil)
	reportAutomationTaskError("once", errcode.New(100001))
	if output.Len() != 0 {
		t.Fatal("unavailable cron produced repeated error log")
	}
	reportAutomationTaskError("once", errors.New("fixture failure"))
	if !strings.Contains(output.String(), "fixture failure") {
		t.Fatal("unexpected cron failure not reported")
	}
}

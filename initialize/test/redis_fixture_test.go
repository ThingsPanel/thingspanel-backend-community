package test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"project/initialize"
	"project/pkg/global"
)

// These cache tests must never load a developer's database or Redis config.
// One private process is shared because the production caches are singletons;
// each test starts with an empty private database and seeds its own records.
func TestMain(m *testing.M) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		os.Exit(m.Run()) // individual tests explicitly report the missing fixture
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	address := listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	_ = listener.Close()
	cmd := exec.Command(binary, "--bind", "127.0.0.1", "--port", port, "--save", "", "--appendonly", "no")
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if client.Ping(context.Background()).Err() == nil {
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	code := 1
	if ready {
		global.REDIS = client
		alarmCache = initialize.NewAlarmCache()
		cache = initialize.NewAutomateCache()
		code = m.Run()
	} else {
		fmt.Fprintln(os.Stderr, "isolated cache-test Redis did not become ready")
	}
	_ = client.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	os.Exit(code)
}

func resetPrivateCache(t *testing.T) {
	t.Helper()
	if global.REDIS == nil {
		t.Skip("redis-server unavailable; isolated cache integration test not run")
	}
	if err := global.REDIS.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
}

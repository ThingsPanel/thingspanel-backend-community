package service

import (
	"context"
	"io"
	"net"
	"os/exec"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func startBrokerServiceTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable for isolated broker service test")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve isolated broker Redis port")
	}
	address := listener.Addr().String()
	_, port, err := net.SplitHostPort(address)
	_ = listener.Close()
	if err != nil {
		t.Fatal("read isolated broker Redis port")
	}
	cmd := exec.Command(binary, "--bind", "127.0.0.1", "--port", port, "--protected-mode", "no", "--save", "", "--appendonly", "no")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal("start isolated broker Redis process")
	}
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: 0, DialTimeout: 100 * time.Millisecond})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping(context.Background()).Err() == nil {
			t.Cleanup(func() {
				_ = client.Close()
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			return client
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = client.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatal("isolated broker Redis did not become ready")
	return nil
}

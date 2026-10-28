package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fixture":
		err = runFixtureCommand(os.Args[2:])
	case "test":
		err = runTestCommand(os.Args[2:])
	case "ledger":
		err = runLedgerCommand(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mqtt-community fixture [flags] | test [flags] | ledger RAW.ndjson ASSERTIONS.json")
}

func runFixtureCommand(args []string) error {
	flags := flag.NewFlagSet("fixture", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:18084", "loopback fixture address")
	tokenPath := flags.String("token-file", "/tmp/tp-mqtt-20261019/build/fixture.token", "service token file; created mode 0600 if absent")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireLoopback(*listen); err != nil {
		return err
	}
	token, err := loadOrCreateToken(*tokenPath)
	if err != nil {
		return err
	}
	f := newFixture(token)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/authenticate", f.authenticate)
	mux.HandleFunc("GET /__test/stats", f.stats)
	mux.HandleFunc("POST /__test/restart-broker", f.requestBrokerRestart)
	mux.HandleFunc("POST /__test/barrier/release", f.releaseBarrier)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	fmt.Printf("fixture ready at http://%s/v1/authenticate; token file %s (value not printed)\n", *listen, *tokenPath)
	return server.ListenAndServe()
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("fixture must bind to a loopback address")
	}
	return nil
}

func loadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(strings.TrimSpace(string(b))) < 32 {
			return "", errors.New("existing fixture token must contain at least 32 bytes")
		}
		return strings.TrimSpace(string(b)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	value := hex.EncodeToString(raw[:])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(f, value+"\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return value, nil
}

func runTestCommand(args []string) error {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	addr := flags.String("broker", "127.0.0.1:32884", "Mosquitto plugin broker address")
	fixtureURL := flags.String("fixture", "http://127.0.0.1:18084", "fixed-grant fixture base URL")
	rawPath := flags.String("raw", "/tmp/tp-mqtt-20261019/build/mqtt-community-qa.raw.ndjson", "path for raw stage evidence NDJSON")
	smokeOnly := flags.Bool("smoke-only", false, "run one minimal MQTT ingress case (sanitizer smoke)")
	stressMessages := flags.Int("stress-messages", 0, "send and verify this many QoS 1 ingress messages")
	stressReconnects := flags.Int("stress-reconnects", 0, "perform this many authenticated connect/disconnect cycles")
	stressReconnectConcurrency := flags.Int("stress-reconnect-concurrency", 1, "maximum concurrent reconnects, capped at 16")
	stressReport := flags.String("stress-report", "", "write a JSON stress count and timing report")
	tickProbe := flags.Bool("tick-probe", false, "compare an idle delayed-auth CONNECT with one during low-rate MQTT traffic")
	refreshRevocationOnly := flags.Bool("refresh-revocation-only", false, "verify a denied lease refresh stops delivery to an existing subscription")
	refreshReport := flags.String("refresh-report", "", "write focused lease-refresh revocation timing and status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	r := &runner{addr: *addr, fixtureURL: strings.TrimRight(*fixtureURL, "/"), timeout: 5 * time.Second, rawPath: *rawPath}
	var runErr error
	if *refreshRevocationOnly {
		started := time.Now()
		runErr = r.testRefreshRevocation()
		status := "PASS"
		failure := ""
		if runErr != nil {
			status, failure = "FAIL", runErr.Error()
		}
		if *refreshReport != "" {
			report := map[string]any{"status": status, "started_at": started.UTC(), "duration_ms": time.Since(started).Milliseconds(), "failure": failure}
			if data, err := json.MarshalIndent(report, "", "  "); err == nil {
				_ = os.WriteFile(*refreshReport, append(data, '\n'), 0o600)
			}
			_ = r.writeFixtureStats(*refreshReport + ".owner.json")
		}
	} else if *tickProbe {
		runErr = r.runTickProbe(*stressReport)
	} else if *smokeOnly {
		runErr = r.runSmoke()
	} else if *stressMessages > 0 || *stressReconnects > 0 {
		runErr = r.runStress(*stressMessages, *stressReconnects, *stressReconnectConcurrency, *stressReport)
	} else {
		runErr = r.run()
	}
	if runErr != nil {
		return runErr
	}
	fmt.Println("PASS: all live MQTT checks completed")
	return nil
}

func waitHTTP(ctx context.Context, addr string) error {
	var last error
	for {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s: %w (last dial: %v)", addr, ctx.Err(), last)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

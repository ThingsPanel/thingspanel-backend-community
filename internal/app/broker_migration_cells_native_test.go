package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"project/internal/brokerstate"
	"project/internal/processor"
	"project/internal/query"
	"project/internal/service"
	"project/internal/storage"
	"project/internal/uplink"
	"project/pkg/global"

	redis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	migrationCellChildConfigEnv = "TP_MIGRATION_CELL_CHILD_CONFIG"
	migrationCellReadyEnv       = "TP_MIGRATION_CELL_READY_FILE"
	migrationCellChildRunEnv    = "TP_MIGRATION_CELL_CHILD_RUN"
	migrationCellChildRunValue  = "1"
)

var migrationCellSchemaPattern = regexp.MustCompile(`^mqtt_(native_e2e|qa)_[a-zA-Z0-9_]+$`)

// MigrationCellRuntimeConfig is written to a 0600 file because it contains
// broker credentials and the path to the private PostgreSQL DSN file.
type MigrationCellRuntimeConfig struct {
	DSNFile         string `json:"dsn_file"`
	Schema          string `json:"schema"`
	RedisAddr       string `json:"redis_addr"`
	BrokerAddr      string `json:"broker_addr"`
	Cell            string `json:"cell"`
	Instance        string `json:"instance"`
	ClientID        string `json:"client_id"`
	ServiceUsername string `json:"service_username"`
	ServicePassword string `json:"service_password"`
}

type migrationCellBrokerConfig struct {
	Binary, Plugin, OwnerURL, TokenPath, Root string
	Cell, Instance                            string
}

// TestBrokerMigrationCellRuntimeChild is invoked as a separate test process
// for each runtime cell. It deliberately returns immediately in the parent.
func TestBrokerMigrationCellRuntimeChild(t *testing.T) {
	if os.Getenv(migrationCellChildRunEnv) != migrationCellChildRunValue {
		t.Skip("private migration runtime child only")
	}
	configPath := os.Getenv(migrationCellChildConfigEnv)
	readyPath := os.Getenv(migrationCellReadyEnv)
	if configPath == "" || readyPath == "" {
		t.Fatal("invalid private runtime configuration")
	}
	cfg, err := readMigrationCellRuntimeConfig(configPath)
	if err != nil {
		t.Fatal("invalid private runtime configuration")
	}
	if err := runMigrationCellRuntimeChild(cfg, readyPath); err != nil {
		// Never emit underlying DSN, Redis address, credential, or transport errors.
		t.Fatal("private cell runtime startup failed")
	}
}

// startMigrationCellRuntime launches the current Go test binary in a fresh
// process, giving each cell independent Viper and package globals.
func startMigrationCellRuntime(t *testing.T, cfg MigrationCellRuntimeConfig) (*exec.Cmd, string, func()) {
	t.Helper()
	if err := validateMigrationCellRuntimeConfig(cfg); err != nil {
		t.Fatalf("invalid migration cell fixture configuration: %v", err)
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "runtime.json")
	readyPath := filepath.Join(dir, "ready")
	logPath := filepath.Join(dir, "runtime.log")
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal("cannot encode private runtime configuration")
	}
	if err := writeMigrationCellPrivateFile(configPath, encoded); err != nil {
		t.Fatal("cannot write private runtime configuration")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal("cannot create private runtime log")
	}
	command := exec.Command(os.Args[0], "-test.run=^TestBrokerMigrationCellRuntimeChild$")
	command.Env = appendMigrationCellEnv(migrationCellBaseEnv(), map[string]string{
		migrationCellChildRunEnv:    migrationCellChildRunValue,
		migrationCellChildConfigEnv: configPath,
		migrationCellReadyEnv:       readyPath,
	})
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal("cannot start isolated cell runtime")
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		_ = logFile.Close()
		close(done)
	}()
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) || command.Process == nil {
			return
		}
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	waitMigrationCellReady(t, readyPath, logPath, done, cfg.Cell, cfg.Instance)
	return command, logPath, stop
}

// startMigrationCellBroker starts a native Mosquitto process bound only to
// loopback. Docker and shared/production brokers are intentionally unsupported.
func startMigrationCellBroker(t *testing.T, cfg migrationCellBrokerConfig) (string, string, func()) {
	t.Helper()
	if cfg.Binary == "" || cfg.Plugin == "" || cfg.OwnerURL == "" || cfg.TokenPath == "" || cfg.Root == "" || cfg.Cell == "" || cfg.Instance == "" {
		t.Fatal("incomplete private native broker configuration")
	}
	if err := validateMigrationPrivateFile(cfg.TokenPath); err != nil {
		t.Fatal("broker token file must be private mode 0600")
	}
	owner, err := url.Parse(cfg.OwnerURL)
	if err != nil || owner.Scheme != "http" || owner.User != nil || owner.RawQuery != "" || owner.Fragment != "" || validateMigrationLoopbackAddr(owner.Host) != nil {
		t.Fatal("owner endpoint must be a loopback test server")
	}
	port := freeNativeE2EPort(t)
	addr := fmt.Sprintf("tcp://127.0.0.1:%d", port)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mosquitto.conf")
	logPath := filepath.Join(dir, "mosquitto.log")
	config := fmt.Sprintf("listener %d 127.0.0.1\nallow_anonymous false\nsys_interval 1\npersistence false\nplugin %s\nlog_dest file %s\nlog_type all\n", port, cfg.Plugin, logPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal("cannot write private Mosquitto config")
	}
	command := exec.Command(cfg.Binary, "-c", configPath)
	command.Dir = cfg.Root
	command.Env = appendMigrationCellEnv(migrationCellBaseEnv(), map[string]string{
		"TP_BROKER_OWNER_URL":     cfg.OwnerURL,
		"TP_BROKER_TOKEN_FILE":    cfg.TokenPath,
		"TP_BROKER_CELL":          cfg.Cell,
		"TP_BROKER_INSTANCE":      cfg.Instance,
		"TP_BROKER_ROUTE_FENCING": "1",
		"TP_BROKER_MAX_PENDING":   "32",
		"TP_BROKER_MAX_SESSIONS":  "64",
		"DYLD_LIBRARY_PATH":       "/tmp/tp-mqtt-20261019/build/install/lib",
	})
	if err := command.Start(); err != nil {
		t.Fatal("cannot start private native Mosquitto")
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) || command.Process == nil {
			return
		}
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	waitMigrationCellListener(t, port, done)
	return addr, logPath, stop
}

func runMigrationCellRuntimeChild(cfg MigrationCellRuntimeConfig, readyPath string) error {
	dsnBytes, err := os.ReadFile(cfg.DSNFile)
	if err != nil {
		return errors.New("private database configuration unavailable")
	}
	dsn := strings.TrimSpace(string(dsnBytes))
	if dsn == "" {
		return errors.New("private database configuration unavailable")
	}
	db, err := gorm.Open(postgres.Open(nativeE2ESearchPath(dsn, cfg.Schema)), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return errors.New("private database unavailable")
	}
	dbSQL, err := db.DB()
	if err != nil {
		return errors.New("private database unavailable")
	}
	defer dbSQL.Close()
	dbSQL.SetMaxOpenConns(4)
	var database, schema string
	if err := db.Raw("SELECT current_database(), current_schema()").Row().Scan(&database, &schema); err != nil || database != "brokerqa" || schema != cfg.Schema {
		return errors.New("database isolation check failed")
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, MaxRetries: 0})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		_ = redisClient.Close()
		return errors.New("private Redis unavailable")
	}
	defer redisClient.Close()
	global.DB, global.REDIS, global.STATUS_REDIS = db, redisClient, redisClient
	global.TPSSEManager = global.NewSSEManager()
	query.SetDefault(db)
	setMigrationCellViper(cfg)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	storageInput := make(chan *storage.Message, 64)
	storageManager := storage.New(db, logger, storage.Config{ChannelBufferSize: 64, TelemetryBatchSize: 1, TelemetryFlushInterval: 250})
	storageCtx, storageCancel := context.WithCancel(context.Background())
	if err := storageManager.Start(storageCtx, storageInput); err != nil {
		storageCancel()
		return errors.New("storage runtime unavailable")
	}
	defer func() { _ = storageManager.Stop(3 * time.Second); storageCancel() }()

	bus := uplink.NewBus(uplink.BusConfig{BufferSize: 64}, logger)
	defer bus.Close()
	heartbeat := service.NewHeartbeatService(redisClient, logger)
	status := uplink.NewStatusUplink(uplink.StatusUplinkConfig{HeartbeatService: heartbeat, BrokerStateStore: brokerstate.New(db), InputProfile: "ingress_v1", Logger: logger})
	telemetry := uplink.NewTelemetryUplink(uplink.TelemetryUplinkConfig{Processor: processor.NewScriptProcessor(), StorageInput: storageInput, HeartbeatService: heartbeat, Logger: logger})
	flow := &UplinkServiceWrapper{UplinkManager: uplink.NewUplinkManager(uplink.UplinkManagerConfig{Bus: bus, TelemetryUplink: telemetry, StatusUplink: status, Logger: logger}), bus: bus, isEnabled: true, logger: logger}
	if err := flow.Start(); err != nil {
		return errors.New("uplink runtime unavailable")
	}
	defer flow.Stop()

	application := &Application{DB: db, Logger: logger, ServiceManager: NewServiceManager(), uplinkService: flow}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mqttService := &MQTTService{app: application, starting: true, startCancel: cancel}
	if err := mqttService.initMQTTAdapter(ctx); err != nil {
		return errors.New("MQTT adapter runtime unavailable")
	}
	defer mqttService.Stop()
	if err := WithDownlinkService()(application); err != nil {
		return errors.New("downlink runtime unavailable")
	}
	if application.downlinkService == nil {
		return errors.New("downlink runtime unavailable")
	}
	if err := application.downlinkService.Start(); err != nil {
		return errors.New("downlink runtime unavailable")
	}
	defer application.downlinkService.Stop()

	ready := []byte(cfg.Cell + "\n" + cfg.Instance + "\n")
	if err := writeMigrationCellReadyFile(readyPath, ready); err != nil {
		return errors.New("cannot signal runtime readiness")
	}
	signalCtx, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()
	<-signalCtx.Done()
	return nil
}

func setMigrationCellViper(cfg MigrationCellRuntimeConfig) {
	values := map[string]any{
		"mqtt.broker": cfg.BrokerAddr, "mqtt.user": cfg.ServiceUsername,
		"mqtt.pass": cfg.ServicePassword, "mqtt.client_id": cfg.ClientID,
		"mqtt.input_profile": "ingress_v1", "mqtt.broker_cell": cfg.Cell,
		"mqtt.broker_instance": cfg.Instance, "mqtt.runtime_instance": cfg.Instance, "mqtt.route_fencing": true,
		"mqtt.retained_profile": "live-ingress", "uplink.enable": true,
		"downlink.buffer_size": 64,
	}
	for key, value := range values {
		viper.Set(key, value)
	}
}

func validateMigrationCellRuntimeConfig(cfg MigrationCellRuntimeConfig) error {
	if cfg.DSNFile == "" || !migrationCellSchemaPattern.MatchString(cfg.Schema) || cfg.Cell == "" || cfg.Instance == "" || cfg.ClientID == "" || cfg.ServiceUsername == "" || cfg.ServicePassword == "" {
		return errors.New("incomplete runtime config")
	}
	if err := validateMigrationLoopbackAddr(cfg.RedisAddr); err != nil {
		return errors.New("Redis must be loopback")
	}
	if err := validateMigrationLoopbackBroker(cfg.BrokerAddr); err != nil {
		return errors.New("broker must be loopback")
	}
	return validateMigrationPrivateFile(cfg.DSNFile)
}

func readMigrationCellRuntimeConfig(path string) (MigrationCellRuntimeConfig, error) {
	var cfg MigrationCellRuntimeConfig
	if err := validateMigrationPrivateFile(path); err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := validateMigrationCellRuntimeConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func validateMigrationPrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("private file must be regular 0600")
	}
	return nil
}

func writeMigrationCellPrivateFile(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return validateMigrationPrivateFile(path)
}

func writeMigrationCellReadyFile(path string, contents []byte) error {
	tmpPath := path + ".tmp"
	if err := writeMigrationCellPrivateFile(tmpPath, contents); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return validateMigrationPrivateFile(path)
}

func validateMigrationLoopbackAddr(addr string) error {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid loopback port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("not a loopback IP")
	}
	return nil
}

func validateMigrationLoopbackBroker(addr string) error {
	u, err := url.Parse(addr)
	if err != nil || u.Scheme != "tcp" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("invalid broker address")
	}
	return validateMigrationLoopbackAddr(u.Host)
}

func waitMigrationCellReady(t *testing.T, readyPath, logPath string, done <-chan struct{}, cell, instance string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(readyPath); err == nil {
			if string(data) != cell+"\n"+instance+"\n" {
				t.Fatal("child runtime readiness identity mismatch")
			}
			return
		}
		select {
		case <-done:
			t.Fatalf("isolated cell runtime exited before readiness; log: %s", logPath)
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("isolated cell runtime readiness timed out")
}

func waitMigrationCellListener(t *testing.T, port int, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case <-done:
			t.Fatal("private native Mosquitto exited before listening")
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("private native Mosquitto listener timed out")
}

func appendMigrationCellEnv(base []string, values map[string]string) []string {
	filtered := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replace := values[key]; !replace {
			filtered = append(filtered, item)
		}
	}
	for key, value := range values {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func migrationCellBaseEnv() []string {
	var env []string
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "DYLD_LIBRARY_PATH", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

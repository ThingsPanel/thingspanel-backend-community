package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	mqttadapter "project/internal/adapter/mqttadapter"
	"project/internal/brokercontract"
	"project/internal/brokercontrol"
	"project/internal/brokerledger"
	"project/internal/query"
	"project/internal/uplink"
	"project/pkg/global"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	redis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	sigkillChildEnv = "TP_SIGKILL_CHILD"
	sigkillClientID = "mqtt-e2e-sigkill"
)

type sigkillAcceptResult struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
}

type sigkillHoldAcceptor struct {
	base       *brokerIngressAcceptor
	result     <-chan trackedAcceptResult
	resultPath string
	hold       bool
}

func (a *sigkillHoldAcceptor) Accept(ctx context.Context, message *uplink.DeviceMessage) error {
	if err := a.base.Accept(ctx, message); err != nil {
		return err
	}
	var result trackedAcceptResult
	select {
	case result = <-a.result:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for committed receipt result")
	}
	if result.err != nil {
		return result.err
	}
	if err := writeSigkillJSON(a.resultPath, sigkillAcceptResult{ID: result.id, Duplicate: result.duplicate}); err != nil {
		return err
	}
	if a.hold {
		select {}
	}
	return nil
}

// TestBrokerIngressNativeSIGKILLChild is invoked only as a private subprocess
// by TestBrokerIngressNativeReceiptSurvivesSIGKILLBeforeAck. In hold mode the
// adapter has committed its real receipt and deliberately cannot reach m.Ack.
func TestBrokerIngressNativeSIGKILLChild(t *testing.T) {
	if os.Getenv(sigkillChildEnv) != "1" {
		t.Skip("private child helper")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("TP_SIGKILL_DB_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open private QA schema")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("configure private QA schema pool")
	}
	sqlDB.SetMaxOpenConns(2)
	defer sqlDB.Close()
	query.SetDefault(db)
	global.DB = db
	redisClient := redis.NewClient(&redis.Options{Addr: os.Getenv("TP_SIGKILL_REDIS_ADDR"), MaxRetries: 0})
	defer redisClient.Close()
	global.REDIS = redisClient
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for key, value := range map[string]string{
		"mqtt.broker": os.Getenv("TP_SIGKILL_BROKER"), "mqtt.user": "mqtt-e2e-service", "mqtt.pass": "e2e-service-password",
		"mqtt.client_id": sigkillClientID, "mqtt.input_profile": "ingress_v1", "mqtt.broker_cell": nativeE2ECell,
		"mqtt.broker_instance": nativeE2ENode, "mqtt.retained_profile": "live-ingress", "mqtt.route_fencing": "true",
		"mqtt.runtime_instance": "mqtt-e2e-sigkill-backend",
	} {
		viper.Set(key, value)
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	bus := uplink.NewBus(uplink.BusConfig{BufferSize: 8}, logger)
	defer bus.Close()
	result := make(chan trackedAcceptResult, 1)
	base := mqttadapter.MQTTConfig{Broker: os.Getenv("TP_SIGKILL_BROKER"), Username: "mqtt-e2e-service", Password: "e2e-service-password", ClientID: sigkillClientID, ManualAck: true}
	var adapter *mqttadapter.Adapter
	hold := os.Getenv("TP_SIGKILL_MODE") == "hold"
	base.BeforeConnect = func(client mqtt.Client) error {
		adapter = mqttadapter.NewAdapter(bus, client, logger)
		adapter.SetContext(ctx)
		if err := adapter.ConfigureIngressFromConfig(); err != nil {
			return err
		}
		store := &trackedLedgerAccept{store: brokerledger.New(db), result: result}
		adapter.SetIngressAcceptor(&sigkillHoldAcceptor{base: &brokerIngressAcceptor{store: store}, result: result, resultPath: os.Getenv("TP_SIGKILL_RESULT_FILE"), hold: hold})
		adapter.RegisterIngressRoutes(client)
		return nil
	}
	client, err := mqttadapter.CreateMQTTClientContext(ctx, base, logger)
	if err != nil {
		t.Fatalf("connect SIGKILL child subscriber: %v", err)
	}
	defer client.Disconnect(250)
	if adapter == nil {
		t.Fatal("BeforeConnect did not construct the adapter")
	}
	if err := adapter.SubscribeAllContext(ctx, client); err != nil {
		t.Fatalf("subscribe SIGKILL child session: %v", err)
	}
	if err := os.WriteFile(os.Getenv("TP_SIGKILL_READY_FILE"), []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if hold {
		select {}
	}
	stopFile := os.Getenv("TP_SIGKILL_STOP_FILE")
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(stopFile); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func TestBrokerIngressNativeReceiptSurvivesSIGKILLBeforeAck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGKILL signal verification requires a Unix host")
	}
	if os.Getenv("TEST_MQTT_NATIVE_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_E2E=1 for isolated PG/real-broker SIGKILL test")
	}
	image := strings.TrimSpace(os.Getenv("TP_NATIVE_E2E_DOCKER_IMAGE"))
	if image == "" {
		t.Skip("set TP_NATIVE_E2E_DOCKER_IMAGE to the frozen Linux production image")
	}
	if image != frozenNativeDockerImage {
		t.Fatalf("SIGKILL smoke must use frozen image %s", frozenNativeDockerImage)
	}
	root := repoRootForNativeE2E(t)
	redisPath := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	if _, err := os.Stat(redisPath); err != nil {
		t.Skipf("Redis QA binary unavailable: %v", err)
	}
	db, schema, closeDB := nativeE2EPostgres(t)
	_ = closeDB // package query objects share the private pool for this process lifetime.
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	previousDB := global.DB
	previousDeviceQuery, previousCommandQuery := query.Device, query.CommandSetLog
	query.SetDefault(db)
	global.DB = db
	t.Cleanup(func() {
		global.DB = previousDB
		query.Device, query.CommandSetLog = previousDeviceQuery, previousCommandQuery
	})
	seedNativeE2EData(t, db)
	previousRedis, previousStatusRedis := global.REDIS, global.STATUS_REDIS
	redisClient, _ := startNativeE2ERedis(t, redisPath)
	global.REDIS, global.STATUS_REDIS = redisClient, redisClient
	t.Cleanup(func() { global.REDIS, global.STATUS_REDIS = previousRedis, previousStatusRedis })

	childDSN := nativeE2ESearchPath(nativeE2EQADataSource(t), schema)
	ownerToken := nativeE2ERandomID(t) + nativeE2ERandomID(t) + nativeE2ERandomID(t)
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), brokercontrol.Config{
		Tokens: []brokercontrol.TokenBinding{{Token: ownerToken, BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode}},
		Services: []brokercontrol.ServiceCredential{{
			Username: "mqtt-e2e-service", Password: "e2e-service-password", BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode,
			AllowedClientIDs: []string{sigkillClientID}, AllowedDeviceIDs: []string{nativeE2EDevice},
			Principal:     brokercontract.Principal{TenantID: nativeE2ETenant, DeviceID: "service-mqtt-e2e", Kind: brokercontract.PrincipalService},
			Policy:        brokercontract.Policy{Publish: []string{"devices/event/response/+/+", "devices/attributes/response/+/+"}, Subscribe: []string{brokercontract.IngressPrefix + "/#", brokercontract.StatePrefix + "/#"}, MaxPayload: brokercontract.MaxPayloadBytes, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress},
			PolicyVersion: "sigkill-native-v1", MappingVersion: "standard-v1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerServer := httptest.NewServer(owner.RuntimeHandler(brokercontrol.NewMigrationStore(db)))
	t.Cleanup(ownerServer.Close)
	tokenPath := filepath.Join(t.TempDir(), "plugin.token")
	if err := os.WriteFile(tokenPath, []byte(ownerToken), 0o600); err != nil {
		t.Fatal(err)
	}
	brokerAddr, brokerLog, stopBroker := startNativeDockerBroker(t, image, ownerServer.URL, tokenPath)
	t.Cleanup(stopBroker)

	work := t.TempDir()
	firstReady, firstResult := filepath.Join(work, "first.ready"), filepath.Join(work, "first.result")
	first := startSIGKILLSubscriber(t, childDSN, redisClient.Options().Addr, brokerAddr, firstReady, firstResult, "hold", filepath.Join(work, "first.log"))
	waitSIGKILLChildReady(t, first, firstReady)
	device := connectNativeE2EDevice(t, context.Background(), brokerAddr)
	publishNativeE2E(t, context.Background(), device, "devices/event/sigkill", []byte(`{"method":"sigkill","params":{"value":1}}`))
	var firstAccept sigkillAcceptResult
	waitNativeE2E(t, 12*time.Second, "child durable receipt before SIGKILL", func() bool {
		return readSigkillJSON(firstResult, &firstAccept)
	})
	if firstAccept.ID == "" || firstAccept.Duplicate {
		t.Fatalf("first child acceptance=%+v, want one new committed receipt", firstAccept)
	}
	var firstRow struct {
		ID            string  `gorm:"column:id"`
		Metadata      []byte  `gorm:"column:metadata"`
		State         string  `gorm:"column:state"`
		SourceEventID *string `gorm:"column:source_event_id"`
	}
	if err := db.Table("broker_ingress_receipts").Select("id,metadata,state,source_event_id").Where("id=?::uuid", firstAccept.ID).Take(&firstRow).Error; err != nil {
		t.Fatalf("read committed receipt before SIGKILL: %v", err)
	}
	if firstRow.SourceEventID != nil {
		t.Fatalf("pre-kill receipt source_event_id=%q, want SQL NULL for ingestion-id-only dedup", *firstRow.SourceEventID)
	}
	var firstMetadata map[string]any
	if err := json.Unmarshal(firstRow.Metadata, &firstMetadata); err != nil {
		t.Fatal(err)
	}
	firstIngestionID, _ := firstMetadata["ingestion_id"].(string)
	if firstRow.State != "pending" || firstIngestionID == "" {
		t.Fatalf("pre-kill receipt state=%q ingestion_id_present=%t", firstRow.State, firstIngestionID != "")
	}
	// The subscriber also receives a state frame, which it may ACK before the
	// device event arrives. Count only the ACK added by the replacement.
	ackBaseline := countNativeE2EAcks(brokerLog, sigkillClientID)
	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL only the test-owned first subscriber process: %v", err)
	}
	waitErr := waitSIGKILLChildExit(first, 5*time.Second)
	if waitErr == nil {
		t.Fatal("first subscriber exited without SIGKILL")
	}
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		t.Fatalf("first child exit error type=%T, want *exec.ExitError", waitErr)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || status.Signal() != syscall.SIGKILL {
		t.Fatalf("first child exit was %v, want SIGKILL", waitErr)
	}

	secondReady, secondResult := filepath.Join(work, "second.ready"), filepath.Join(work, "second.result")
	secondStop := filepath.Join(work, "second.stop")
	second := startSIGKILLSubscriber(t, childDSN, redisClient.Options().Addr, brokerAddr, secondReady, secondResult, "resume", filepath.Join(work, "second.log"), secondStop)
	t.Cleanup(func() {
		if !second.waited.Load() {
			_ = os.WriteFile(secondStop, []byte("stop\n"), 0o600)
			_ = terminateSIGKILLChild(second, 3*time.Second)
		}
	})
	waitSIGKILLChildReady(t, second, secondReady)
	var redelivery sigkillAcceptResult
	waitNativeE2E(t, 12*time.Second, "redelivered committed frame accepted by replacement process", func() bool {
		return readSigkillJSON(secondResult, &redelivery)
	})
	if redelivery.ID != firstAccept.ID || !redelivery.Duplicate {
		t.Fatalf("SIGKILL redelivery=%+v, want duplicate of %s", redelivery, firstAccept.ID)
	}
	var persistedMetadata map[string]any
	var storedMetadata []byte
	var metadataRow struct {
		Metadata []byte `gorm:"column:metadata"`
	}
	if err := db.Raw("SELECT metadata FROM broker_ingress_receipts WHERE id=?::uuid", redelivery.ID).Scan(&metadataRow).Error; err != nil {
		t.Fatal(err)
	}
	storedMetadata = metadataRow.Metadata
	if err := json.Unmarshal(storedMetadata, &persistedMetadata); err != nil {
		t.Fatal(err)
	}
	if got, _ := persistedMetadata["ingestion_id"].(string); got != firstIngestionID {
		t.Fatalf("redelivery ingestion_id=%q, original=%q", got, firstIngestionID)
	}
	waitNativeE2E(t, 5*time.Second, "replacement process MQTT ACK", func() bool { return countNativeE2EAcks(brokerLog, sigkillClientID) > ackBaseline })
	if err := os.WriteFile(secondStop, []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := terminateSIGKILLChild(second, 5*time.Second); err != nil {
		t.Fatalf("stop replacement subscriber: %v", err)
	}

	ledger := brokerledger.New(db)
	processor := uplink.NewReliableProcessor(logrus.New())
	worked, err := ledger.ProcessOne(context.Background(), processor.Process)
	if err != nil || !worked {
		t.Fatalf("process the redelivered receipt once: worked=%v err=%v", worked, err)
	}
	worked, err = ledger.ProcessOne(context.Background(), processor.Process)
	if err != nil || worked {
		t.Fatalf("second business processing pass must be empty: worked=%v err=%v", worked, err)
	}
	var receipts, eventRows, effects int64
	if err := db.Table("broker_ingress_receipts").Where("id=?::uuid AND state='processed'", firstAccept.ID).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("event_datas").Where("device_id=? AND identify='sigkill'", nativeE2EDevice).Count(&eventRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("broker_ingress_effects").Where("receipt_id=? AND effect_kind='automation'", firstAccept.ID).Count(&effects).Error; err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || eventRows != 1 || effects != 1 {
		t.Fatalf("post-kill result processed_receipts=%d business_events=%d effect_intents=%d, want 1 each", receipts, eventRows, effects)
	}
	if os.Getenv("TP_NATIVE_E2E_EVIDENCE_DIR") != "" {
		writeSIGKILLEvidence(t, filepath.Join(os.Getenv("TP_NATIVE_E2E_EVIDENCE_DIR"), "nosource"), brokerLog, first.logPath, second.logPath, firstAccept, redelivery, firstIngestionID)
	}
}

type sigkillChildProcess struct {
	cmd     *exec.Cmd
	done    chan error
	logPath string
	waited  atomic.Bool
}

func startSIGKILLSubscriber(t *testing.T, dsn, redisAddr, broker, ready, result, mode, logPath string, stopFile ...string) *sigkillChildProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestBrokerIngressNativeSIGKILLChild$", "-test.count=1"}
	cmd := exec.Command(executable, args...)
	cmd.Env = make([]string, 0, len(os.Environ())+6)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "TEST_BROKER_DATABASE_DSN=") || strings.HasPrefix(item, sigkillChildEnv+"=") || strings.HasPrefix(item, "TP_SIGKILL_") {
			continue
		}
		cmd.Env = append(cmd.Env, item)
	}
	cmd.Env = append(cmd.Env, sigkillChildEnv+"=1", "TP_SIGKILL_DB_DSN="+dsn, "TP_SIGKILL_REDIS_ADDR="+redisAddr, "TP_SIGKILL_BROKER="+broker, "TP_SIGKILL_READY_FILE="+ready, "TP_SIGKILL_RESULT_FILE="+result, "TP_SIGKILL_MODE="+mode)
	if len(stopFile) > 0 {
		cmd.Env = append(cmd.Env, "TP_SIGKILL_STOP_FILE="+stopFile[0])
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatalf("start subscriber test process: %v", err)
	}
	done := make(chan error, 1)
	child := &sigkillChildProcess{cmd: cmd, done: done, logPath: logPath}
	go func() {
		err := cmd.Wait()
		child.waited.Store(true)
		_ = log.Close()
		done <- err
	}()
	t.Cleanup(func() {
		if !child.waited.Load() {
			_ = child.cmd.Process.Kill()
			select {
			case <-child.done:
			case <-time.After(3 * time.Second):
			}
		}
	})
	return child
}

func waitSIGKILLChildReady(t *testing.T, child *sigkillChildProcess, path string) {
	t.Helper()
	waitNativeE2E(t, 12*time.Second, "subprocess subscription readiness", func() bool {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		select {
		case err := <-child.done:
			t.Fatalf("subscriber exited before ready: %v; child log: %s", err, nativeE2ETail(child.logPath))
		default:
		}
		return false
	})
}

func waitSIGKILLChildExit(child *sigkillChildProcess, timeout time.Duration) error {
	select {
	case err := <-child.done:
		return err
	case <-time.After(timeout):
		return errors.New("subscriber subprocess did not exit within timeout")
	}
}

func terminateSIGKILLChild(child *sigkillChildProcess, timeout time.Duration) error {
	if child.waited.Load() {
		return nil
	}
	select {
	case <-child.done:
		return nil
	case <-time.After(timeout):
		_ = child.cmd.Process.Kill()
		select {
		case <-child.done:
			return nil
		case <-time.After(timeout):
			return fmt.Errorf("child %d did not stop", child.cmd.Process.Pid)
		}
	}
}

func readSigkillJSON(path string, dst any) bool {
	body, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(body, dst) == nil
}

func writeSigkillJSON(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeSIGKILLEvidence(t *testing.T, dir, brokerLog, firstLog, secondLog string, first, second sigkillAcceptResult, ingestionID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{"broker.log": brokerLog, "subscriber-before-kill.log": firstLog, "subscriber-after-kill.log": secondLog} {
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read evidence %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metadata := map[string]any{"image_id": frozenNativeDockerImage, "broker_cell": nativeE2ECell, "client_id": sigkillClientID, "kill_signal": "SIGKILL", "before_kill": first, "redelivery": second, "ingestion_id": ingestionID, "source_event_id": nil, "dedup_basis": "ingestion_id", "business_effect_count": 1, "skipped": false}
	if err := writeSigkillJSON(filepath.Join(dir, "result.json"), metadata); err != nil {
		t.Fatal(err)
	}
}

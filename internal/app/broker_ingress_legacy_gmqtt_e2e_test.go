package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mqttadapter "project/internal/adapter/mqttadapter"
	"project/internal/brokerstate"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/query"
	"project/internal/service"
	"project/internal/storage"
	"project/internal/uplink"
	"project/mqtt"
	"project/pkg/global"

	mqttclient "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This opt-in experiment runs the fixed legacy GMQTT image in a private Docker
// network and exercises the legacy Adapter -> Uplink -> Storage -> SQL path.
// It does not implement the authenticated ingress acceptance stage, so that
// stage is explicitly reported as not applicable rather than inferred from PUBACK.
func TestBrokerIngressLegacyGMQTTOwnerPostgresHealth(t *testing.T) {
	if os.Getenv("TEST_MQTT_LEGACY_GMQTT_E2E") != "1" {
		t.Skip("set TEST_MQTT_LEGACY_GMQTT_E2E=1 to run isolated legacy GMQTT full-stack health")
	}
	root := repoRootForNativeE2E(t)
	db, schema, closeDB := nativeE2EPostgres(t)
	t.Cleanup(closeDB)
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	dbSQL.SetMaxOpenConns(4)
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	legacyRunID := nativeE2ERandomID(t)
	pgUser, pgPassword := startLegacyGMQTTDBRole(t, schema, legacyRunID)
	seedNativeE2EData(t, db)
	seedNativeE2ELoadDevicesLegacy(t, db, nativeE2ELoadConnections)

	previousDB, previousRedis, previousStatusRedis, previousSSE := global.DB, global.REDIS, global.STATUS_REDIS, global.TPSSEManager
	previousDeviceQuery, previousCommandQuery := query.Device, query.CommandSetLog
	query.SetDefault(db)
	global.DB = db
	redisPath := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	redisClient, stopRedis := startNativeE2ERedis(t, redisPath)
	global.REDIS, global.STATUS_REDIS = redisClient, redisClient
	global.TPSSEManager = global.NewSSEManager()
	t.Cleanup(func() {
		global.DB, global.REDIS, global.STATUS_REDIS, global.TPSSEManager = previousDB, previousRedis, previousStatusRedis, previousSSE
		query.Device, query.CommandSetLog = previousDeviceQuery, previousCommandQuery
		_ = redisClient.Close()
		stopRedis()
	})

	brokerURL, rootPassword, stopBroker := startLegacyGMQTTContainer(t, legacyRunID, pgUser, pgPassword)
	t.Cleanup(stopBroker)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	values := map[string]string{"mqtt.broker": brokerURL, "mqtt.user": "root", "mqtt.pass": rootPassword, "mqtt.client_id": "qa-legacy-adapter-" + legacyRunID, "mqtt.input_profile": "legacy"}
	previous := make(map[string]string, len(values))
	for key, value := range values {
		previous[key] = viper.GetString(key)
		viper.Set(key, value)
	}
	t.Cleanup(func() {
		for key, value := range previous {
			viper.Set(key, value)
		}
	})
	if err := mqtt.MqttInit(); err != nil {
		t.Fatalf("initialize legacy MQTT config: %v", err)
	}

	log := logrus.New()
	log.SetOutput(os.Stderr)
	log.SetLevel(logrus.WarnLevel)
	bus := uplink.NewBus(uplink.BusConfig{BufferSize: 256}, log)
	t.Cleanup(bus.Close)
	storageInput := make(chan *storage.Message, 256)
	interceptedStorage := make(chan *storage.Message, 256)
	storageManager := storage.New(db, log, storage.Config{ChannelBufferSize: 256, TelemetryBatchSize: 32, TelemetryFlushInterval: 250})
	storageCtx, storageCancel := context.WithCancel(context.Background())
	if err := storageManager.Start(storageCtx, storageInput); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageManager.Stop(5 * time.Second); storageCancel() })
	observed := make(chan legacyAdapterObservation, 8192)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-interceptedStorage:
				if message == nil {
					continue
				}
				if points, ok := message.Data.([]storage.TelemetryDataPoint); ok {
					for _, point := range points {
						if point.Key == "qa_id" {
							if value, ok := point.Value.(string); ok {
								observed <- legacyAdapterObservation{At: time.Now().UTC(), DeviceID: message.DeviceID, EventID: value}
							}
						}
					}
				}
				storageInput <- message
			}
		}
	}()
	status := uplink.NewStatusUplink(uplink.StatusUplinkConfig{HeartbeatService: service.NewHeartbeatService(redisClient, log), BrokerStateStore: brokerstate.New(db), InputProfile: "legacy", Logger: log})
	telemetry := uplink.NewTelemetryUplink(uplink.TelemetryUplinkConfig{Processor: processor.NewScriptProcessor(), StorageInput: interceptedStorage, HeartbeatService: service.NewHeartbeatService(redisClient, log), Logger: log})
	flow := &UplinkServiceWrapper{UplinkManager: uplink.NewUplinkManager(uplink.UplinkManagerConfig{Bus: bus, TelemetryUplink: telemetry, StatusUplink: status, Logger: log}), bus: bus, isEnabled: true, logger: log}
	if err := flow.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flow.Stop() })

	adapterOptions := mqttclient.NewClientOptions().AddBroker(brokerURL).SetClientID("qa-legacy-adapter-" + legacyRunID).SetUsername("root").SetPassword(rootPassword).SetCleanSession(true).SetAutoReconnect(false)
	adapterClient := mqttclient.NewClient(adapterOptions)
	if err := mqttadapter.WaitTokenContext(ctx, adapterClient.Connect(), 8*time.Second); err != nil {
		logPath := captureLegacyGMQTTLogs(legacyRunID)
		t.Fatalf("connect legacy MQTT Adapter: %v; isolated broker log: %s", err, logPath)
	}
	adapter := mqttadapter.NewAdapter(bus, adapterClient, log)
	adapter.SetContext(ctx)
	if err := adapter.ConfigureIngressFromConfig(); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SubscribeAllContext(ctx, adapterClient); err != nil {
		t.Fatalf("subscribe legacy Adapter topics: %v", err)
	}
	t.Cleanup(func() { mqttadapter.DisconnectMQTTClient(adapterClient, log) })

	runLegacyGMQTTLoad(t, ctx, db, brokerURL, observed, legacyRunID)
}

type legacyAdapterObservation struct {
	At                time.Time
	DeviceID, EventID string
}
type legacyLoadEvent struct {
	EventID               string    `json:"event_id"`
	PlannedDeviceID       string    `json:"planned_device_id"`
	ObservedDeviceIDs     []string  `json:"observed_device_ids,omitempty"`
	PayloadBytes          int       `json:"payload_bytes"`
	PlannedAt             time.Time `json:"planned_at"`
	SentAt                time.Time `json:"sent_at,omitempty"`
	BrokerACKAt           time.Time `json:"broker_ack_at,omitempty"`
	BrokerACKError        string    `json:"broker_ack_error,omitempty"`
	AdapterUplinkObserved bool      `json:"adapter_uplink_observed"`
	SQLRowCount           int       `json:"terminal_sql_row_count"`
}
type legacyLoadSummary struct {
	Kind                string        `json:"kind"`
	RunID               string        `json:"run_id"`
	Profile             string        `json:"profile"`
	Duration            time.Duration `json:"duration_ns"`
	Connections         int           `json:"connections"`
	Rate                int           `json:"rate_per_second"`
	Planned             int           `json:"planned"`
	Sent                int           `json:"sent"`
	BrokerACK           int           `json:"broker_ack"`
	IngressSeen         int           `json:"adapter_uplink_seen"`
	Accepted            any           `json:"accepted"`
	AcceptedStage       string        `json:"accepted_stage"`
	TerminalSQL         int           `json:"terminal_sql_rows"`
	EffectIntentRows    int           `json:"effect_intent_sql_rows"`
	SideEffectsExecuted string        `json:"side_effects_executed"`
	WrongDevice         int           `json:"wrong_device_identity_events"`
	DuplicateObserved   int           `json:"duplicate_adapter_uplink_deliveries"`
	ExtraSQL            int           `json:"extra_sql_rows"`
	MaxGeneratorLagMS   float64       `json:"max_generator_lag_ms"`
	LatencyMeasurement  string        `json:"latency_measurement"`
	Result              string        `json:"result"`
	StartedAt           time.Time     `json:"started_at"`
	FinishedAt          time.Time     `json:"finished_at"`
}

func runLegacyGMQTTLoad(t *testing.T, ctx context.Context, db *gorm.DB, broker string, observed <-chan legacyAdapterObservation, runID string) {
	t.Helper()
	duration, err := time.ParseDuration(os.Getenv("TP_LEGACY_GMQTT_LOAD_DURATION"))
	if err != nil || duration != 5*time.Second {
		t.Fatalf("TP_LEGACY_GMQTT_LOAD_DURATION must be 5s for this harness")
	}
	total := int(duration.Seconds()) * nativeE2ELoadRate
	events := make([]*legacyLoadEvent, total)
	clients := make([]mqttclient.Client, nativeE2ELoadConnections)
	for i := range clients {
		username, password := nativeE2ELoadVoucher(i)
		options := mqttclient.NewClientOptions().AddBroker(broker).SetClientID(fmt.Sprintf("legacy-device-%03d", i)).SetUsername(username).SetPassword(password).SetCleanSession(true).SetAutoReconnect(false)
		client := mqttclient.NewClient(options)
		if err := mqttadapter.WaitTokenContext(ctx, client.Connect(), 8*time.Second); err != nil {
			t.Fatalf("connect legacy fixture device %03d: %v", i, err)
		}
		clients[i] = client
	}
	defer func() {
		for _, client := range clients {
			if client != nil && client.IsConnected() {
				client.Disconnect(100)
			}
		}
	}()
	started := time.Now().UTC().Add(500 * time.Millisecond)
	for i := 0; i < total; i++ {
		eventID := fmt.Sprintf("%s-%06d", runID, i)
		events[i] = &legacyLoadEvent{EventID: eventID, PlannedDeviceID: nativeE2ELoadDeviceID(i % len(clients)), PayloadBytes: nativeLoadPayloadSize(i), PlannedAt: started.Add(time.Duration(i) * time.Second / nativeE2ELoadRate)}
	}
	summaryPath := filepath.Join("/tmp", "tp-mqtt-20261019", "build", "legacy-gmqtt-full-stack-load-"+runID+".ndjson")
	partialSummary := legacyLoadSummary{Kind: "summary", RunID: runID, Profile: "legacy-gmqtt-full-stack-health", Duration: duration, Connections: len(clients), Rate: nativeE2ELoadRate, Planned: total, Accepted: nil, AcceptedStage: "not applicable: legacy path has no durable acceptance transaction", SideEffectsExecuted: "unobserved", LatencyMeasurement: "not collected; SQL sampled after send drain", StartedAt: started}
	ledgerWritten := false
	defer func() {
		if ledgerWritten {
			return
		}
		partialSummary.Result, partialSummary.FinishedAt = "INCOMPLETE", time.Now().UTC()
		for _, event := range events {
			if !event.SentAt.IsZero() {
				partialSummary.Sent++
			}
			if !event.BrokerACKAt.IsZero() {
				partialSummary.BrokerACK++
			}
			if event.AdapterUplinkObserved {
				partialSummary.IngressSeen++
			}
			if event.SQLRowCount > 0 {
				partialSummary.TerminalSQL++
			}
		}
		if err := writeLegacyLoadLedger(summaryPath, events, partialSummary); err != nil {
			t.Errorf("write incomplete legacy load ledger: %v", err)
		}
	}()
	var wg sync.WaitGroup
sendLoop:
	for i, event := range events {
		if err := ctx.Err(); err != nil {
			event.BrokerACKError = "not sent; context canceled"
			break sendLoop
		}
		if wait := time.Until(event.PlannedAt); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				event.BrokerACKError = "not sent; context cancelled"
				break sendLoop
			case <-timer.C:
			}
		}
		payload := nativeLoadPayload(event.EventID, event.PayloadBytes, true)
		event.SentAt = time.Now().UTC()
		token := clients[i%len(clients)].Publish("devices/telemetry", 1, false, payload)
		wg.Add(1)
		go func(event *legacyLoadEvent, token mqttclient.Token) {
			defer wg.Done()
			if !token.WaitTimeout(8 * time.Second) {
				event.BrokerACKError = "PUBACK timeout"
				return
			}
			if err := token.Error(); err != nil {
				event.BrokerACKError = err.Error()
				return
			}
			event.BrokerACKAt = time.Now().UTC()
		}(event, token)
	}
	wg.Wait()
	byID := make(map[string]*legacyLoadEvent, total)
	for _, event := range events {
		byID[event.EventID] = event
	}
	observedCount := make(map[string]int, total)
	observeDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(observeDeadline) {
		select {
		case item := <-observed:
			if event := byID[item.EventID]; event != nil {
				observedCount[item.EventID]++
				event.AdapterUplinkObserved = true
				event.ObservedDeviceIDs = append(event.ObservedDeviceIDs, item.DeviceID)
			}
		case <-time.After(50 * time.Millisecond):
		}
		seen := 0
		for _, event := range events {
			if event.AdapterUplinkObserved {
				seen++
			}
		}
		if seen == total {
			break
		}
	}
	sqlCounts := make(map[string]int, total)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var rows []struct {
			DeviceID string `gorm:"column:device_id"`
			Value    string `gorm:"column:string_v"`
			Count    int    `gorm:"column:count"`
		}
		if err := db.Table("telemetry_datas").Select("device_id,string_v,count(*) AS count").Where("tenant_id=? AND device_id IN ? AND key='qa_id' AND string_v LIKE ?", nativeE2ETenant, legacyLoadDeviceIDs(), runID+"-%").Group("device_id,string_v").Scan(&rows).Error; err != nil {
			t.Fatalf("query legacy telemetry SQL: %v", err)
		}
		for _, row := range rows {
			if event := byID[row.Value]; event != nil && event.PlannedDeviceID == row.DeviceID {
				sqlCounts[row.Value] = row.Count
			}
		}
		all := true
		for _, event := range events {
			if sqlCounts[event.EventID] == 0 {
				all = false
				break
			}
		}
		if all {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, event := range events {
		event.SQLRowCount = sqlCounts[event.EventID]
	}
	finished := time.Now().UTC()
	summary := legacyLoadSummary{Kind: "summary", RunID: runID, Profile: "legacy-gmqtt-full-stack-health", Duration: duration, Connections: len(clients), Rate: nativeE2ELoadRate, Planned: total, Accepted: nil, AcceptedStage: "not applicable: legacy GMQTT -> Adapter path has no durable broker-ingress acceptance transaction; PUBACK is not acceptance", SideEffectsExecuted: "unobserved; legacy telemetry path has no transactional effect intent at the trusted receipt boundary", LatencyMeasurement: "not collected; SQL is sampled after send drain", StartedAt: started, FinishedAt: finished}
	for _, event := range events {
		if !event.SentAt.IsZero() {
			summary.Sent++
		}
		if !event.BrokerACKAt.IsZero() {
			summary.BrokerACK++
		}
		if event.AdapterUplinkObserved {
			summary.IngressSeen++
		}
		if event.SQLRowCount == 1 {
			summary.TerminalSQL++
		}
		if event.SQLRowCount > 1 {
			summary.ExtraSQL += event.SQLRowCount - 1
		}
		if len(event.ObservedDeviceIDs) > 1 {
			summary.DuplicateObserved += len(event.ObservedDeviceIDs) - 1
		}
		if len(event.ObservedDeviceIDs) > 0 {
			seen := map[string]bool{}
			for _, id := range event.ObservedDeviceIDs {
				seen[id] = true
			}
			if len(seen) != 1 || !seen[event.PlannedDeviceID] {
				summary.WrongDevice++
			}
		}
		lag := float64(event.SentAt.Sub(event.PlannedAt).Microseconds()) / 1000
		if lag > summary.MaxGeneratorLagMS {
			summary.MaxGeneratorLagMS = lag
		}
	}
	summary.EffectIntentRows = 0
	summary.Result = "INCOMPLETE"
	if summary.Sent == total && summary.BrokerACK == total && summary.IngressSeen == total && summary.TerminalSQL == total && summary.WrongDevice == 0 && summary.DuplicateObserved == 0 && summary.ExtraSQL == 0 {
		summary.Result = "PASS_HEALTH_ONLY"
	}
	if err := writeLegacyLoadLedger(summaryPath, events, summary); err != nil {
		t.Fatal(err)
	}
	ledgerWritten = true
	t.Logf("legacy GMQTT full stack result=%s planned=%d sent=%d broker_ack=%d adapter_uplink_seen=%d accepted=N/A terminal_sql=%d wrong_device=%d duplicate=%d extra_sql=%d report=%s", summary.Result, summary.Planned, summary.Sent, summary.BrokerACK, summary.IngressSeen, summary.TerminalSQL, summary.WrongDevice, summary.DuplicateObserved, summary.ExtraSQL, summaryPath)
	if summary.Result != "PASS_HEALTH_ONLY" {
		t.Fatalf("legacy GMQTT health run incomplete; report %s", summaryPath)
	}
}

func writeLegacyLoadLedger(path string, events []*legacyLoadEvent, summary legacyLoadSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(file)
	for _, event := range events {
		if err = enc.Encode(event); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err = enc.Encode(summary); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func legacyLoadDeviceIDs() []string {
	ids := make([]string, nativeE2ELoadConnections)
	for i := range ids {
		ids[i] = nativeE2ELoadDeviceID(i)
	}
	return ids
}

func seedNativeE2ELoadDevicesLegacy(t *testing.T, db *gorm.DB, count int) {
	t.Helper()
	rows := make([]model.Device, 0, count)
	for i := 0; i < count; i++ {
		username, password := nativeE2ELoadVoucher(i)
		voucher := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
		rows = append(rows, model.Device{ID: nativeE2ELoadDeviceID(i), TenantID: nativeE2ETenant, DeviceNumber: fmt.Sprintf("native-e2e-load-number-%03d", i), Voucher: voucher, IsEnabled: "enabled", ActivateFlag: "active", IsOnline: 0})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed legacy load devices: %v", err)
	}
}

func startLegacyGMQTTDBRole(t *testing.T, schema, runID string) (string, string) {
	t.Helper()
	dsnBytes, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := gorm.Open(postgres.Open(strings.TrimSpace(string(dsnBytes))), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	role := "gmqttqa_" + runID
	password := hex.EncodeToString(random)
	quotedSchema := `"` + schema + `"`
	if err = admin.Exec("CREATE ROLE " + role + " LOGIN PASSWORD '" + password + "' CONNECTION LIMIT 4").Error; err != nil {
		t.Fatalf("create isolated legacy GMQTT DB role: %v", err)
	}
	for _, stmt := range []string{"GRANT USAGE ON SCHEMA " + quotedSchema + " TO " + role, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quotedSchema + " TO " + role, "GRANT USAGE,SELECT,UPDATE ON ALL SEQUENCES IN SCHEMA " + quotedSchema + " TO " + role, "ALTER ROLE " + role + " SET search_path TO " + quotedSchema} {
		if err = admin.Exec(stmt).Error; err != nil {
			t.Fatalf("grant isolated legacy DB schema: %v", err)
		}
	}
	t.Cleanup(func() {
		cleanup, openErr := gorm.Open(postgres.Open(strings.TrimSpace(string(dsnBytes))), &gorm.Config{})
		if openErr == nil {
			_ = cleanup.Exec("DROP OWNED BY " + role).Error
			_ = cleanup.Exec("DROP ROLE IF EXISTS " + role).Error
			if sqlDB, e := cleanup.DB(); e == nil {
				_ = sqlDB.Close()
			}
		}
	})
	if sqlDB, e := admin.DB(); e == nil {
		_ = sqlDB.Close()
	}
	return role, password
}

func startLegacyGMQTTContainer(t *testing.T, runID, pgUser, pgPassword string) (string, string, func()) {
	t.Helper()
	image := "registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-gmqtt:v1.1.7"
	network := "qa-gmqtt-" + runID
	redisName := "qa-gmqtt-redis-" + runID
	brokerName := "qa-gmqtt-old-" + runID
	port := freeNativeE2EPort(t)
	rootPassword := nativeE2ERandomID(t) + nativeE2ERandomID(t)
	dir := t.TempDir()
	if out, err := exec.Command("docker", "network", "create", network).CombinedOutput(); err != nil {
		t.Fatalf("create isolated legacy Docker network: %v %s", err, out)
	}
	cleanup := func() {
		_ = captureLegacyGMQTTLogs(runID)
		_ = exec.Command("docker", "rm", "-f", brokerName).Run()
		_ = exec.Command("docker", "rm", "-f", redisName).Run()
		_ = exec.Command("docker", "network", "rm", network).Run()
	}
	t.Cleanup(cleanup)
	if out, err := exec.Command("docker", "run", "-d", "--rm", "--name", redisName, "--network", network, "--network-alias", "legacy-redis", "redis:7-alpine", "redis-server", "--save", "", "--appendonly", "no").CombinedOutput(); err != nil {
		t.Fatalf("start isolated legacy Redis: %v %s", err, out)
	}
	defaultConfig := `listeners:
  - address: ":1883"
mqtt:
  max_packet_size: 268435456
  server_receive_maximum: 1000
  shared_subscription_available: true
  wildcard_subscription_available: true
plugin_order:
  - thingspanel
log:
  level: info
  format: text
  dump_packet: false
`
	thingspanelConfig := fmt.Sprintf("db:\n  redis:\n    conn: legacy-redis:6379\n    db_num: 0\n    password: \"\"\n  psql:\n    psqladdr: host.docker.internal\n    psqlport: 35432\n    psqldb: ThingsPanel\n    psqluser: %s\n    psqlpass: %s\nmqtt:\n  broker: 127.0.0.1:1883\n  password: %s\n  plugin_password: %s\n", pgUser, pgPassword, rootPassword, nativeE2ERandomID(t))
	defaultPath, pluginPath := filepath.Join(dir, "default_config.yml"), filepath.Join(dir, "thingspanel.yml")
	if err := os.WriteFile(defaultPath, []byte(defaultConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(thingspanelConfig), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "-d", "--rm", "--name", brokerName, "--network", network, "-p", fmt.Sprintf("127.0.0.1:%d:1883", port), "-v", defaultPath + ":/gmqttd/default_config.yml:ro", "-v", pluginPath + ":/gmqttd/thingspanel.yml:ro", image}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("start isolated legacy GMQTT: %v %s", err, out)
	}
	addr := fmt.Sprintf("tcp://127.0.0.1:%d", port)
	waitNativeE2E(t, 20*time.Second, "legacy GMQTT isolated listener", func() bool {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	return addr, rootPassword, cleanup
}

func captureLegacyGMQTTLogs(runID string) string {
	path := filepath.Join("/tmp", "tp-mqtt-20261019", "build", "legacy-gmqtt-"+runID+".log")
	name := "qa-gmqtt-old-" + runID
	output, err := exec.Command("docker", "logs", name).CombinedOutput()
	if err != nil { return path }
	_ = os.WriteFile(path, output, 0600)
	return path
}

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mqttadapter "project/internal/adapter/mqttadapter"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/brokercontrol"
	"project/internal/brokerstate"
	"project/internal/downlink"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/query"
	"project/internal/service"
	"project/internal/storage"
	"project/internal/uplink"
	"project/pkg/global"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	redis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	nativeE2ETenant = "tenant-mqtt-e2e"
	nativeE2EDevice = "device-mqtt-e2e"
	nativeE2ECell   = "community"
	nativeE2ENode   = "node-a"
)

// This test crosses real Mosquitto plugin, Paho, Owner HTTP, PostgreSQL, and
// Redis boundaries. Set TEST_MQTT_NATIVE_E2E=1 to run the isolated fixture.
func TestBrokerIngressNativeMosquittoOwnerPostgresE2E(t *testing.T) {
	if os.Getenv("TEST_MQTT_NATIVE_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_E2E=1 to build/load native Mosquitto and run PostgreSQL/Redis integration")
	}
	root := repoRootForNativeE2E(t)
	mosquittoPath := nativeE2EPath("MOSQUITTO_BIN", "/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto")
	includePath := nativeE2EPath("MOSQUITTO_INCLUDE_DIR", "/tmp/tp-mqtt-20261019/mosquitto/include")
	cjsonPath := nativeE2EPath("CJSON_INCLUDE_DIR", "/opt/homebrew/opt/cjson/include")
	redisPath := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	for _, path := range []string{mosquittoPath, filepath.Join(includePath, "mosquitto_broker.h"), cjsonPath, redisPath} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("native MQTT e2e dependency %q unavailable: %v (override the corresponding environment path)", path, err)
		}
	}
	db, _, closeDB := nativeE2EPostgres(t)
	// query.SetDefault has no inverse for the package's initial nil DB. Keep
	// these test-only pools open until the test process exits so later package
	// tests cannot observe generated query objects backed by a closed pool.
	_ = closeDB
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	previousGlobalDB, previousRedis, previousStatusRedis, previousSSEManager := global.DB, global.REDIS, global.STATUS_REDIS, global.TPSSEManager
	previousDeviceQuery, previousCommandQuery := query.Device, query.CommandSetLog
	query.SetDefault(db)
	global.DB = db
	redisClient, stopRedis := startNativeE2ERedis(t, redisPath)
	global.REDIS, global.STATUS_REDIS = redisClient, redisClient
	global.TPSSEManager = global.NewSSEManager()
	t.Cleanup(func() {
		global.DB, global.REDIS, global.STATUS_REDIS = previousGlobalDB, previousRedis, previousStatusRedis
		global.TPSSEManager = previousSSEManager
		query.Device, query.CommandSetLog = previousDeviceQuery, previousCommandQuery
	})
	seedNativeE2EData(t, db)

	tokenBytes := make([]byte, 48)
	if _, err := rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	ownerToken := hex.EncodeToString(tokenBytes)
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), brokercontrol.Config{
		Tokens: []brokercontrol.TokenBinding{{Token: ownerToken, BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode}},
		Services: []brokercontrol.ServiceCredential{{
			Username: "mqtt-e2e-service", Password: "e2e-service-password", BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode,
			AllowedClientIDs: []string{"mqtt-e2e-failed-acceptor", "mqtt-e2e-service"},
			AllowedDeviceIDs: []string{nativeE2EDevice},
			Principal:        bc.Principal{TenantID: nativeE2ETenant, DeviceID: "service-mqtt-e2e", Kind: bc.PrincipalService},
			Policy:           bc.Policy{Publish: []string{"devices/command/native-e2e-number/+", "devices/event/response/+/+", "devices/attributes/response/+/+"}, Subscribe: []string{bc.IngressPrefix + "/#", bc.StatePrefix + "/#"}, MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress},
			PolicyVersion:    "native-e2e-v1",
			MappingVersion:   "standard-v1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerServer := httptest.NewServer(owner.Handler())
	t.Cleanup(ownerServer.Close)
	tokenPath := filepath.Join(t.TempDir(), "broker-owner.token")
	if err := os.WriteFile(tokenPath, []byte(ownerToken), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginPath := buildNativeE2EPlugin(t, root, includePath, cjsonPath)
	brokerAddr, brokerLog, stopBroker := startNativeE2EBroker(t, mosquittoPath, pluginPath, ownerServer.URL, tokenPath, root)
	t.Cleanup(stopBroker)
	setNativeE2EViper(t, brokerAddr)

	log := logrus.New()
	log.SetOutput(io.Discard)
	bus := uplink.NewBus(uplink.BusConfig{BufferSize: 64}, log)
	t.Cleanup(bus.Close)
	storageInput := make(chan *storage.Message, 64)
	storageManager := storage.New(db, log, storage.Config{ChannelBufferSize: 64, TelemetryBatchSize: 1, TelemetryFlushInterval: 250})
	storageCtx, storageCancel := context.WithCancel(context.Background())
	if err := storageManager.Start(storageCtx, storageInput); err != nil {
		t.Fatalf("start production storage manager: %v", err)
	}
	t.Cleanup(func() { _ = storageManager.Stop(5 * time.Second); storageCancel() })
	statusUplink := uplink.NewStatusUplink(uplink.StatusUplinkConfig{HeartbeatService: service.NewHeartbeatService(redisClient, log), BrokerStateStore: brokerstate.New(db), InputProfile: "ingress_v1", Logger: log})
	telemetryUplink := uplink.NewTelemetryUplink(uplink.TelemetryUplinkConfig{Processor: processor.NewScriptProcessor(), StorageInput: storageInput, HeartbeatService: service.NewHeartbeatService(redisClient, log), Logger: log})
	flow := &UplinkServiceWrapper{UplinkManager: uplink.NewUplinkManager(uplink.UplinkManagerConfig{Bus: bus, TelemetryUplink: telemetryUplink, StatusUplink: statusUplink, Logger: log}), bus: bus, isEnabled: true, logger: log}
	if err := flow.Start(); err != nil {
		t.Fatalf("start production UplinkServiceWrapper/StatusUplink: %v", err)
	}
	t.Cleanup(func() { _ = flow.Stop() })
	app := &Application{DB: db, Logger: log, uplinkService: flow}
	ctx, cancel := context.WithCancel(context.Background())
	service := &MQTTService{app: app, starting: true, startCancel: cancel}
	service.clientFactory = func(ctx context.Context, cfg mqttadapter.MQTTConfig, logger *logrus.Logger) (mqtt.Client, error) {
		if !cfg.ManualAck {
			return nil, errors.New("ingress_v1 service MQTT client did not enable manual ACK")
		}
		return mqttadapter.CreateMQTTClientContext(ctx, cfg, logger)
	}
	t.Cleanup(func() { _ = service.Stop() })

	// Real Paho/Mosquitto comparison: broker PUBACK succeeds, but the fake
	// durable acceptor rejects each delivery, so this subscriber sends no PUBACK.
	fakeCtx, fakeCancel := context.WithCancel(ctx)
	t.Cleanup(fakeCancel)
	fakeClient, err := mqttadapter.CreateMQTTClientContext(fakeCtx, mqttadapter.MQTTConfig{
		Broker: brokerAddr, Username: "mqtt-e2e-service", Password: "e2e-service-password", ClientID: "mqtt-e2e-failed-acceptor", ManualAck: true,
	}, log)
	if err != nil {
		t.Fatalf("connect failure-path Paho subscriber: %v", err)
	}
	fakeAdapter := mqttadapter.NewAdapter(bus, fakeClient, log)
	fakeAdapter.SetContext(fakeCtx)
	if err := fakeAdapter.ConfigureIngressFromConfig(); err != nil {
		t.Fatal(err)
	}
	rejector := &e2ERejectingAcceptor{}
	fakeAdapter.SetIngressAcceptor(rejector)
	if err := fakeAdapter.SubscribeAllContext(ctx, fakeClient); err != nil {
		t.Fatalf("subscribe failure-path Paho client: %v", err)
	}
	deviceClient := connectNativeE2EDevice(t, ctx, brokerAddr)
	pausedPayload := []byte(`{"source_event_id":"event-paused-e2e","method":"native-e2e","params":{"value":1}}`)
	waitNativeE2E(t, 5*time.Second, "initial failure-path state PUBACK", func() bool { return countNativeE2EAcks(brokerLog, "mqtt-e2e-failed-acceptor") > 0 })
	time.Sleep(150 * time.Millisecond)
	fakeAckBaseline := countNativeE2EAcks(brokerLog, "mqtt-e2e-failed-acceptor")
	publishNativeE2E(t, ctx, deviceClient, "devices/event/e2e-paused", pausedPayload)
	waitNativeE2E(t, 5*time.Second, "rejecting acceptor invocation", func() bool { return rejector.calls.Load() > 0 })
	time.Sleep(500 * time.Millisecond)
	if receipts := nativeE2EReceiptCount(t, db, nativeE2ETenant, nativeE2EDevice, "event-paused-e2e"); receipts != 0 {
		t.Fatalf("failed acceptor produced %d durable receipts", receipts)
	}
	if rows := nativeE2ECount(t, db, "event_datas"); rows != 0 {
		t.Fatalf("failed acceptor produced %d event rows", rows)
	}
	if got := countNativeE2EAcks(brokerLog, "mqtt-e2e-failed-acceptor"); got != fakeAckBaseline {
		t.Fatalf("failed acceptor unexpectedly ACKed %d additional service deliveries", got-fakeAckBaseline)
	}
	fakeCancel()
	fakeClient.Disconnect(250)

	// Production path: configureBrokerIngress creates the real receipt store and
	// supervised worker before SubscribeAllContext. Device PUBACK and DB terminal
	// state are asserted independently below.
	if err := service.initMQTTAdapter(ctx); err != nil {
		t.Fatalf("initialize MQTTService ingress: %v; mosquitto log: %s", err, nativeE2ETail(brokerLog))
	}
	publishNativeE2E(t, ctx, deviceClient, "devices/event/e2e-event", pausedPayload)
	waitNativeE2EReceipt(t, db, "event-paused-e2e", "processed")
	assertNativeE2ECounts(t, db, "event_datas", 1)
	nativeE2EBusinessEffects(t, db, 1)
	waitNativeE2E(t, 5*time.Second, "application PUBACK after event acceptance", func() bool { return countNativeE2EAcks(brokerLog, "mqtt-e2e-service") >= 1 })

	// A repeated source ID has a different MQTT packet ID but resolves to the
	// original receipt; verify the subscriber ACK and unchanged business counts.
	beforeDuplicateAck := countNativeE2EAcks(brokerLog, "mqtt-e2e-service")
	publishNativeE2E(t, ctx, deviceClient, "devices/event/e2e-event", pausedPayload)
	waitNativeE2E(t, 5*time.Second, "duplicate delivery application PUBACK", func() bool { return countNativeE2EAcks(brokerLog, "mqtt-e2e-service") > beforeDuplicateAck })
	assertNativeE2ECounts(t, db, "event_datas", 1)
	nativeE2EBusinessEffects(t, db, 1)
	if receipts := nativeE2EReceiptCount(t, db, nativeE2ETenant, nativeE2EDevice, "event-paused-e2e"); receipts != 1 {
		t.Fatalf("same event source ID produced %d receipts", receipts)
	}

	// Ordinary attribute sampling has no source ID and preserves its existing
	// payload/key contract; a repeat remains a valid sample, not an event dedup.
	attributePayload := []byte(`{"temperature":22.4}`)
	publishNativeE2E(t, ctx, deviceClient, "devices/attributes/attr-e2e", attributePayload)
	waitNativeE2E(t, 5*time.Second, "attribute business row", func() bool { return nativeE2EAttributeCount(t, db, nativeE2EDevice, "temperature") == 1 })
	nativeE2EBusinessEffects(t, db, 2)

	// Send real MQTT telemetry through Adapter -> Bus -> TelemetryUplink -> the
	// production batch writer. A TEL policy then plans a durable machine command.
	if err := GetGlobalMQTTAdapter().RememberMapping(nativeE2EDevice, "standard-v1"); err != nil {
		t.Fatalf("remember standard mapping for telemetry command: %v", err)
	}
	downlinkCtx, downlinkCancel := context.WithCancel(ctx)
	downlinkBus := downlink.NewBus(16)
	downlinkService := &DownlinkServiceWrapper{bus: downlinkBus, ctx: downlinkCtx, cancel: downlinkCancel, logger: log, processor: processor.NewScriptProcessor()}
	app.downlinkService = downlinkService
	if err := downlinkService.Start(); err != nil {
		t.Fatalf("start native durable downlink worker: %v", err)
	}
	t.Cleanup(func() { _ = downlinkService.Stop() })
	telemetryCommands := make(chan struct {
		topic   string
		payload []byte
	}, 2)
	if err := mqttadapter.WaitTokenContext(ctx, deviceClient.Subscribe("devices/command/native-e2e-number/+", 1, func(_ mqtt.Client, msg mqtt.Message) {
		telemetryCommands <- struct {
			topic   string
			payload []byte
		}{topic: msg.Topic(), payload: append([]byte(nil), msg.Payload()...)}
	}), 5*time.Second); err != nil {
		t.Fatalf("subscribe device for TEL automation command: %v", err)
	}
	sceneID, conditionID, actionID := "scene-native-telemetry-e2e", "condition-native-telemetry-e2e", "action-native-telemetry-e2e"
	if err := db.Create(&model.SceneAutomation{ID: sceneID, Name: "native telemetry automation", Enabled: "Y", TenantID: nativeE2ETenant, Creator: "fixture", Updator: "fixture", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	telType, telParam, telOperator := "TEL", "temperature", ">"
	if err := db.Create(&model.DeviceTriggerCondition{ID: conditionID, SceneAutomationID: sceneID, Enabled: "Y", GroupID: "g1", TriggerConditionType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, TriggerSource: stringPtr(nativeE2EDevice), TriggerParamType: &telType, TriggerParam: &telParam, TriggerOperator: &telOperator, TriggerValue: "20", TenantID: nativeE2ETenant}).Error; err != nil {
		t.Fatal(err)
	}
	actionType, actionParam, actionValue := "CMD", "setPower", `{"method":"setPower","params":{"value":99}}`
	if err := db.Create(&model.SceneActionInfo{ID: actionID, SceneID: sceneID, ActionTarget: nativeE2EDevice, ActionType: model.AUTOMATE_ACTION_TYPE_ONE, ActionParamType: &actionType, ActionParam: &actionParam, ActionValue: &actionValue, CreatedAt: time.Now().UTC(), TenantID: nativeE2ETenant}).Error; err != nil {
		t.Fatal(err)
	}
	publishNativeE2E(t, ctx, deviceClient, "devices/telemetry", []byte(`{"temperature":22.4}`))
	waitNativeE2E(t, 10*time.Second, "trusted telemetry sample, current value, and activity", func() bool {
		var history, current, samples, effects, activity, pending int64
		_ = db.Table("telemetry_datas").Where("device_id=? AND tenant_id=? AND key=?", nativeE2EDevice, nativeE2ETenant, "temperature").Count(&history).Error
		_ = db.Table("telemetry_current_datas").Where("device_id=? AND tenant_id=? AND key=?", nativeE2EDevice, nativeE2ETenant, "temperature").Count(&current).Error
		_ = db.Table("broker_ingress_receipts").Where("tenant_id=? AND device_id=? AND stream='telemetry_sample' AND broker_cell=? AND route_epoch=1", nativeE2ETenant, nativeE2EDevice, nativeE2ECell).Count(&samples).Error
		_ = db.Table("broker_ingress_effects").Where("tenant_id=? AND device_id=? AND effect_kind='automation' AND receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE stream='telemetry_sample' AND tenant_id=? AND device_id=?)", nativeE2ETenant, nativeE2EDevice, nativeE2ETenant, nativeE2EDevice).Count(&effects).Error
		_ = db.Table("broker_telemetry_activity").Where("tenant_id=? AND device_id=? AND broker_cell=? AND route_epoch=1", nativeE2ETenant, nativeE2EDevice, nativeE2ECell).Count(&activity).Error
		_ = db.Table("broker_state_projection_queue").Where("tenant_id=? AND device_id=?", nativeE2ETenant, nativeE2EDevice).Count(&pending).Error
		return history == 1 && current == 1 && samples == 1 && effects == 1 && activity == 1 && pending == 0
	})
	var telemetryCurrent storage.TelemetryCurrentData
	if err := db.Where("device_id=? AND tenant_id=? AND key=?", nativeE2EDevice, nativeE2ETenant, "temperature").Take(&telemetryCurrent).Error; err != nil {
		t.Fatal(err)
	}
	if telemetryCurrent.NumberV == nil || *telemetryCurrent.NumberV != 22.4 {
		t.Fatalf("telemetry current value=%v, want 22.4", telemetryCurrent.NumberV)
	}
	var planned struct{ CommandID, State string }
	waitNativeE2E(t, 10*time.Second, "TEL automation durable command", func() bool {
		return db.Table("broker_automation_actions").Select("command_id,state").Where("effect_id IN (SELECT id FROM broker_ingress_effects WHERE effect_kind='automation' AND receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE stream='telemetry_sample' AND tenant_id=? AND device_id=?))", nativeE2ETenant, nativeE2EDevice).Take(&planned).Error == nil
	})
	if planned.State != "accepted" {
		t.Fatalf("TEL automation action state=%q", planned.State)
	}
	var cmd brokercommands.Row
	if err := db.Where("id=? AND tenant_id=?", planned.CommandID, nativeE2ETenant).Take(&cmd).Error; err != nil {
		t.Fatal(err)
	}
	if cmd.CallerID != "automation:"+sceneID || cmd.Intent != "automation.command" {
		t.Fatalf("TEL automation command used unexpected caller/intent: %q/%q", cmd.CallerID, cmd.Intent)
	}
	var delivered struct {
		topic   string
		payload []byte
	}
	select {
	case delivered = <-telemetryCommands:
	case <-time.After(10 * time.Second):
		t.Fatalf("device received no TEL automation command; broker log: %s", nativeE2ETail(brokerLog))
	}
	if wantTopic := "devices/command/native-e2e-number/" + planned.CommandID; delivered.topic != wantTopic {
		t.Fatalf("TEL command topic=%q, want %q", delivered.topic, wantTopic)
	}
	var commandWire map[string]any
	if err := json.Unmarshal(delivered.payload, &commandWire); err != nil || commandWire["method"] != "setPower" || commandWire["params"].(map[string]any)["value"] != float64(99) {
		t.Fatalf("TEL command wire payload=%s err=%v", delivered.payload, err)
	}
	publishNativeE2E(t, ctx, deviceClient, "devices/command/response/"+planned.CommandID, []byte(`{"result":0}`))
	waitNativeE2E(t, 10*time.Second, "TEL automation command device ACK", func() bool {
		var state string
		return db.Table("broker_command_requests").Select("state").Where("id=? AND tenant_id=?", planned.CommandID, nativeE2ETenant).Scan(&state).Error == nil && state == brokercommands.StateDeviceReportedSuccess
	})

	// A valid command ACK updates its own row. A publish naming an ID belonging
	// to another device is rejected before ledger acceptance and leaves that row.
	seedNativeE2ECommand(t, db, nativeE2EDevice, "cmd-e2e-valid", "0")
	publishNativeE2E(t, ctx, deviceClient, "devices/command/response/cmd-e2e-valid", []byte(`{"result":0}`))
	waitNativeE2E(t, 5*time.Second, "scoped command ACK row update", func() bool { return nativeE2ECommandStatus(t, db, nativeE2EDevice, "cmd-e2e-valid") == "3" })
	seedNativeE2ECommand(t, db, "other-device", "cmd-e2e-foreign", "0")
	foreignAckBaseline := countNativeE2EAcks(brokerLog, "mqtt-e2e-service")
	publishNativeE2E(t, ctx, deviceClient, "devices/command/response/cmd-e2e-foreign", []byte(`{"result":0}`))
	waitNativeE2E(t, 5*time.Second, "durable quarantine for cross-device ACK", func() bool {
		return nativeE2ECount(t, db, "broker_ingress_quarantine") == 1
	})
	waitNativeE2E(t, 5*time.Second, "subscriber PUBACK after durable quarantine", func() bool {
		return countNativeE2EAcks(brokerLog, "mqtt-e2e-service") > foreignAckBaseline
	})
	if got := nativeE2ECommandStatus(t, db, "other-device", "cmd-e2e-foreign"); got != "0" {
		t.Fatalf("cross-device ACK changed foreign command status to %q", got)
	}
	if receipts := nativeE2EBusinessReceiptCount(t, db); receipts != 5 {
		t.Fatalf("expected event, attribute, telemetry sample, two valid command ACK business receipts; got %d", receipts)
	}
	if receipts := nativeE2EStreamReceiptCount(t, db, "state_transition"); receipts != 1 {
		t.Fatalf("expected one distinct state-transition receipt; got %d", receipts)
	}
	if effects := nativeE2EStateEffectCount(t, db); effects != 1 {
		t.Fatalf("expected one distinct state-transition effect; got %d", effects)
	}
	if countNativeE2EAcks(brokerLog, "mqtt-e2e-service") <= foreignAckBaseline {
		t.Fatal("cross-device ACK was not acknowledged after durable quarantine")
	}

	// Attach heartbeat configuration only after business ingress checks; keeping
	// it absent earlier preserves the ordinary no-transform decoder profile.
	otherConfig, configID := `{"online_timeout":30}`, "config-mqtt-e2e"
	if err := db.Create(&model.DeviceConfig{ID: configID, Name: "MQTT E2E", DeviceType: "1", TenantID: nativeE2ETenant, OtherConfig: &otherConfig}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Device{}).Where("id = ? AND tenant_id = ?", nativeE2EDevice, nativeE2ETenant).Update("device_config_id", configID).Error; err != nil {
		t.Fatal(err)
	}
	waitNativeE2E(t, 5*time.Second, "broker connection state projected by StatusUplink", func() bool {
		var online int16
		if err := db.Table("devices").Select("is_online").Where("id = ? AND tenant_id = ?", nativeE2EDevice, nativeE2ETenant).Scan(&online).Error; err != nil {
			return false
		}
		var pending int64
		if err := db.Table("broker_state_projection_queue").Where("tenant_id = ? AND device_id = ?", nativeE2ETenant, nativeE2EDevice).Count(&pending).Error; err != nil {
			return false
		}
		return online == 1 && pending == 0
	})
	var stateHistory int64
	if err := db.Table("device_status_history").Where("tenant_id = ? AND device_id = ? AND status = 1", nativeE2ETenant, nativeE2EDevice).Count(&stateHistory).Error; err != nil {
		t.Fatal(err)
	}
	if stateHistory != 1 {
		t.Fatalf("expected one online status history row after projection, got %d", stateHistory)
	}

	if err := service.Stop(); err != nil {
		t.Fatalf("stop MQTTService: %v", err)
	}
	cancel()
	deviceClient.Disconnect(250)
	stopRedis()
}

type e2ERejectingAcceptor struct{ calls atomic.Int32 }

func (a *e2ERejectingAcceptor) Accept(context.Context, *uplink.DeviceMessage) error {
	a.calls.Add(1)
	return errors.New("intentional native e2e acceptor rejection")
}

func nativeE2EPath(variable, fallback string) string {
	if value := os.Getenv(variable); value != "" {
		return value
	}
	return fallback
}

func repoRootForNativeE2E(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot locate repository root from %s: %v", wd, err)
	}
	return root
}

func nativeE2EPostgres(t *testing.T) (*gorm.DB, string, func()) {
	t.Helper()
	dsnBytes, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
	if err != nil {
		t.Skipf("PostgreSQL QA DSN unavailable: %v", err)
	}
	dsn := strings.TrimSpace(string(dsnBytes))
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Skipf("cannot connect PostgreSQL QA instance: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "mqtt_native_e2e_" + hex.EncodeToString(random)
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		_ = adminSQL.Close()
		t.Fatalf("create private PostgreSQL schema: %v", err)
	}
	isolated, err := gorm.Open(postgres.Open(nativeE2ESearchPath(dsn, schema)), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminSQL.Close()
		t.Fatalf("connect private PostgreSQL schema: %v", err)
	}
	isolatedSQL, err := isolated.DB()
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = isolatedSQL.Close()
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminSQL.Close()
	}
	return isolated, schema, cleanup
}

func nativeE2ESearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return dsn + " search_path=" + schema
}

func migrateNativeE2ESchema(t *testing.T, db *gorm.DB, root string) error {
	t.Helper()
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &model.DeviceStatusHistory{}, &storage.TelemetryData{}, &storage.TelemetryCurrentData{}, &model.CommandSetLog{}, &model.AttributeSetLog{}, &model.SceneAutomation{}, &model.SceneActionInfo{}, &model.DeviceTriggerCondition{}, &model.DeviceModelCommand{}, &model.DataScript{}, &storage.EventDataModel{}, &storage.AttributeData{}); err != nil {
		return fmt.Errorf("migrate business fixture tables: %w", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS attribute_datas_device_key ON attribute_datas(device_id,key)").Error; err != nil {
		return err
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS device_topic_mappings (
		id bigserial PRIMARY KEY, device_config_id varchar(36) NOT NULL, enabled boolean NOT NULL DEFAULT true,
		direction varchar(8) NOT NULL, source_topic text NOT NULL, target_topic text NOT NULL,
		priority integer NOT NULL DEFAULT 0, data_identifier text
	)`).Error; err != nil {
		return err
	}
	for _, filename := range []string{"001_broker_owner_tables.sql", "002_ledger.sql", "003_state.sql", "004_mapping_snapshots.sql", "005_ingress_effects.sql", "006_commands.sql", "007_state_projection.sql", "008_quarantine.sql", "009_automation_actions.sql", "010_telemetry_activity.sql", "011_safe_device_voucher.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "sql", "broker", filename))
		if err != nil {
			return fmt.Errorf("read broker migration %s: %w", filename, err)
		}
		statement := strings.ReplaceAll(string(body), "public.", "")
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply private-schema migration %s: %w", filename, err)
		}
	}
	return nil
}

func seedNativeE2EData(t *testing.T, db *gorm.DB) {
	t.Helper()
	device := model.Device{ID: nativeE2EDevice, TenantID: nativeE2ETenant, DeviceNumber: "native-e2e-number", Voucher: `{"username":"mqtt-e2e-device","password":"e2e-device-password"}`, IsEnabled: "enabled", ActivateFlag: "active", IsOnline: 0}
	if err := db.Create(&device).Error; err != nil {
		t.Fatalf("seed native e2e device: %v", err)
	}
}

func seedNativeE2ECommand(t *testing.T, db *gorm.DB, deviceID, messageID, status string) {
	t.Helper()
	message, operation := messageID, "1"
	row := model.CommandSetLog{ID: "cmd-row-" + messageID, DeviceID: deviceID, MessageID: &message, Status: &status, OperationType: &operation, CreatedAt: time.Now().UTC()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed command log: %v", err)
	}
}

func buildNativeE2EPlugin(t *testing.T, root, includePath, cjsonPath string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "libtp-mosquitto-e2e.so")
	command := exec.Command("go", "build", "-tags", "mosquitto_plugin", "-buildmode=c-shared", "-o", output, "./cmd/tp-mosquitto-plugin")
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=1", "CGO_CFLAGS=-I"+includePath+" -I"+cjsonPath)
	if body, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build native Mosquitto plugin: %v\n%s", err, body)
	}
	return output
}

func startNativeE2EBroker(t *testing.T, binary, plugin, ownerURL, tokenPath, root string) (string, string, func()) {
	t.Helper()
	port := freeNativeE2EPort(t)
	addr := fmt.Sprintf("tcp://127.0.0.1:%d", port)
	dir := t.TempDir()
	configPath, logPath := filepath.Join(dir, "mosquitto.conf"), filepath.Join(dir, "mosquitto.log")
	config := fmt.Sprintf("listener %d 127.0.0.1\nallow_anonymous false\nsys_interval 1\npersistence false\nplugin %s\nlog_dest file %s\nlog_type all\n", port, plugin, logPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-c", configPath)
	command.Dir = root
	command.Env = append(os.Environ(),
		"TP_BROKER_OWNER_URL="+ownerURL,
		"TP_BROKER_TOKEN_FILE="+tokenPath,
		"TP_BROKER_CELL="+nativeE2ECell,
		"TP_BROKER_INSTANCE="+nativeE2ENode,
		"TP_BROKER_MAX_PENDING=32",
		"TP_BROKER_MAX_SESSIONS=64",
		"DYLD_LIBRARY_PATH=/tmp/tp-mqtt-20261019/build/install/lib",
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start private Mosquitto process: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) || command.Process == nil {
			return
		}
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-waited:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-waited
		}
	}
	t.Cleanup(stop)
	waitNativeE2E(t, 8*time.Second, "native Mosquitto listener", func() bool {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if err != nil {
			select {
			case exitErr := <-waited:
				t.Fatalf("native Mosquitto exited before listening: %v; log: %s", exitErr, nativeE2ETail(logPath))
			default:
			}
			return false
		}
		_ = connection.Close()
		return true
	})
	return addr, logPath, stop
}

func startNativeE2ERedis(t *testing.T, binary string) (*redis.Client, func()) {
	t.Helper()
	port, dir := freeNativeE2EPort(t), t.TempDir()
	logFile, err := os.Create(filepath.Join(dir, "redis.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--port", fmt.Sprint(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--dir", dir)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start isolated Redis: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait(); _ = logFile.Close() }()
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port), MaxRetries: 0})
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) {
			return
		}
		_ = client.Close()
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-waited:
		case <-time.After(2 * time.Second):
			_ = command.Process.Kill()
			<-waited
		}
	}
	t.Cleanup(stop)
	waitNativeE2E(t, 5*time.Second, "isolated Redis PING", func() bool { return client.Ping(context.Background()).Err() == nil })
	return client, stop
}

func setNativeE2EViper(t *testing.T, broker string) {
	t.Helper()
	values := map[string]string{"mqtt.broker": broker, "mqtt.user": "mqtt-e2e-service", "mqtt.pass": "e2e-service-password", "mqtt.client_id": "mqtt-e2e-service", "mqtt.input_profile": "ingress_v1", "mqtt.broker_cell": nativeE2ECell, "mqtt.broker_instance": nativeE2ENode, "mqtt.retained_profile": "live-ingress"}
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
}

func connectNativeE2EDevice(t *testing.T, ctx context.Context, broker string) mqtt.Client {
	t.Helper()
	options := mqtt.NewClientOptions().AddBroker(broker).SetClientID("device-native-e2e").SetUsername("mqtt-e2e-device").SetPassword("e2e-device-password").SetCleanSession(true)
	client := mqtt.NewClient(options)
	if err := mqttadapter.WaitTokenContext(ctx, client.Connect(), 5*time.Second); err != nil {
		t.Fatalf("connect authenticated native device: %v", err)
	}
	t.Cleanup(func() { client.Disconnect(250) })
	return client
}

func publishNativeE2E(t *testing.T, ctx context.Context, client mqtt.Client, topic string, payload []byte) {
	t.Helper()
	if err := mqttadapter.WaitTokenContext(ctx, client.Publish(topic, 1, false, payload), 5*time.Second); err != nil {
		t.Fatalf("publish %s / broker PUBACK: %v", topic, err)
	}
}

func waitNativeE2E(t *testing.T, timeout time.Duration, label string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func waitNativeE2EReceipt(t *testing.T, db *gorm.DB, sourceID, state string) {
	t.Helper()
	waitNativeE2E(t, 10*time.Second, "receipt "+sourceID+" state "+state, func() bool {
		var got string
		return db.Raw("SELECT state FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND source_event_id=?", nativeE2ETenant, nativeE2EDevice, sourceID).Scan(&got).Error == nil && got == state
	})
}

func nativeE2EReceiptCount(t *testing.T, db *gorm.DB, tenant, device, sourceID string) int64 {
	t.Helper()
	q := db.Table("broker_ingress_receipts").Where("tenant_id=? AND device_id=?", tenant, device)
	if sourceID != "" {
		q = q.Where("source_event_id=?", sourceID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		t.Fatalf("count receipts: %v", err)
	}
	return count
}

func nativeE2EStreamReceiptCount(t *testing.T, db *gorm.DB, stream string) int64 {
	t.Helper()
	var count int64
	if err := db.Table("broker_ingress_receipts").Where("tenant_id=? AND device_id=? AND stream=?", nativeE2ETenant, nativeE2EDevice, stream).Count(&count).Error; err != nil {
		t.Fatalf("count %s receipts: %v", stream, err)
	}
	return count
}

func nativeE2EBusinessReceiptCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Table("broker_ingress_receipts").Where("tenant_id=? AND device_id=? AND stream <> 'state_transition'", nativeE2ETenant, nativeE2EDevice).Count(&count).Error; err != nil {
		t.Fatalf("count business receipts: %v", err)
	}
	return count
}

func nativeE2EBusinessEffects(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	waitNativeE2E(t, 5*time.Second, "business ingress effect count", func() bool {
		var count int64
		return db.Table("broker_ingress_effects").Where("tenant_id=? AND device_id=? AND receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND stream <> 'state_transition')", nativeE2ETenant, nativeE2EDevice, nativeE2ETenant, nativeE2EDevice).Count(&count).Error == nil && count == expected
	})
}

func nativeE2EStateEffectCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Table("broker_ingress_effects").Where("tenant_id=? AND device_id=? AND effect_kind='automation' AND receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE tenant_id=? AND device_id=? AND stream='state_transition')", nativeE2ETenant, nativeE2EDevice, nativeE2ETenant, nativeE2EDevice).Count(&count).Error; err != nil {
		t.Fatalf("count state-transition effects: %v", err)
	}
	return count
}

func nativeE2ECount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func assertNativeE2ECounts(t *testing.T, db *gorm.DB, table string, expected int64) {
	t.Helper()
	waitNativeE2E(t, 5*time.Second, table+" count", func() bool { return nativeE2ECount(t, db, table) == expected })
}

func nativeE2EAttributeCount(t *testing.T, db *gorm.DB, device, key string) int64 {
	t.Helper()
	var count int64
	if err := db.Table("attribute_datas").Where("device_id=? AND key=?", device, key).Count(&count).Error; err != nil {
		t.Fatalf("count attribute rows: %v", err)
	}
	return count
}

func nativeE2ECommandStatus(t *testing.T, db *gorm.DB, device, messageID string) string {
	t.Helper()
	var status string
	if err := db.Table("command_set_logs").Select("status").Where("device_id=? AND message_id=?", device, messageID).Scan(&status).Error; err != nil {
		t.Fatalf("read command status: %v", err)
	}
	return status
}

func countNativeE2EAcks(logPath, clientID string) int {
	body, err := os.ReadFile(logPath)
	if err != nil {
		return 0
	}
	return strings.Count(string(body), "Received PUBACK from "+clientID+" ")
}

func nativeE2ETail(path string) string {
	body, _ := os.ReadFile(path)
	if len(body) > 3000 {
		body = body[len(body)-3000:]
	}
	return string(body)
}

func freeNativeE2EPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

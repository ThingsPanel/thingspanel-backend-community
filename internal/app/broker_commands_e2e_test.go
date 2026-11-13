package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mqttadapter "project/internal/adapter/mqttadapter"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/brokercontrol"
	"project/internal/downlink"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/query"
	"project/internal/service"
	"project/internal/uplink"
	"project/pkg/constant"
	"project/pkg/global"

	"github.com/casbin/casbin/v2"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// TestDurableCommandNativeMosquittoPostgresE2E exercises the public command
// service through the real durable dispatcher, Mosquitto plugin and Paho
// device client. It requires the same isolated QA dependencies as the ingress
// E2E fixture and is opt-in via TEST_MQTT_NATIVE_E2E=1.
func TestDurableCommandNativeMosquittoPostgresE2E(t *testing.T) {
	if os.Getenv("TEST_MQTT_NATIVE_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_E2E=1 to run isolated native Mosquitto/Redis/PostgreSQL command E2E")
	}
	root := repoRootForNativeE2E(t)
	mosquittoPath := nativeE2EPath("MOSQUITTO_BIN", "/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto")
	includePath := nativeE2EPath("MOSQUITTO_INCLUDE_DIR", "/tmp/tp-mqtt-20261019/mosquitto/include")
	cjsonPath := nativeE2EPath("CJSON_INCLUDE_DIR", "/opt/homebrew/opt/cjson/include")
	redisPath := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	for _, path := range []string{mosquittoPath, filepath.Join(includePath, "mosquitto_broker.h"), cjsonPath, redisPath} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("native command E2E dependency %q unavailable: %v", path, err)
		}
	}
	db, _, closeDB := nativeE2EPostgres(t)
	_ = closeDB
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.DeviceModelCommand{}, &model.DataScript{}, &model.DeviceStatusHistory{}); err != nil {
		t.Fatalf("migrate command service fixture: %v", err)
	}
	for _, migration := range []string{"006_commands.sql", "007_state_projection.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "sql", "broker", migration))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(strings.ReplaceAll(string(body), "public.", "")).Error; err != nil {
			t.Fatalf("apply %s in private schema: %v", migration, err)
		}
	}

	previousDB, previousRedis, previousEnforcer := global.DB, global.REDIS, global.CasbinEnforcer
	previousDeviceQuery, previousLogQuery := query.Device, query.CommandSetLog
	query.SetDefault(db)
	global.DB = db
	redisClient, _ := startNativeE2ERedis(t, redisPath)
	global.REDIS = redisClient
	enforcer, err := casbin.NewEnforcer(filepath.Join(root, "configs", "casbin.conf"))
	if err != nil {
		t.Fatalf("load Casbin request model: %v", err)
	}
	global.CasbinEnforcer = enforcer
	t.Cleanup(func() {
		global.DB, global.REDIS, global.CasbinEnforcer = previousDB, previousRedis, previousEnforcer
		query.Device, query.CommandSetLog = previousDeviceQuery, previousLogQuery
	})
	seedNativeE2EData(t, db)
	otherDevice := model.Device{ID: "device-mqtt-e2e-other", TenantID: nativeE2ETenant, DeviceNumber: "other-device-number", Voucher: `{"username":"mqtt-e2e-other-device","password":"other-device-password"}`, IsEnabled: "enabled", ActivateFlag: "active", IsOnline: 0}
	if err := db.Create(&otherDevice).Error; err != nil {
		t.Fatalf("seed unauthorized command subscriber: %v", err)
	}
	status, tenant := "N", nativeE2ETenant
	user := model.User{ID: "operator-mqtt-e2e", TenantID: &tenant, Status: &status, PhoneNumber: "e2e", Email: "e2e@example.invalid", Password: "unused"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed command caller: %v", err)
	}

	tokenBytes := make([]byte, 48)
	if _, err := rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	ownerToken := hex.EncodeToString(tokenBytes)
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), brokercontrol.Config{
		Tokens: []brokercontrol.TokenBinding{{Token: ownerToken, BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode}},
		Services: []brokercontrol.ServiceCredential{{
			Username: "mqtt-e2e-service", Password: "e2e-service-password", BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode,
			AllowedClientIDs: []string{"mqtt-e2e-service"}, AllowedDeviceIDs: []string{nativeE2EDevice},
			Principal:     bc.Principal{TenantID: nativeE2ETenant, DeviceID: "service-mqtt-e2e", Kind: bc.PrincipalService},
			Policy:        bc.Policy{Publish: []string{"devices/command/native-e2e-number/+", "devices/event/response/+/+", "devices/attributes/response/+/+"}, Subscribe: []string{bc.IngressPrefix + "/#", bc.StatePrefix + "/#"}, MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress},
			PolicyVersion: "native-command-e2e-v1", MappingVersion: "standard-v1",
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
	plugin := buildNativeE2EPlugin(t, root, includePath, cjsonPath)
	brokerAddr, brokerLog, stopBroker := startNativeE2EBroker(t, mosquittoPath, plugin, ownerServer.URL, tokenPath, root)
	t.Cleanup(stopBroker)
	setNativeE2EViper(t, brokerAddr)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	uplinkBus := uplink.NewBus(uplink.BusConfig{BufferSize: 64}, logger)
	t.Cleanup(uplinkBus.Close)
	app := &Application{DB: db, Logger: logger, uplinkService: &UplinkServiceWrapper{bus: uplinkBus}}
	ctx, cancel := context.WithCancel(context.Background())
	serviceMQTT := &MQTTService{app: app, starting: true, startCancel: cancel}
	t.Cleanup(func() { _ = serviceMQTT.Stop() })
	if err := serviceMQTT.initMQTTAdapter(ctx); err != nil {
		t.Fatalf("initialize MQTTService: %v; mosquitto log: %s", err, nativeE2ETail(brokerLog))
	}
	adapter := GetGlobalMQTTAdapter()
	if adapter == nil {
		t.Fatal("MQTTService did not publish its configured adapter")
	}
	if err := adapter.RememberMapping(nativeE2EDevice, "standard-v1"); err != nil {
		t.Fatalf("seed authenticated standard mapping version: %v", err)
	}

	downlinkCtx, downlinkCancel := context.WithCancel(ctx)
	downlinkBus := downlink.NewBus(16)
	downlinkService := &DownlinkServiceWrapper{bus: downlinkBus, ctx: downlinkCtx, cancel: downlinkCancel, logger: logger, processor: processor.NewScriptProcessor()}
	app.downlinkService = downlinkService
	if err := downlinkService.Start(); err != nil {
		t.Fatalf("start real durable downlink worker: %v", err)
	}
	t.Cleanup(func() { _ = downlinkService.Stop() })

	device := connectNativeE2EDevice(t, ctx, brokerAddr)
	deviceMessages := make(chan []byte, 8)
	if err := mqttadapter.WaitTokenContext(ctx, device.Subscribe("devices/command/native-e2e-number/+", 1, func(_ mqtt.Client, msg mqtt.Message) {
		deviceMessages <- append([]byte(nil), msg.Payload()...)
	}), 5*time.Second); err != nil {
		t.Fatalf("subscribe real device command topic: %v", err)
	}
	wrongDevice := connectNativeE2EOtherDevice(t, ctx, brokerAddr)
	wrongMessages := make(chan []byte, 2)
	if err := mqttadapter.WaitTokenContext(ctx, wrongDevice.Subscribe("devices/command/native-e2e-number/+", 1, func(_ mqtt.Client, msg mqtt.Message) {
		wrongMessages <- append([]byte(nil), msg.Payload()...)
	}), 5*time.Second); err != nil {
		// A denied SUBACK is a valid ACL outcome; the invariant below is that the
		// unrelated principal receives no command delivery.
		logger.WithError(err).Debug("wrong-device command subscription denied")
	}

	commandService := &service.CommandData{}
	requestValue := `{"value":7}`
	request := &model.PutMessageForCommand{DeviceID: nativeE2EDevice, Identify: "setPower", Value: &requestValue, IdempotencyKey: "native-command-idempotency"}
	const submissions = 12
	ids := make([]string, submissions)
	errs := make([]error, submissions)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = commandService.CommandPutMessageWithResult(ctx, user.ID, request, fmt.Sprint(constant.Manual), nativeE2ETenant)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("same-key submit %d: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Fatalf("same-key submits returned different receipts %q and %q", ids[0], ids[i])
		}
	}
	if _, err := commandService.CommandPutMessageWithResult(ctx, user.ID, &model.PutMessageForCommand{DeviceID: nativeE2EDevice, Identify: "setPower", Value: stringPtr(`{"value":8}`), IdempotencyKey: request.IdempotencyKey}, fmt.Sprint(constant.Manual), nativeE2ETenant); !errors.Is(err, brokercommands.ErrConflict) {
		t.Fatalf("same key with changed request error=%v, want ErrConflict", err)
	}
	var payload []byte
	select {
	case payload = <-deviceMessages:
	case <-time.After(10 * time.Second):
		t.Fatalf("device received no actual command; broker log: %s", nativeE2ETail(brokerLog))
	}
	var command map[string]any
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("device command payload is not unwrapped JSON: %s (%v)", payload, err)
	}
	if command["method"] != "setPower" || command["params"].(map[string]any)["value"] != float64(7) {
		t.Fatalf("device received command=%s", payload)
	}
	select {
	case duplicate := <-deviceMessages:
		t.Fatalf("idempotent retries caused a second MQTT delivery: %s", duplicate)
	case <-time.After(350 * time.Millisecond):
	}
	select {
	case leaked := <-wrongMessages:
		t.Fatalf("command leaked to unrelated device principal: %s", leaked)
	case <-time.After(100 * time.Millisecond):
	}
	statusBeforeAck, err := commandService.GetCommandStatus(ctx, ids[0], nativeE2ETenant)
	if err != nil || (statusBeforeAck.Status != brokercommands.StateBrokerAccepted && statusBeforeAck.Status != brokercommands.StateDeviceReportedSuccess) {
		t.Fatalf("status before device ACK=%+v err=%v", statusBeforeAck, err)
	}

	ack := []byte(`{"result":0}`)
	publishNativeE2E(t, ctx, device, "devices/command/response/"+ids[0], ack)
	waitNativeE2E(t, 10*time.Second, "durable command device report", func() bool {
		status, err := commandService.GetCommandStatus(ctx, ids[0], nativeE2ETenant)
		return err == nil && status.Status == brokercommands.StateDeviceReportedSuccess
	})
	otherTenantStatus, err := commandService.GetCommandStatus(ctx, ids[0], "other-tenant")
	if err != nil {
		t.Fatal(err)
	} else if otherTenantStatus.Status != "not_found" {
		t.Fatalf("cross-tenant status lookup returned %+v", otherTenantStatus)
	}
	var requestCount, logCount int64
	if err := db.Table("broker_command_requests").Where("id=?", ids[0]).Count(&requestCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("command_set_logs").Where("message_id=?", ids[0]).Count(&logCount).Error; err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 || logCount != 1 {
		t.Fatalf("durable commands=%d command logs=%d, want one each", requestCount, logCount)
	}

	// The same key is scoped to the original caller and tenant, even after its
	// request has completed. These lookups must not reveal another caller's row.
	canonical := []byte(`{"device_id":"device-mqtt-e2e","identify":"setPower","params":{"value":7},"has_params":true}`)
	commandStore := brokercommands.New(db)
	if _, found, err := commandStore.FindIdempotent(ctx, nativeE2ETenant, "another-caller", "device.command", request.IdempotencyKey, canonical); err != nil || found {
		t.Fatalf("other caller idempotency lookup found=%v err=%v", found, err)
	}
	if _, found, err := commandStore.FindIdempotent(ctx, "other-tenant", user.ID, "device.command", request.IdempotencyKey, canonical); err != nil || found {
		t.Fatalf("other tenant idempotency lookup found=%v err=%v", found, err)
	}

	// A retry cannot bypass a caller whose access was revoked after acceptance.
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("status", "Y").Error; err != nil {
		t.Fatal(err)
	}
	if retryID, err := commandService.CommandPutMessageWithResult(ctx, user.ID, request, fmt.Sprint(constant.Manual), nativeE2ETenant); err == nil || retryID != "" {
		t.Fatalf("frozen caller retry returned id=%q err=%v", retryID, err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("status", "N").Error; err != nil {
		t.Fatal(err)
	}

	// A reliable device event creates a durable automation effect. Its machine
	// policy, not the prior human command caller, authorizes the resulting command.
	sceneID, conditionID, actionID := "scene-native-command-e2e", "condition-native-command-e2e", "action-native-command-e2e"
	if err := db.Create(&model.SceneAutomation{ID: sceneID, Name: "native command automation", Enabled: "Y", TenantID: nativeE2ETenant, Creator: "fixture", Updator: "fixture", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	triggerType, triggerParam, triggerOperator := model.TRIGGER_PARAM_TYPE_EVT, "alarm", "="
	triggerValue := `{"match_mode":"field","conditions":[{"field":"state","operator":"=","value":"on"}]}`
	if err := db.Create(&model.DeviceTriggerCondition{ID: conditionID, SceneAutomationID: sceneID, Enabled: "Y", GroupID: "g1", TriggerConditionType: model.DEVICE_TRIGGER_CONDITION_TYPE_ONE, TriggerSource: stringPtr(nativeE2EDevice), TriggerParamType: &triggerType, TriggerParam: &triggerParam, TriggerOperator: &triggerOperator, TriggerValue: triggerValue, TenantID: nativeE2ETenant}).Error; err != nil {
		t.Fatal(err)
	}
	actionType, actionParam, actionValue := "CMD", "setPower", `{"method":"setPower","params":{"value":99}}`
	if err := db.Create(&model.SceneActionInfo{ID: actionID, SceneID: sceneID, ActionTarget: nativeE2EDevice, ActionType: model.AUTOMATE_ACTION_TYPE_ONE, ActionParamType: &actionType, ActionParam: &actionParam, ActionValue: &actionValue, CreatedAt: time.Now().UTC(), TenantID: nativeE2ETenant}).Error; err != nil {
		t.Fatal(err)
	}
	automationEvent := []byte(`{"source_event_id":"automation-event-native-e2e","method":"alarm","params":{"state":"on"}}`)
	publishNativeE2E(t, ctx, device, "devices/event/alarm", automationEvent)
	waitNativeE2EReceipt(t, db, "automation-event-native-e2e", "processed")
	waitNativeE2E(t, 10*time.Second, "automation durable command request", func() bool {
		return nativeE2ECount(t, db, "broker_command_requests") == 2 && nativeE2ECount(t, db, "broker_automation_actions") == 1
	})
	var automation struct {
		CommandID string `gorm:"column:command_id"`
		State     string `gorm:"column:state"`
		Policy    string `gorm:"column:policy_sha256"`
	}
	if err := db.Table("broker_automation_actions").Select("command_id,state,policy_sha256").Where("effect_id IN (SELECT id FROM broker_ingress_effects WHERE receipt_id IN (SELECT id FROM broker_ingress_receipts WHERE source_event_id=?))", "automation-event-native-e2e").Take(&automation).Error; err != nil {
		t.Fatal(err)
	}
	var autoCommand brokercommands.Row
	if err := db.Where("id=? AND tenant_id=?", automation.CommandID, nativeE2ETenant).Take(&autoCommand).Error; err != nil {
		t.Fatal(err)
	}
	if automation.State != "accepted" || len(automation.Policy) != 64 || autoCommand.CallerID != "automation:"+sceneID || autoCommand.Intent != "automation.command" || autoCommand.CallerID == user.ID {
		t.Fatalf("automation did not use scoped machine policy: action=%+v command caller=%q intent=%q", automation, autoCommand.CallerID, autoCommand.Intent)
	}
	var prepared brokercommands.PreparedCommand
	if err := json.Unmarshal(autoCommand.Payload, &prepared); err != nil || prepared.Automation == nil || prepared.Automation.SceneID != sceneID || prepared.Automation.ActionID != actionID || prepared.Automation.PolicySHA256 != automation.Policy {
		t.Fatalf("durable command lacks matching policy proof: proof=%+v err=%v", prepared.Automation, err)
	}
	var automationPayload []byte
	select {
	case automationPayload = <-deviceMessages:
	case <-time.After(10 * time.Second):
		t.Fatalf("device received no automation command; broker log: %s", nativeE2ETail(brokerLog))
	}
	var automationWire map[string]any
	if err := json.Unmarshal(automationPayload, &automationWire); err != nil || automationWire["method"] != "setPower" || automationWire["params"].(map[string]any)["value"] != float64(99) {
		t.Fatalf("device automation payload=%s err=%v", automationPayload, err)
	}
	// Duplicate source delivery cannot mint another command or delivery.
	publishNativeE2E(t, ctx, device, "devices/event/alarm", automationEvent)
	waitNativeE2E(t, 5*time.Second, "automation duplicate ingress ACK", func() bool { return nativeE2ECount(t, db, "broker_automation_actions") == 1 })
	if status, err := commandService.GetCommandStatus(ctx, automation.CommandID, nativeE2ETenant); err != nil || status.Status != brokercommands.StateBrokerAccepted {
		t.Fatalf("automation status lookup=%+v err=%v", status, err)
	}
	select {
	case duplicate := <-deviceMessages:
		t.Fatalf("duplicate event caused second automation delivery: %s", duplicate)
	case <-time.After(300 * time.Millisecond):
	}
	publishNativeE2E(t, ctx, device, "devices/command/response/"+automation.CommandID, []byte(`{"result":0}`))
	waitNativeE2E(t, 10*time.Second, "automation device ACK", func() bool {
		status, err := commandService.GetCommandStatus(ctx, automation.CommandID, nativeE2ETenant)
		return err == nil && status.Status == brokercommands.StateDeviceReportedSuccess
	})
	if err := db.Raw("SELECT state FROM broker_automation_actions WHERE command_id=?", automation.CommandID).Scan(&automation.State).Error; err != nil || automation.State != "accepted" {
		t.Fatalf("automation action audit was not preserved after ACK: state=%q err=%v", automation.State, err)
	}

	// Once accepted, retry identity is based on client input. Device state and
	// receipt deadline changes must not create a replacement command.
	if err := db.Table("devices").Where("id = ? AND tenant_id = ?", nativeE2EDevice, nativeE2ETenant).Updates(map[string]any{"is_enabled": "disabled", "device_number": "changed-after-acceptance"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("broker_command_requests").Where("id = ?", ids[0]).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	retryID, err := commandService.CommandPutMessageWithResult(ctx, user.ID, request, fmt.Sprint(constant.Manual), nativeE2ETenant)
	if err != nil || retryID != ids[0] {
		t.Fatalf("same client request after device/config/expiry change returned id=%q err=%v, want original %q", retryID, err, ids[0])
	}
	select {
	case extra := <-deviceMessages:
		t.Fatalf("accepted retry published a replacement MQTT command: %s", extra)
	case <-time.After(350 * time.Millisecond):
	}
	downlinkCancel()
	cancel()
}

func stringPtr(s string) *string { return &s }

func connectNativeE2EOtherDevice(t *testing.T, ctx context.Context, broker string) mqtt.Client {
	t.Helper()
	options := mqtt.NewClientOptions().AddBroker(broker).SetClientID("device-native-e2e-other").SetUsername("mqtt-e2e-other-device").SetPassword("other-device-password").SetCleanSession(true)
	client := mqtt.NewClient(options)
	if err := mqttadapter.WaitTokenContext(ctx, client.Connect(), 5*time.Second); err != nil {
		t.Fatalf("connect unrelated native device: %v", err)
	}
	t.Cleanup(func() { client.Disconnect(250) })
	return client
}

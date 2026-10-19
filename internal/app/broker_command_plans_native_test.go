package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	"project/internal/uplink"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/casbin/casbin/v2"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	jwt "github.com/golang-jwt/jwt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// TestPlannedCommandNativeMosquittoPostgresE2E proves the SQL013 plan lifecycle
// reaches one real Paho device delivery and a scoped device ACK. Encore is an
// exact-response httptest stub; this is not a live Encore or production test.
func TestPlannedCommandNativeMosquittoPostgresE2E(t *testing.T) {
	if os.Getenv("TEST_MQTT_NATIVE_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_E2E=1 to run isolated native command-plan E2E")
	}
	t.Setenv("TP_BROKER_ROUTE_FENCING", "1")
	root := repoRootForNativeE2E(t)
	mosquittoPath := nativeE2EPath("MOSQUITTO_BIN", "/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto")
	includePath := nativeE2EPath("MOSQUITTO_INCLUDE_DIR", "/tmp/tp-mqtt-20261019/mosquitto/include")
	cjsonPath := nativeE2EPath("CJSON_INCLUDE_DIR", "/opt/homebrew/opt/cjson/include")
	redisPath := nativeE2EPath("REDIS_SERVER", "/opt/homebrew/bin/redis-server")
	if strings.TrimSpace(os.Getenv("TP_NATIVE_E2E_DOCKER_IMAGE")) == "" {
		for _, path := range []string{mosquittoPath, filepath.Join(includePath, "mosquitto_broker.h"), cjsonPath} {
			if _, err := os.Stat(path); err != nil {
				t.Skipf("native command-plan dependency %q unavailable: %v", path, err)
			}
		}
	}
	if _, err := os.Stat(redisPath); err != nil {
		t.Skipf("native command-plan Redis dependency unavailable: %v", err)
	}
	db, _, closeDB := nativeE2EPostgres(t)
	_ = closeDB // query.SetDefault has no inverse; keep this isolated pool alive through package tests.
	if err := migrateNativeE2ESchema(t, db, root); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.DeviceModelCommand{}, &model.DataScript{}, &model.CommandSetLog{}, &model.AttributeSetLog{}, &model.TelemetrySetLog{}, &model.ExpectedData{}, &model.DeviceTopicMapping{}); err != nil {
		t.Fatalf("migrate command-plan business fixture: %v", err)
	}

	previousDB, previousRedis, previousStatusRedis := global.DB, global.REDIS, global.STATUS_REDIS
	previousSSEManager := global.TPSSEManager
	previousEnforcer := global.CasbinEnforcer
	previousDeviceQuery, previousLogQuery := query.Device, query.CommandSetLog
	query.SetDefault(db)
	global.DB = db
	redisClient, _ := startNativeE2ERedis(t, redisPath)
	global.REDIS, global.STATUS_REDIS = redisClient, redisClient
	global.TPSSEManager = global.NewSSEManager()
	enforcer, err := casbin.NewEnforcer(filepath.Join(root, "configs", "casbin.conf"))
	if err != nil {
		t.Fatal("load command-plan Casbin model")
	}
	global.CasbinEnforcer = enforcer
	t.Cleanup(func() {
		global.DB, global.REDIS, global.STATUS_REDIS = previousDB, previousRedis, previousStatusRedis
		global.TPSSEManager = previousSSEManager
		global.CasbinEnforcer = previousEnforcer
		query.Device, query.CommandSetLog = previousDeviceQuery, previousLogQuery
	})
	seedNativeE2EData(t, db)

	const userID, configID, templateID = "operator-plan-native-e2e", "config-plan-native-e2e", "template-plan-native-e2e"
	status, authority, protocolType, tenant := "N", "TENANT_ADMIN", "MQTT", nativeE2ETenant
	user := model.User{ID: userID, TenantID: &tenant, Status: &status, Authority: &authority, PhoneNumber: "fixture", Email: "plan-native@example.invalid", Password: "unused"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal("seed command-plan operator")
	}
	if err := db.Create(&model.DeviceConfig{ID: configID, Name: "plan native fixture", TenantID: nativeE2ETenant, DeviceType: "1", DeviceTemplateID: stringPtr(templateID), ProtocolType: &protocolType, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("seed command-plan device config")
	}
	if err := db.Model(&model.Device{}).Where("id=? AND tenant_id=?", nativeE2EDevice, nativeE2ETenant).Update("device_config_id", configID).Error; err != nil {
		t.Fatal("bind native device config")
	}
	const jwtKey = "isolated-native-command-plan-jwt-key"
	token := commandPlanNativeSessionToken(t, jwtKey, userID, nativeE2ETenant, authority)
	if err := redisClient.Set(context.Background(), token, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed current user session")
	}

	var revision = 1
	semantic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/" + nativeE2EDevice:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"device_template_id":%q}`, nativeE2EDevice, nativeE2ETenant, templateID)
		case "/api/device-templates/" + templateID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"thing_model_id":"model-plan","thing_model_snapshot_id":"snapshot-plan"}`, templateID, nativeE2ETenant)
		case "/api/v2/thing-models/model-plan/versions":
			fmt.Fprintf(w, `{"items":[{"publishedRevision":%d,"snapshotID":"snapshot-plan"}],"page":1,"pageSize":200,"total":1}`, revision)
		case fmt.Sprintf("/api/v2/thing-models/model-plan/versions/%d", revision):
			fmt.Fprintf(w, `{"modelID":"model-plan","publishedRevision":%d,"snapshotID":"snapshot-plan","model":{"fields":[{"kind":"COMMAND","identifier":"set_power","access":{"read":false,"write":true,"observe":false},"parameters":{"schemaVersion":1,"items":[{"identifier":"value","valueType":{"kind":"BOOL"}}]}}]},"resources":{"modelID":"model-plan","revision":0,"profile":{},"webChartConfig":{},"appChartConfig":{}}}`, revision)
		default:
			http.NotFound(w, r)
		}
	}))
	defer semantic.Close()

	// Freeze all feature/runtime settings before any worker is started.
	setNativeE2EViper(t, "tcp://127.0.0.1:1")
	runtimeInstance := fmt.Sprintf("command-plan-native-%d", time.Now().UnixNano())
	for key, value := range map[string]any{
		"mqtt.route_fencing": true, "mqtt.runtime_instance": runtimeInstance,
		"mqtt.command_plans_enabled": true, "jwt.key": jwtKey,
		"mqtt.semantic_base_url": semantic.URL, "mqtt.semantic_development_http": true,
	} {
		old, had := viper.Get(key), viper.IsSet(key)
		viper.Set(key, value)
		t.Cleanup(func() {
			if had {
				viper.Set(key, old)
			} else {
				viper.Set(key, "")
			}
		})
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		t.Fatal("generate private owner token")
	}
	ownerToken := hex.EncodeToString(random)
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), brokercontrol.Config{
		Tokens: []brokercontrol.TokenBinding{{Token: ownerToken, BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode}},
		Services: []brokercontrol.ServiceCredential{{
			Username: "mqtt-e2e-service", Password: "e2e-service-password", BrokerCell: nativeE2ECell, BrokerInstance: nativeE2ENode,
			AllowedClientIDs: []string{"mqtt-e2e-service"}, AllowedDeviceIDs: []string{nativeE2EDevice},
			Principal:     bc.Principal{TenantID: nativeE2ETenant, DeviceID: "service-mqtt-e2e", Kind: bc.PrincipalService},
			Policy:        bc.Policy{Publish: []string{"devices/command/native-e2e-number/+", "devices/telemetry/control/native-e2e-number", "devices/attributes/get/native-e2e-number", "devices/attributes/set/native-e2e-number/+", "devices/event/response/+/+", "devices/attributes/response/+/+"}, Subscribe: []string{bc.IngressPrefix + "/#", bc.StatePrefix + "/#"}, MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress},
			PolicyVersion: "native-command-plan-v1", MappingVersion: "standard-v1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerServer := httptest.NewServer(owner.RuntimeHandler(brokercontrol.NewMigrationStore(db)))
	t.Cleanup(ownerServer.Close)
	tokenPath := filepath.Join(t.TempDir(), "broker-owner.token")
	if err := os.WriteFile(tokenPath, []byte(ownerToken), 0o600); err != nil {
		t.Fatal("write private owner token file")
	}
	pluginPath := ""
	if strings.TrimSpace(os.Getenv("TP_NATIVE_E2E_DOCKER_IMAGE")) == "" {
		pluginPath = buildNativeE2EPlugin(t, root, includePath, cjsonPath)
	}
	brokerAddr, brokerLog, stopBroker := startNativeE2EBroker(t, mosquittoPath, pluginPath, ownerServer.URL, tokenPath, root)
	t.Cleanup(stopBroker)
	setNativeE2EViper(t, brokerAddr)
	// setNativeE2EViper was called above with a placeholder only to populate the
	// remaining credentials; replace broker after all runtime settings are set.
	viper.Set("mqtt.broker", brokerAddr)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	bus := uplink.NewBus(uplink.BusConfig{BufferSize: 64}, logger)
	t.Cleanup(bus.Close)
	statusUplink := uplink.NewStatusUplink(uplink.StatusUplinkConfig{HeartbeatService: service.NewHeartbeatService(redisClient, logger), BrokerStateStore: brokerstate.New(db), InputProfile: "ingress_v1", Logger: logger})
	manager := uplink.NewUplinkManager(uplink.UplinkManagerConfig{Bus: bus, StatusUplink: statusUplink, Logger: logger})
	flow := &UplinkServiceWrapper{UplinkManager: manager, bus: bus, isEnabled: true, logger: logger}
	if err := flow.Start(); err != nil {
		t.Fatalf("start state projection: %v", err)
	}
	t.Cleanup(func() { _ = flow.Stop() })
	app := &Application{DB: db, Logger: logger, uplinkService: flow}
	ctx, cancel := context.WithCancel(context.Background())
	mqttService := &MQTTService{app: app, starting: true, startCancel: cancel}
	t.Cleanup(func() { _ = mqttService.Stop() })
	if err := mqttService.initMQTTAdapter(ctx); err != nil {
		t.Fatalf("initialize route-fenced MQTTService: %v; broker log: %s", err, nativeE2ETail(brokerLog))
	}
	device := connectNativeE2EDevice(t, ctx, brokerAddr)
	deviceMessages := make(chan nativeExpectedDelivery, 8)
	if err := mqttadapter.WaitTokenContext(ctx, device.Subscribe("devices/command/native-e2e-number/+", 1, func(_ mqtt.Client, msg mqtt.Message) {
		deviceMessages <- nativeExpectedDelivery{topic: msg.Topic(), payload: append([]byte(nil), msg.Payload()...)}
	}), 5*time.Second); err != nil {
		t.Fatalf("subscribe real device command topic: %v", err)
	}
	waitNativeE2E(t, 10*time.Second, "real connection observation", func() bool {
		var count int64
		return db.Table("broker_connection_observations").Where("tenant_id=? AND device_id=? AND online=true", nativeE2ETenant, nativeE2EDevice).Count(&count).Error == nil && count > 0
	})

	dlCtx, dlCancel := context.WithCancel(ctx)
	dl := &DownlinkServiceWrapper{bus: downlink.NewBus(8), ctx: dlCtx, cancel: dlCancel, logger: logger, processor: processor.NewScriptProcessor()}
	app.downlinkService = dl
	if err := dl.Start(); err != nil {
		t.Fatalf("start production durable command dispatcher: %v", err)
	}
	t.Cleanup(func() { _ = dl.Stop() })

	claims := &utils.UserClaims{ID: userID, TenantID: nativeE2ETenant, Authority: authority}
	cmdService := &service.CommandData{}
	value := `{"value":true}`
	request := &model.PutMessageForCommand{DeviceID: nativeE2EDevice, Identify: "set_power", Value: &value}
	preview, err := cmdService.PreviewCommandPlan(ctx, claims, token, request)
	if err != nil || preview.PlanID == "" || !preview.AuthorizationRequired {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	select {
	case got := <-deviceMessages:
		t.Fatalf("preview published a device command: %s", got.payload)
	case <-time.After(300 * time.Millisecond):
	}
	use := service.CommandPlanUse{PlanHash: preview.PlanHash, IdempotencyKey: "native-plan-submit-key"}
	if _, err := cmdService.SubmitCommandPlan(ctx, claims, token, preview.PlanID, use); err == nil {
		t.Fatal("submit without human authorization succeeded")
	}
	authorizationID, err := cmdService.AuthorizeCommandPlan(ctx, claims, token, preview.PlanID, use)
	if err != nil || authorizationID == "" {
		t.Fatalf("human authorization=%q err=%v", authorizationID, err)
	}
	use.AuthorizationID = authorizationID
	commandID, err := cmdService.SubmitCommandPlan(ctx, claims, token, preview.PlanID, use)
	if err != nil || commandID == "" {
		t.Fatalf("authorized plan submit id=%q err=%v", commandID, err)
	}
	retryID, err := cmdService.SubmitCommandPlan(ctx, claims, token, preview.PlanID, use)
	if err != nil || retryID != commandID {
		t.Fatalf("same-key submit id=%q err=%v want=%q", retryID, err, commandID)
	}
	otherKey := use
	otherKey.IdempotencyKey = "native-plan-second-key"
	if _, err := cmdService.SubmitCommandPlan(ctx, claims, token, preview.PlanID, otherKey); err == nil {
		t.Fatal("second idempotency key reused the one-shot plan")
	}
	var payload []byte
	select {
	case delivery := <-deviceMessages:
		if delivery.topic != "devices/command/native-e2e-number/"+commandID {
			t.Fatalf("planned command arrived on unexpected topic %q", delivery.topic)
		}
		payload = delivery.payload
	case <-time.After(10 * time.Second):
		t.Fatalf("no real MQTT command delivery; broker log: %s", nativeE2ETail(brokerLog))
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil || wire["method"] != "set_power" || wire["params"].(map[string]any)["value"] != true {
		t.Fatalf("device payload=%s err=%v", payload, err)
	}
	if strings.Contains(string(payload), "broker_frame") || strings.Contains(string(payload), "tp-internal") {
		t.Fatalf("private storage frame leaked to device: %s", payload)
	}
	select {
	case duplicate := <-deviceMessages:
		t.Fatalf("plan retry caused second MQTT delivery: %s", duplicate.payload)
	case <-time.After(300 * time.Millisecond):
	}
	statusBeforeAck, err := cmdService.GetCommandStatus(ctx, commandID, nativeE2ETenant)
	if err != nil || (statusBeforeAck.Status != brokercommands.StateBrokerAccepted && statusBeforeAck.Status != brokercommands.StateDeviceReportedSuccess) {
		t.Fatalf("pre-ACK state=%+v err=%v", statusBeforeAck, err)
	}
	publishNativeE2E(t, ctx, device, "devices/command/response/"+commandID, []byte(`{"result":0}`))
	waitNativeE2E(t, 10*time.Second, "planned command device ACK", func() bool {
		got, err := cmdService.GetCommandStatus(ctx, commandID, nativeE2ETenant)
		return err == nil && got.Status == brokercommands.StateDeviceReportedSuccess
	})
	var plans, commands, logs int64
	db.Table("broker_command_plans").Where("id=? AND command_id=?", preview.PlanID, commandID).Count(&plans)
	db.Table("broker_command_requests").Where("id=? AND tenant_id=?", commandID, nativeE2ETenant).Count(&commands)
	db.Table("command_set_logs").Where("message_id=?", commandID).Count(&logs)
	if plans != 1 || commands != 1 || logs != 1 {
		t.Fatalf("durable plan links=%d commands=%d logs=%d, want one each", plans, commands, logs)
	}
	if _, err := brokercommands.New(db).GetScoped(ctx, nativeE2ETenant, commandID); err != nil {
		t.Fatalf("load terminal command receipt: %v", err)
	}
	runExpectedNativeSmoke(t, ctx, db, device, deviceMessages, claims)
}

type nativeExpectedDelivery struct {
	topic   string
	payload []byte
}

func runExpectedNativeSmoke(t *testing.T, ctx context.Context, db *gorm.DB, device mqtt.Client, deliveries chan nativeExpectedDelivery, claims *utils.UserClaims) {
	t.Helper()
	t.Run("expected-data-downlinks", func(t *testing.T) {
		t.Helper()
		if os.Getenv("TEST_MQTT_EXPECTED_SMOKE") != "1" {
			t.Skip("set TEST_MQTT_EXPECTED_SMOKE=1 to run expected-data native smoke")
		}
		const number = "native-e2e-number"
		for _, filter := range []string{
			"devices/telemetry/control/" + number,
			"devices/attributes/get/" + number,
			"devices/attributes/set/" + number + "/+",
		} {
			if err := mqttadapter.WaitTokenContext(ctx, device.Subscribe(filter, 1, func(_ mqtt.Client, msg mqtt.Message) {
				deliveries <- nativeExpectedDelivery{topic: msg.Topic(), payload: append([]byte(nil), msg.Payload()...)}
			}), 5*time.Second); err != nil {
				t.Fatalf("subscribe expected-data wire filter %q: %v", filter, err)
			}
		}

		expectedSvc := &service.ExpectedData{}
		for _, tc := range []struct {
			kind, topic string
			payload     string
			identify    *string
		}{
			{kind: "telemetry", topic: "devices/telemetry/control/" + number, payload: `{"temperature":21}`},
			{kind: "attribute", topic: "", payload: `{"temperature":22}`},
			{kind: "command", topic: "", identify: stringPtr("set_expected"), payload: `{"value":true}`},
		} {
			request := &model.CreateExpectedDataReq{DeviceID: nativeE2EDevice, SendType: tc.kind, Payload: &tc.payload, Identify: tc.identify}
			created, err := expectedSvc.Create(ctx, request, claims)
			if err != nil {
				t.Fatalf("create %s expected-data intent: %v", tc.kind, err)
			}
			var saved model.ExpectedData
			if err := db.Where("id=? AND tenant_id=?", created.ID, nativeE2ETenant).Take(&saved).Error; err != nil {
				t.Fatalf("load %s expected-data receipt: %v", tc.kind, err)
			}
			if saved.Message == nil || *saved.Message == "" {
				t.Fatalf("%s expected-data intent has no command receipt ID", tc.kind)
			}
			id := *saved.Message
			if tc.kind == "attribute" {
				tc.topic = "devices/attributes/set/" + number + "/" + id
			} else if tc.kind == "command" {
				tc.topic = "devices/command/" + number + "/" + id
			}
			select {
			case got := <-deliveries:
				if got.topic != tc.topic {
					t.Fatalf("%s expected data delivered on %q, want %q", tc.kind, got.topic, tc.topic)
				}
				if len(got.payload) == 0 || bytes.Contains(got.payload, []byte("TP-COMMAND")) || bytes.Contains(got.payload, []byte("tp-internal")) {
					t.Fatalf("%s private frame leaked or payload empty", tc.kind)
				}
				var wire map[string]any
				if err := json.Unmarshal(got.payload, &wire); err != nil {
					t.Fatalf("%s wire payload is not JSON: %v", tc.kind, err)
				}
				if tc.kind == "command" {
					params, ok := wire["params"].(map[string]any)
					if wire["method"] != "set_expected" || !ok || params["value"] != true {
						t.Fatalf("unexpected command wire payload: %s", got.payload)
					}
				} else if (tc.kind == "telemetry" && wire["temperature"] != float64(21)) || (tc.kind == "attribute" && wire["temperature"] != float64(22)) {
					t.Fatalf("unexpected %s wire payload: %s", tc.kind, got.payload)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timed out waiting for %s expected data", tc.kind)
			}

			if err := expectedSvc.Send(ctx, nativeE2EDevice); err != nil {
				t.Fatalf("retry expected-data send for %s: %v", tc.kind, err)
			}
			select {
			case duplicate := <-deliveries:
				t.Fatalf("expected-data Send retry duplicated %s on %q", tc.kind, duplicate.topic)
			case <-time.After(300 * time.Millisecond):
			}

			if tc.kind == "telemetry" {
				waitNativeE2E(t, 10*time.Second, "telemetry protocol acknowledgment", func() bool {
					row, err := brokercommands.New(db).GetScoped(ctx, nativeE2ETenant, id)
					return err == nil && row.State == brokercommands.StateBrokerAccepted
				})
				continue
			}
			ackTopic := "devices/command/response/" + id
			if tc.kind == "attribute" {
				ackTopic = "devices/attributes/set/response/" + id
			}
			publishNativeE2E(t, ctx, device, ackTopic, []byte(`{"result":0}`))
			waitNativeE2E(t, 10*time.Second, tc.kind+" scoped device ACK", func() bool {
				row, err := brokercommands.New(db).GetScoped(ctx, nativeE2ETenant, id)
				return err == nil && row.State == brokercommands.StateDeviceReportedSuccess
			})
		}

		// The established manual control APIs use the same durable wire owner but
		// do not create scheduled expected_datas rows.
		standard := &service.TelemetryData{}
		telemetryValue := `{"temperature":31}`
		if err := standard.TelemetryPutMessage(ctx, claims.ID, &model.PutMessage{DeviceID: nativeE2EDevice, Value: telemetryValue}, "1"); err != nil {
			t.Fatalf("standard telemetry control API: %v", err)
		}
		telemetryRow := latestNativeControlRow(t, ctx, db, "control.telemetry")
		telemetryWire := awaitNativeDelivery(t, deliveries, "devices/telemetry/control/"+number)
		assertNativeJSONField(t, telemetryWire, "temperature", float64(31))
		waitNativeE2E(t, 10*time.Second, "standard telemetry protocol acknowledgment", func() bool {
			row, err := brokercommands.New(db).GetScoped(ctx, nativeE2ETenant, telemetryRow.ID)
			return err == nil && row.State == brokercommands.StateBrokerAccepted
		})

		attributeValue := `{"temperature":32}`
		if err := (&service.AttributeData{}).AttributePutMessage(ctx, claims.ID, &model.AttributePutMessage{DeviceID: nativeE2EDevice, Value: attributeValue}, "1"); err != nil {
			t.Fatalf("standard attribute control API: %v", err)
		}
		attributeRow := latestNativeControlRow(t, ctx, db, "control.attribute")
		attributeTopic := "devices/attributes/set/" + number + "/" + attributeRow.ID
		assertNativeJSONField(t, awaitNativeDelivery(t, deliveries, attributeTopic), "temperature", float64(32))
		publishNativeE2E(t, ctx, device, "devices/attributes/set/response/"+attributeRow.ID, []byte(`{"result":0}`))
		waitNativeE2E(t, 10*time.Second, "standard attribute device ACK", func() bool {
			row, err := brokercommands.New(db).GetScoped(ctx, nativeE2ETenant, attributeRow.ID)
			return err == nil && row.State == brokercommands.StateDeviceReportedSuccess
		})

		getClaims := claims
		if err := (&service.AttributeData{}).AttributeGetMessage(getClaims, &model.AttributeGetMessageReq{DeviceID: nativeE2EDevice, Keys: []string{"temperature"}}); err != nil {
			t.Fatalf("standard attribute get API: %v", err)
		}
		getRow := latestNativeControlRow(t, ctx, db, "control.attribute_get")
		getWire := awaitNativeDelivery(t, deliveries, "devices/attributes/get/"+number)
		var getPayload map[string]any
		if err := json.Unmarshal(getWire, &getPayload); err != nil || fmt.Sprint(getPayload["keys"]) != "[temperature]" {
			t.Fatalf("unexpected attribute-get wire payload %s err=%v", getWire, err)
		}
		waitNativeE2E(t, 10*time.Second, "standard attribute get protocol acknowledgment", func() bool {
			row, err := brokercommands.New(db).GetScoped(ctx, nativeE2ETenant, getRow.ID)
			return err == nil && row.State == brokercommands.StateBrokerAccepted
		})

		wrongClaims := &utils.UserClaims{ID: claims.ID, TenantID: "tenant-native-e2e-other", Authority: claims.Authority}
		payload := `{"temperature":99}`
		if _, err := expectedSvc.Create(ctx, &model.CreateExpectedDataReq{DeviceID: nativeE2EDevice, SendType: "telemetry", Payload: &payload}, wrongClaims); err == nil {
			t.Fatal("cross-tenant expected-data creation was accepted")
		}
	})
}

func latestNativeControlRow(t *testing.T, ctx context.Context, db *gorm.DB, intent string) brokercommands.Row {
	t.Helper()
	var row brokercommands.Row
	if err := db.WithContext(ctx).Where("tenant_id=? AND device_id=? AND intent=?", nativeE2ETenant, nativeE2EDevice, intent).Order("created_at DESC").Take(&row).Error; err != nil {
		t.Fatalf("load standard control receipt %s: %v", intent, err)
	}
	return row
}

func awaitNativeDelivery(t *testing.T, deliveries <-chan nativeExpectedDelivery, topic string) []byte {
	t.Helper()
	select {
	case got := <-deliveries:
		if got.topic != topic {
			t.Fatalf("device received topic %q, want %q", got.topic, topic)
		}
		if len(got.payload) == 0 || bytes.Contains(got.payload, []byte("TP-COMMAND")) || bytes.Contains(got.payload, []byte("tp-internal")) {
			t.Fatalf("private command frame leaked or payload was empty")
		}
		return got.payload
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for native device topic %q", topic)
		return nil
	}
}

func assertNativeJSONField(t *testing.T, payload []byte, key string, want any) {
	t.Helper()
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil || wire[key] != want {
		t.Fatalf("wire JSON field %s=%v want=%v payload=%s err=%v", key, wire[key], want, payload, err)
	}
}

func commandPlanNativeSessionToken(t *testing.T, key, userID, tenant, authority string) string {
	t.Helper()
	claims := utils.UserClaims{ID: userID, TenantID: tenant, Authority: authority, StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix()}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal("sign isolated command-plan user session")
	}
	return token
}

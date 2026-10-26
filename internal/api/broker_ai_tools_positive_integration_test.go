package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"project/internal/brokeraitools"
	"project/internal/model"
	"project/internal/query"
	"project/internal/service"
	"project/pkg/global"
)

// This integration fixture exercises delegated calls through the registered
// HTTP routes against private PostgreSQL/Redis and an explicit Encore contract
// stub. It does not start an MQTT publisher or claim a command was executed.
func TestBrokerAIToolPositiveDescribeTelemetryAndPreview(t *testing.T) {
	db, cleanupDB := openSemanticEvidencePG(t)
	defer cleanupDB()
	redisClient := startBrokerAPITestRedis(t)
	if pool, err := db.DB(); err != nil {
		t.Fatal("database pool")
	} else {
		pool.SetMaxOpenConns(4)
	}

	// Replace the three deliberately small status-test tables with the actual
	// isolated broker schema used by the service paths under test.
	for _, table := range []string{"broker_telemetry_activity", "broker_connection_observations", "broker_transport_bindings"} {
		if err := db.Exec("DROP TABLE IF EXISTS " + table + " CASCADE").Error; err != nil {
			t.Fatalf("drop fixture table %s", table)
		}
	}
	if err := db.AutoMigrate(&model.User{}, &model.DeviceConfig{}, &model.DeviceModelCommand{}, &model.DataScript{}, &model.CommandSetLog{}, &model.DeviceTopicMapping{}); err != nil {
		t.Fatal("migrate command/service fixture")
	}
	entries, err := os.ReadDir("../../sql/broker")
	if err != nil {
		t.Fatal("read broker migrations")
	}
	var migrations []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrations = append(migrations, entry.Name())
		}
	}
	sort.Strings(migrations)
	for _, name := range migrations {
		contents, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", name))
		if err != nil {
			t.Fatalf("read broker migration %s", name)
		}
		if err := db.Exec(strings.ReplaceAll(string(contents), "public.", "")).Error; err != nil {
			t.Fatalf("apply broker migration %s: %v", name, err)
		}
	}

	previousDB, previousRedis, previousEnforcer := global.DB, global.REDIS, global.CasbinEnforcer
	previousQuery := *query.Q
	previousDeviceQuery, previousLogQuery := query.Device, query.CommandSetLog
	previousSettings := map[string]struct {
		value any
		set   bool
	}{}
	for _, key := range []string{"jwt.key", "mqtt.input_profile", "mqtt.route_fencing", "mqtt.ai_tools_enabled", "mqtt.command_plans_enabled", "mqtt.semantic_base_url", "mqtt.semantic_development_http"} {
		previousSettings[key] = struct {
			value any
			set   bool
		}{viper.Get(key), viper.IsSet(key)}
	}
	enforcer, err := casbin.NewEnforcer(filepath.Join("..", "..", "configs", "casbin.conf"))
	if err != nil {
		t.Fatal("load Casbin model")
	}
	global.DB, global.REDIS, global.CasbinEnforcer = db, redisClient, enforcer
	query.SetDefault(db)
	t.Cleanup(func() {
		service.CloseBrokerSemanticIdleConnections()
		global.DB, global.REDIS, global.CasbinEnforcer = previousDB, previousRedis, previousEnforcer
		*query.Q = previousQuery
		query.Device, query.CommandSetLog = previousDeviceQuery, previousLogQuery
		for key, setting := range previousSettings {
			if setting.set {
				viper.Set(key, setting.value)
			} else {
				viper.Set(key, "")
			}
		}
	})

	const tenant, userID, deviceID, configID, templateID = "ai-positive-tenant", "ai-positive-user", "device-a", "ai-positive-config", "ai-positive-template"
	status, authority, protocol := "N", "TENANT_ADMIN", "MQTT"
	if err := db.Create(&model.User{ID: userID, TenantID: stringPtr(tenant), Status: &status, Authority: &authority, PhoneNumber: "fixture", Email: "ai-positive@example.invalid", Password: "unused"}).Error; err != nil {
		t.Fatalf("seed current human user: %v", err)
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES(?,?,?,1,'active')`, tenant, deviceID, "cell-ai-test").Error; err != nil {
		t.Fatal("seed device route")
	}
	if err := db.Create(&model.DeviceConfig{ID: configID, Name: "integration config", TenantID: tenant, DeviceType: "1", DeviceTemplateID: stringPtr(templateID), ProtocolType: &protocol, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("seed device config")
	}
	if err := db.Create(&model.Device{ID: deviceID, TenantID: tenant, Name: stringPtr("active command device"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "ai-positive-number", DeviceConfigID: stringPtr(configID), IsOnline: 0}).Error; err != nil {
		t.Fatal("seed active device")
	}
	if err := db.Create(&model.Device{ID: "device-b", TenantID: tenant, Name: stringPtr("other active device"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "ai-positive-number-b", IsOnline: 0}).Error; err != nil {
		t.Fatal("seed second active device")
	}
	commandName := "set_power"
	if err := db.Create(&model.DeviceModelCommand{ID: "ai-command-definition", TenantID: tenant, DeviceTemplateID: templateID, DataIdentifier: commandName, DataName: &commandName, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("seed command definition")
	}
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,'instance-a','boot-a','conn-a',1,1,true,now(),now())`, tenant, deviceID, "cell-ai-test").Error; err != nil {
		t.Fatal("seed fresh connection evidence")
	}

	const jwtKey = "ai-positive-session-test-key"
	parent := semanticEvidenceJWT(t, jwtKey, userID, tenant, authority)
	if err := redisClient.Set(context.Background(), parent, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed live parent session")
	}
	viper.Set("jwt.key", jwtKey)
	viper.Set("mqtt.input_profile", "ingress_v1")
	viper.Set("mqtt.route_fencing", true)
	viper.Set("mqtt.ai_tools_enabled", true)
	viper.Set("mqtt.command_plans_enabled", true)

	encore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+parent {
			http.Error(w, "unexpected delegated upstream identity", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/" + deviceID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"device_template_id":%q}`, deviceID, tenant, templateID)
		case "/api/device-templates/" + templateID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"thing_model_id":"ai-positive-model","thing_model_snapshot_id":"ai-positive-snapshot"}`, templateID, tenant)
		case "/api/v2/thing-models/ai-positive-model/versions":
			fmt.Fprint(w, `{"items":[{"publishedRevision":3,"snapshotID":"ai-positive-snapshot"}],"page":1,"pageSize":200,"total":1}`)
		case "/api/v2/thing-models/ai-positive-model/versions/3":
			fmt.Fprint(w, `{"modelID":"ai-positive-model","publishedRevision":3,"snapshotID":"ai-positive-snapshot","model":{"name":"Fixture","fields":[{"kind":"TELEMETRY","identifier":"temperature","unit":"°C","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"FLOAT"}},{"kind":"COMMAND","identifier":"set_power","access":{"read":false,"write":true,"observe":false},"parameters":{"schemaVersion":1,"items":[{"identifier":"value","valueType":{"kind":"BOOL"}}]}}]},"resources":{"modelID":"ai-positive-model","revision":0,"profile":{},"webChartConfig":{},"appChartConfig":{}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer encore.Close()
	viper.Set("mqtt.semantic_base_url", encore.URL)
	viper.Set("mqtt.semantic_development_http", true)

	for _, operation := range []string{brokeraitools.Describe, brokeraitools.Telemetry, brokeraitools.Preview} {
		route := service.BrokerAIToolPolicyRoute(operation)
		if _, err := enforcer.AddNamedGroupingPolicy("g2", route, route); err != nil {
			t.Fatalf("configure explicit %s capability", operation)
		}
		if _, err := enforcer.AddPolicy("role-ai-positive", route, "allow"); err != nil {
			t.Fatalf("configure explicit %s permission", operation)
		}
	}
	if _, err := enforcer.AddGroupingPolicy(userID, "role-ai-positive"); err != nil {
		t.Fatal("assign tool role")
	}
	for _, op := range []string{brokeraitools.Describe, brokeraitools.Telemetry, brokeraitools.Preview} {
		route := service.BrokerAIToolPolicyRoute(op)
		if len(enforcer.GetFilteredNamedGroupingPolicy("g2", 0, route)) == 0 {
			t.Fatalf("test Casbin route capability missing for %s", op)
		}
		if allowed, err := enforcer.Enforce(userID, service.BrokerAIToolPolicyRoute(op), "allow"); err != nil || !allowed {
			t.Fatalf("test Casbin policy did not authorize %s: allowed=%v err=%v", op, allowed, err)
		}
	}
	// A trusted, already-processed telemetry receipt is the only query source.
	observed := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	payload := []byte(`{"temperature":22.25}`)
	digest := sha256.Sum256(payload)
	receiptID := uuid.NewString()
	metadata, _ := json.Marshal(map[string]any{"source": "trusted_telemetry_batch", "timestamp": observed.UnixMilli(), "logical_device_id": deviceID})
	processed := observed.Add(time.Second)
	if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,created_at,processed_at) VALUES(?,?,?,'telemetry_sample',?,1,?,?,?,'processed',?,?)`, receiptID, tenant, deviceID, "cell-ai-test", payload, metadata, digest[:], observed, processed).Error; err != nil {
		t.Fatal("seed trusted telemetry receipt")
	}
	if err := db.Exec(`INSERT INTO broker_telemetry_activity(tenant_id,device_id,transport_device_id,broker_cell,route_epoch,receipt_id,observed_at,next_check_at) VALUES(?,?,?,?,1,?,?,?)`, tenant, deviceID, deviceID, "cell-ai-test", receiptID, observed, observed.Add(time.Minute)).Error; err != nil {
		t.Fatal("seed trusted telemetry activity")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterBrokerAIToolRoutes(router)
	call := func(path, body, delegated string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+delegated)
		router.ServeHTTP(recorder, request)
		return recorder
	}
	issue := func(scopes []string, replay bool) brokeraitools.Issued {
		t.Helper()
		body, _ := json.Marshal(service.BrokerAIToolGrantRequest{DeviceIDs: []string{deviceID}, Scopes: scopes, Replay: replay})
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/broker-ai/delegations", strings.NewReader(string(body)))
		request.Header.Set("x-token", parent)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("issue scopes %v replay=%t status=%d body=%s", scopes, replay, recorder.Code, recorder.Body.String())
		}
		var result brokeraitools.Issued
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.Token == "" || result.TenantID != tenant || result.SubjectID != userID {
			t.Fatalf("invalid issued identity/result err=%v result=%+v", err, result)
		}
		return result
	}
	grant := issue([]string{brokeraitools.Describe, brokeraitools.Telemetry, brokeraitools.Preview}, false)
	callTool := func(op, body string, delegated string) *httptest.ResponseRecorder {
		return call("/api/v1/broker-ai/tools/"+op, body, delegated)
	}

	describe := callTool(brokeraitools.Describe, `{"device_id":"`+deviceID+`"}`, grant.Token)
	if describe.Code != http.StatusOK || !strings.Contains(describe.Body.String(), `"tenant_id":"`+tenant+`"`) || !strings.Contains(describe.Body.String(), `"subject_id":"`+userID+`"`) || !strings.Contains(describe.Body.String(), `"device_id":"`+deviceID+`"`) || !strings.Contains(describe.Body.String(), "ai-positive-snapshot") {
		t.Fatalf("delegated describe status=%d body=%s", describe.Code, describe.Body.String())
	}

	start, end := observed.Add(-time.Minute), observed.Add(time.Minute)
	window, _ := json.Marshal(service.BrokerAIToolInput{DeviceID: deviceID, Window: &service.BrokerAIToolWindow{Start: start, End: end, Keys: []string{"temperature"}, Limit: 10}})
	telemetry := callTool(brokeraitools.Telemetry, string(window), grant.Token)
	if telemetry.Code != http.StatusOK || !strings.Contains(telemetry.Body.String(), `"tenant_id":"`+tenant+`"`) || !strings.Contains(telemetry.Body.String(), `"subject_id":"`+userID+`"`) || !strings.Contains(telemetry.Body.String(), `"device_id":"`+deviceID+`"`) || !strings.Contains(telemetry.Body.String(), receiptID) || !strings.Contains(telemetry.Body.String(), `"value":22.25`) {
		t.Fatalf("delegated telemetry status=%d body=%s", telemetry.Code, telemetry.Body.String())
	}
	for _, invalidWindow := range []*service.BrokerAIToolWindow{
		{Start: observed.Add(-25 * time.Hour), End: end, Keys: []string{"temperature"}, Limit: 1},
		{Start: start, End: end, Keys: []string{}, Limit: 1},
		{Start: start, End: end, Keys: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, Limit: 1},
		{Start: start, End: end, Keys: []string{"temperature"}, Limit: 201},
	} {
		body, _ := json.Marshal(service.BrokerAIToolInput{DeviceID: deviceID, Window: invalidWindow})
		if got := callTool(brokeraitools.Telemetry, string(body), grant.Token); got.Code != http.StatusBadRequest {
			t.Fatalf("invalid telemetry bounds status=%d body=%s window=%+v", got.Code, got.Body.String(), invalidWindow)
		}
	}

	commandValue := `{"value":true}`
	commandInput, _ := json.Marshal(service.BrokerAIToolInput{DeviceID: deviceID, Command: &model.PutMessageForCommand{DeviceID: deviceID, Identify: "set_power", Value: &commandValue}})
	var beforeCommands, beforePlans int64
	if err := db.Table("broker_command_requests").Count(&beforeCommands).Error; err != nil {
		t.Fatal("count command requests before preview")
	}
	if err := db.Table("broker_command_plans").Count(&beforePlans).Error; err != nil {
		t.Fatal("count plans before preview")
	}
	preview := callTool(brokeraitools.Preview, string(commandInput), grant.Token)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"authorization_required":true`) || strings.Contains(preview.Body.String(), "message_id") {
		t.Fatalf("delegated preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	if !strings.Contains(preview.Body.String(), `"tenant_id":"`+tenant+`"`) || !strings.Contains(preview.Body.String(), `"subject_id":"`+userID+`"`) || !strings.Contains(preview.Body.String(), `"device_id":"`+deviceID+`"`) {
		t.Fatalf("preview identity/device scope mismatch: %s", preview.Body.String())
	}
	var afterCommands, afterPlans, commandLogs int64
	if err := db.Table("broker_command_requests").Count(&afterCommands).Error; err != nil {
		t.Fatal("count command requests after preview")
	}
	if err := db.Table("broker_command_plans").Count(&afterPlans).Error; err != nil {
		t.Fatal("count plans after preview")
	}
	if err := db.Table("command_set_logs").Count(&commandLogs).Error; err != nil {
		t.Fatal("count command logs after preview")
	}
	if afterCommands != beforeCommands || afterPlans != beforePlans+1 || commandLogs != 0 {
		t.Fatalf("preview side effects commands=%d→%d plans=%d→%d commandLogs=%d; want plan-only and no dispatch/request", beforeCommands, afterCommands, beforePlans, afterPlans, commandLogs)
	}

	// A capability absent from the grant and a Replay credential cannot invoke
	// preview even when the caller's current Casbin route allows it.
	describeOnly := issue([]string{brokeraitools.Describe}, false)
	if got := callTool(brokeraitools.Preview, string(commandInput), describeOnly.Token); got.Code != http.StatusUnauthorized {
		t.Fatalf("ungranted preview status=%d body=%s", got.Code, got.Body.String())
	}
	if got := callTool(brokeraitools.Describe, `{"device_id":"device-b"}`, describeOnly.Token); got.Code != http.StatusUnauthorized {
		t.Fatalf("ungranted same-tenant device status=%d body=%s", got.Code, got.Body.String())
	}
	for _, scope := range []string{brokeraitools.Describe, brokeraitools.Telemetry} {
		replay := issue([]string{scope}, true)
		if got := callTool(scope, func() string {
			if scope == brokeraitools.Describe {
				return `{"device_id":"` + deviceID + `"}`
			}
			return string(window)
		}(), replay.Token); got.Code != http.StatusOK {
			t.Fatalf("replay read-only scope %s status=%d body=%s", scope, got.Code, got.Body.String())
		}
	}
	if got := issueReplayPreview(t, router, parent, deviceID); got != http.StatusUnauthorized {
		t.Fatalf("Replay preview issuer status=%d, want unauthorized", got)
	}
}

func issueReplayPreview(t *testing.T, router *gin.Engine, parent, device string) int {
	t.Helper()
	body, _ := json.Marshal(service.BrokerAIToolGrantRequest{DeviceIDs: []string{device}, Scopes: []string{brokeraitools.Preview}, Replay: true})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/broker-ai/delegations", strings.NewReader(string(body)))
	request.Header.Set("x-token", parent)
	router.ServeHTTP(recorder, request)
	return recorder.Code
}

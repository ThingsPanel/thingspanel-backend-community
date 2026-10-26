package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	jwt "github.com/golang-jwt/jwt"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokercommands"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestCommandPlanServicePreviewAuthorizeSubmitLifecycle(t *testing.T) {
	db := brokerEvidenceTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.DeviceModelCommand{}, &model.DataScript{}, &model.CommandSetLog{}, &model.DeviceTopicMapping{}); err != nil {
		t.Fatal("migrate isolated command plan tables")
	}
	for _, migration := range []string{"006_commands.sql", "013_command_plans.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", migration))
		if err != nil {
			t.Fatal("read isolated command migration")
		}
		if err := db.Exec(strings.ReplaceAll(string(contents), "public.", "")).Error; err != nil {
			t.Fatalf("apply isolated command migration %s: %v", migration, err)
		}
	}

	previousDB, previousRedis, previousEnforcer := global.DB, global.REDIS, global.CasbinEnforcer
	previousDeviceQuery, previousLogQuery := query.Device, query.CommandSetLog
	previousSettings := map[string]struct {
		value any
		set   bool
	}{
		"mqtt.input_profile":             {viper.Get("mqtt.input_profile"), viper.IsSet("mqtt.input_profile")},
		"mqtt.route_fencing":             {viper.Get("mqtt.route_fencing"), viper.IsSet("mqtt.route_fencing")},
		"mqtt.command_plans_enabled":     {viper.Get("mqtt.command_plans_enabled"), viper.IsSet("mqtt.command_plans_enabled")},
		"mqtt.semantic_base_url":         {viper.Get("mqtt.semantic_base_url"), viper.IsSet("mqtt.semantic_base_url")},
		"mqtt.semantic_development_http": {viper.Get("mqtt.semantic_development_http"), viper.IsSet("mqtt.semantic_development_http")},
		"jwt.key":                        {viper.Get("jwt.key"), viper.IsSet("jwt.key")},
	}
	redisClient := startBrokerServiceTestRedis(t)
	global.DB, global.REDIS = db, redisClient
	query.SetDefault(db)
	enforcer, err := casbin.NewEnforcer(filepath.Join("..", "..", "configs", "casbin.conf"))
	if err != nil {
		t.Fatal("load isolated Casbin model")
	}
	global.CasbinEnforcer = enforcer
	t.Cleanup(func() {
		CloseBrokerSemanticIdleConnections()
		global.DB, global.REDIS, global.CasbinEnforcer = previousDB, previousRedis, previousEnforcer
		query.Device, query.CommandSetLog = previousDeviceQuery, previousLogQuery
		_ = redisClient.Close()
		for key, setting := range previousSettings {
			if setting.set {
				viper.Set(key, setting.value)
			} else {
				viper.Set(key, "")
			}
		}
	})

	const tenant, userID, deviceID, configID, templateID = "tenant-a", "command-plan-user", "device-a", "config-a", "template-a"
	status, authority, protocolType := "N", "TENANT_ADMIN", "MQTT"
	if err := db.Create(&model.User{ID: userID, TenantID: stringPtr(tenant), Status: &status, Authority: &authority, PhoneNumber: "fixture", Email: "command-plan@example.invalid", Password: "unused"}).Error; err != nil {
		t.Fatal("seed command operator")
	}
	if err := db.Create(&model.DeviceConfig{ID: configID, Name: "fixture config", TenantID: tenant, DeviceType: "1", DeviceTemplateID: stringPtr(templateID), ProtocolType: &protocolType, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("seed command device config")
	}
	if err := db.Model(&model.Device{}).Where("id=? AND tenant_id=?", deviceID, tenant).Update("device_config_id", configID).Error; err != nil {
		t.Fatal("bind device config")
	}
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,'instance-a','boot-a','connection-a',1,1,true,now(),now())`, tenant, deviceID, "cell-a").Error; err != nil {
		t.Fatal("seed fresh online connection")
	}

	const jwtKey = "isolated-command-plan-jwt-key"
	token := commandPlanSessionToken(t, jwtKey, userID, tenant, authority)
	if err := redisClient.Set(context.Background(), token, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed current authenticated session")
	}
	viper.Set("mqtt.input_profile", "ingress_v1")
	viper.Set("mqtt.route_fencing", true)
	viper.Set("mqtt.command_plans_enabled", true)
	viper.Set("jwt.key", jwtKey)
	var snapshot = "snapshot-a"
	var revision = 1
	encore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/" + deviceID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"device_template_id":%q}`, deviceID, tenant, templateID)
		case "/api/device-templates/" + templateID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"thing_model_id":"model-a","thing_model_snapshot_id":%q}`, templateID, tenant, snapshot)
		case "/api/v2/thing-models/model-a/versions":
			fmt.Fprintf(w, `{"items":[{"publishedRevision":%d,"snapshotID":%q}],"page":1,"pageSize":200,"total":1}`, revision, snapshot)
		case fmt.Sprintf("/api/v2/thing-models/model-a/versions/%d", revision):
			fmt.Fprintf(w, `{"modelID":"model-a","publishedRevision":%d,"snapshotID":%q,"model":{"fields":[{"kind":"COMMAND","identifier":"set_power","access":{"read":false,"write":true,"observe":false},"parameters":{"schemaVersion":1,"items":[{"identifier":"value","valueType":{"kind":"BOOL"}}]}}]},"resources":{"modelID":"model-a","revision":0,"profile":{},"webChartConfig":{},"appChartConfig":{}}}`, revision, snapshot)
		default:
			http.NotFound(w, r)
		}
	}))
	defer encore.Close()
	viper.Set("mqtt.semantic_base_url", encore.URL)
	viper.Set("mqtt.semantic_development_http", true)
	claims := &utils.UserClaims{ID: userID, TenantID: tenant, Authority: authority}
	request := func() *model.PutMessageForCommand {
		value := `{"value":true}`
		return &model.PutMessageForCommand{DeviceID: deviceID, Identify: "set_power", Value: &value}
	}
	preview := func() CommandPlanPreview {
		t.Helper()
		got, err := GroupApp.CommandData.PreviewCommandPlan(context.Background(), claims, token, request())
		if err != nil {
			t.Fatalf("preview command plan: %v", err)
		}
		if !got.AuthorizationRequired || got.PlanID == "" || got.PlanHash == "" || got.SemanticPins.SnapshotID != snapshot {
			t.Fatalf("unexpected command preview: %+v", got)
		}
		return got
	}
	use := func(p CommandPlanPreview, auth, key string) CommandPlanUse {
		return CommandPlanUse{PlanHash: p.PlanHash, AuthorizationID: auth, IdempotencyKey: key}
	}
	enabledMapping := model.DeviceTopicMapping{DeviceConfigID: configID, Name: "unsupported custom mapping", Direction: "downlink", SourceTopic: "custom/source", TargetTopic: "custom/target", Enabled: true}
	if err := db.Create(&enabledMapping).Error; err != nil {
		t.Fatal("seed unsupported custom mapping")
	}
	if _, err := GroupApp.CommandData.PreviewCommandPlan(context.Background(), claims, token, request()); !errors.Is(err, brokercommands.ErrRejected) {
		t.Fatalf("enabled custom mapping before preview=%v, want rejected", err)
	}
	if err := db.Delete(&enabledMapping).Error; err != nil {
		t.Fatal("remove unsupported custom mapping")
	}
	disabledMapping := enabledMapping
	disabledMapping.ID = 0
	disabledMapping.Enabled = false
	if err := db.Create(&disabledMapping).Error; err != nil {
		t.Fatal("seed disabled custom mapping")
	}
	if err := db.Model(&model.DeviceTopicMapping{}).Where("id=?", disabledMapping.ID).Update("enabled", false).Error; err != nil {
		t.Fatal("mark custom mapping disabled")
	}
	_ = preview() // Disabled custom rows do not affect the standard MQTT command path.

	stateChanged := preview()
	if err := db.Exec(`UPDATE broker_connection_observations SET event_seq=2,observed_at=now(),received_at=now() WHERE tenant_id=? AND device_id=?`, tenant, deviceID).Error; err != nil {
		t.Fatal("change fresh route state")
	}
	if _, err := GroupApp.CommandData.AuthorizeCommandPlan(context.Background(), claims, token, stateChanged.PlanID, use(stateChanged, "", "")); !errors.Is(err, brokercommands.ErrFenced) {
		t.Fatalf("authorize after state evidence changed=%v, want fenced", err)
	}

	semanticChanged := preview()
	revision++
	if _, err := GroupApp.CommandData.AuthorizeCommandPlan(context.Background(), claims, token, semanticChanged.PlanID, use(semanticChanged, "", "")); !errors.Is(err, brokercommands.ErrFenced) {
		t.Fatalf("authorize after published semantic changed=%v, want fenced", err)
	}
	revision--

	disabledCaller := preview()
	if err := db.Model(&model.User{}).Where("id=?", userID).Update("status", "Y").Error; err != nil {
		t.Fatal("disable test caller")
	}
	if _, err := GroupApp.CommandData.AuthorizeCommandPlan(context.Background(), claims, token, disabledCaller.PlanID, use(disabledCaller, "", "")); err == nil {
		t.Fatal("disabled caller authorized a plan")
	}
	if err := db.Model(&model.User{}).Where("id=?", userID).Update("status", "N").Error; err != nil {
		t.Fatal("restore test caller")
	}

	valid := preview()
	if _, err := GroupApp.CommandData.SubmitCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, "", "pre-auth")); err == nil {
		t.Fatal("direct submit without human authorization was accepted")
	}
	firstAuth, err := GroupApp.CommandData.AuthorizeCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, "", ""))
	if err != nil || firstAuth == "" {
		t.Fatalf("human authorization=%q err=%v", firstAuth, err)
	}
	secondAuth, err := GroupApp.CommandData.AuthorizeCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, "", ""))
	if err != nil || secondAuth != firstAuth {
		t.Fatalf("authorization was not idempotent: first=%q second=%q err=%v", firstAuth, secondAuth, err)
	}

	if err := db.Exec(`CREATE FUNCTION fail_command_plan_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected command log failure'; END $$; CREATE TRIGGER fail_command_plan_log BEFORE INSERT ON command_set_logs FOR EACH ROW EXECUTE FUNCTION fail_command_plan_log()`).Error; err != nil {
		t.Fatal("install atomic rollback fault")
	}
	if _, err := GroupApp.CommandData.SubmitCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, firstAuth, "plan-key-1")); err == nil {
		t.Fatal("injected log failure did not fail durable submit")
	}
	if err := db.Exec(`DROP TRIGGER fail_command_plan_log ON command_set_logs; DROP FUNCTION fail_command_plan_log()`).Error; err != nil {
		t.Fatal("remove atomic rollback fault")
	}
	var commandRows, logRows int64
	db.Table("broker_command_requests").Count(&commandRows)
	db.Table("command_set_logs").Count(&logRows)
	var stored brokercommands.Plan
	if err := db.Where("id=?", valid.PlanID).Take(&stored).Error; err != nil {
		t.Fatal("read plan after rollback")
	}
	if commandRows != 0 || logRows != 0 || stored.CommandID != nil || stored.AuthorizationID == nil || *stored.AuthorizationID != firstAuth {
		t.Fatalf("rollback left command=%d log=%d command_id=%v auth=%v", commandRows, logRows, stored.CommandID, stored.AuthorizationID)
	}

	messageID, err := GroupApp.CommandData.SubmitCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, firstAuth, "plan-key-1"))
	if err != nil || messageID == "" {
		t.Fatalf("submit authorized plan message=%q err=%v", messageID, err)
	}
	retryID, err := GroupApp.CommandData.SubmitCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, firstAuth, "plan-key-1"))
	if err != nil || retryID != messageID {
		t.Fatalf("same idempotency key retry id=%q err=%v, want %q", retryID, err, messageID)
	}
	if _, err := GroupApp.CommandData.SubmitCommandPlan(context.Background(), claims, token, valid.PlanID, use(valid, firstAuth, "plan-key-2")); err == nil {
		t.Fatal("second idempotency key consumed the same approved plan")
	}
	db.Table("broker_command_requests").Count(&commandRows)
	db.Table("command_set_logs").Count(&logRows)
	if commandRows != 1 || logRows != 1 {
		t.Fatalf("successful plan count command=%d log=%d, want exactly one each", commandRows, logRows)
	}

	if err := db.Delete(&disabledMapping).Error; err != nil {
		t.Fatal("remove disabled custom mapping")
	}
	lateMapping := enabledMapping
	lateMapping.ID = 0
	if err := db.Create(&lateMapping).Error; err != nil {
		t.Fatal("add custom mapping after command acceptance")
	}
	row, err := brokercommands.New(db).GetScoped(context.Background(), tenant, messageID)
	if err != nil {
		t.Fatal("load accepted command for final guard")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var binding struct{ Phase string }
		if err := tx.Raw(`SELECT phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=? FOR UPDATE`, tenant, deviceID).Scan(&binding).Error; err != nil {
			return err
		}
		return validatePlannedCommandFinal(context.Background(), tx, row)
	})
	if !errors.Is(err, brokercommands.ErrRejected) {
		t.Fatalf("final guard after late custom mapping=%v, want rejected", err)
	}
}

func commandPlanSessionToken(t *testing.T, key, userID, tenant, authority string) string {
	t.Helper()
	claims := utils.UserClaims{ID: userID, TenantID: tenant, Authority: authority, StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix()}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal("sign isolated user session")
	}
	return token
}

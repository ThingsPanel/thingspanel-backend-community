package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"project/internal/brokeraitools"
	"project/internal/brokercommands"
	"project/internal/middleware"
	"project/internal/model"
	"project/internal/query"
	"project/internal/service"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestBrokerAIToolHTTPDelegationRevocationAndOperationLogIsolation(t *testing.T) {
	db, cleanupDB := openSemanticEvidencePG(t)
	defer cleanupDB()
	redisClient := startBrokerAPITestRedis(t)

	commandDDL, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "006_commands.sql"))
	if err != nil {
		t.Fatal("read isolated command migration")
	}
	if err := db.Exec(strings.ReplaceAll(string(commandDDL), "public.", "")).Error; err != nil {
		t.Fatal("create private command table")
	}
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatal("create private operation log table")
	}

	previousDB, previousRedis, previousEnforcer := global.DB, global.REDIS, global.CasbinEnforcer
	previousQuery := *query.Q
	previousOperationLog := query.OperationLog
	previousViper := map[string]struct {
		value any
		set   bool
	}{}
	for _, key := range []string{"jwt.key", "mqtt.input_profile", "mqtt.route_fencing", "mqtt.ai_tools_enabled", "mqtt.command_plans_enabled"} {
		previousViper[key] = struct {
			value any
			set   bool
		}{viper.Get(key), viper.IsSet(key)}
	}
	previousLogOutput := logrus.StandardLogger().Out
	previousLogLevel := logrus.GetLevel()
	var logOutput bytes.Buffer

	enforcer, err := casbin.NewEnforcer(filepath.Join("..", "..", "configs", "casbin.conf"))
	if err != nil {
		t.Fatal("load isolated Casbin model")
	}
	global.DB, global.REDIS, global.CasbinEnforcer = db, redisClient, enforcer
	query.SetDefault(db)
	logrus.SetOutput(&logOutput)
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		global.DB, global.REDIS, global.CasbinEnforcer = previousDB, previousRedis, previousEnforcer
		*query.Q = previousQuery
		query.OperationLog = previousOperationLog
		logrus.SetOutput(previousLogOutput)
		logrus.SetLevel(previousLogLevel)
		for key, setting := range previousViper {
			if setting.set {
				viper.Set(key, setting.value)
			} else {
				viper.Set(key, "")
			}
		}
	})

	const tenant, userID, deviceID, jwtKey = "ai-http-tenant", "ai-http-user", "ai-http-device", "ai-http-test-signing-key"
	if err := db.Exec(`INSERT INTO users(id,tenant_id,status,authority) VALUES(?,?,?,?)`, userID, tenant, "N", "TENANT_ADMIN").Error; err != nil {
		t.Fatal("seed human user")
	}
	device := model.Device{ID: deviceID, TenantID: tenant, Name: stringPtr("fixture"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "ai-http-number", IsOnline: 0}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal("seed active device")
	}

	resultRoute := service.BrokerAIToolPolicyRoute(brokeraitools.Result)
	if _, err := enforcer.AddNamedGroupingPolicy("g2", resultRoute, resultRoute); err != nil {
		t.Fatal("add explicit tool capability")
	}
	if _, err := enforcer.AddPolicy("role-ai-test", resultRoute, "allow"); err != nil {
		t.Fatal("add tool allow policy")
	}
	if _, err := enforcer.AddGroupingPolicy(userID, "role-ai-test"); err != nil {
		t.Fatal("assign tool role")
	}

	parentJWT := semanticEvidenceJWT(t, jwtKey, userID, tenant, "TENANT_ADMIN")
	if err := redisClient.Set(context.Background(), parentJWT, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed parent session")
	}
	viper.Set("jwt.key", jwtKey)
	viper.Set("mqtt.input_profile", "ingress_v1")
	viper.Set("mqtt.route_fencing", true)
	viper.Set("mqtt.ai_tools_enabled", true)
	viper.Set("mqtt.command_plans_enabled", true)

	commandID := uuid.NewString()
	row := brokercommands.Row{
		ID: commandID, TenantID: tenant, CallerID: userID, Intent: "device.command.plan", IdempotencyKey: "fixture-key",
		DeviceID: deviceID, TargetDeviceID: deviceID, RouteCell: "cell-a", RouteEpoch: 1,
		CanonicalRequest: []byte(`{"fixture":true}`), CanonicalSHA256: bytes.Repeat([]byte{0x42}, 32),
		Payload: []byte(`{"fixture":"payload must not escape"}`), ExpiresAt: time.Now().Add(time.Hour),
		State: brokercommands.StateDeviceReportedSuccess, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("seed scoped result row")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterBrokerAIToolRoutes(router)
	// This sentinel is deliberately registered after OperationLogs so the test
	// proves that the middleware and private log table are live.
	router.Use(middleware.OperationLogs())
	router.POST("/integration/sentinel", func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: userID, TenantID: tenant, Authority: "TENANT_ADMIN"})
		c.JSON(http.StatusOK, gin.H{"marker": "sentinel-response"})
	})
	claims := &utils.UserClaims{ID: userID, TenantID: tenant, Authority: "TENANT_ADMIN"}
	attachClaims := func(c *gin.Context) {
		c.Set("claims", claims)
		c.Next()
	}
	planAPI := &CommandPlanApi{}
	router.POST("/api/v1/command/datas/plans/:plan_id/authorize", attachClaims, planAPI.Authorize)
	router.POST("/api/v1/command/datas/plans/:plan_id/submit", attachClaims, planAPI.Submit)

	call := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	issuerBody, _ := json.Marshal(service.BrokerAIToolGrantRequest{DeviceIDs: []string{deviceID}, Scopes: []string{brokeraitools.Result}})
	issuedResponse := call(http.MethodPost, "/api/v1/broker-ai/delegations", string(issuerBody), map[string]string{"x-token": parentJWT})
	if issuedResponse.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", issuedResponse.Code, issuedResponse.Body.String())
	}
	if issuedResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("issued credential response missing no-store")
	}
	var issued brokeraitools.Issued
	if err := json.Unmarshal(issuedResponse.Body.Bytes(), &issued); err != nil || issued.Token == "" || issued.ID == "" {
		t.Fatalf("invalid issuer response; decode error=%v", err)
	}
	if issued.TenantID != tenant || issued.SubjectID != userID {
		t.Fatalf("issuer identity=(tenant %q, subject %q), want (%q, %q)", issued.TenantID, issued.SubjectID, tenant, userID)
	}
	if strings.Contains(issuedResponse.Body.String(), parentJWT) {
		t.Fatal("issuer response exposed parent session")
	}

	toolBody := `{"device_id":"` + deviceID + `","message_id":"` + commandID + `"}`
	toolResponse := call(http.MethodPost, "/api/v1/broker-ai/tools/commands.getResult", toolBody, map[string]string{"Authorization": "Bearer " + issued.Token})
	if toolResponse.Code != http.StatusOK {
		t.Fatalf("tool result status=%d body=%s", toolResponse.Code, toolResponse.Body.String())
	}
	if toolResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("tool response missing no-store")
	}
	var toolOutput service.BrokerAIToolOutput
	if err := json.Unmarshal(toolResponse.Body.Bytes(), &toolOutput); err != nil {
		t.Fatalf("decode tool response: %v", err)
	}
	if toolOutput.TenantID != tenant || toolOutput.SubjectID != userID || toolOutput.Operation != brokeraitools.Result || toolOutput.DeviceID != deviceID {
		t.Fatalf("tool response actor/scope=(%q,%q,%q,%q), want (%q,%q,%q,%q)", toolOutput.TenantID, toolOutput.SubjectID, toolOutput.Operation, toolOutput.DeviceID, tenant, userID, brokeraitools.Result, deviceID)
	}
	for _, required := range []string{commandID, brokercommands.StateDeviceReportedSuccess, "created_at"} {
		if !strings.Contains(toolResponse.Body.String(), required) {
			t.Fatalf("tool result missing %q: %s", required, toolResponse.Body.String())
		}
	}
	for _, forbidden := range []string{issued.Token, parentJWT, "payload must not escape", "canonical_request", "caller_id"} {
		if strings.Contains(toolResponse.Body.String(), forbidden) {
			t.Fatalf("tool result leaked %q: %s", forbidden, toolResponse.Body.String())
		}
	}
	t.Run("ChatBridge runtime adapter dispatcher and real CLI", func(t *testing.T) {
		runChatBridgeBrokerToolRuntimeE2E(t, router, issued, tenant, userID, deviceID, commandID)
	})

	useBody := `{"plan_hash":"` + strings.Repeat("a", 64) + `","authorization_id":"` + uuid.NewString() + `","idempotency_key":"opaque-attempt"}`
	for _, path := range []string{
		"/api/v1/command/datas/plans/" + uuid.NewString() + "/authorize",
		"/api/v1/command/datas/plans/" + uuid.NewString() + "/submit",
	} {
		response := call(http.MethodPost, path, useBody, map[string]string{"x-token": issued.Token})
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("opaque credential passed as x-token to %s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	revoked := call(http.MethodPost, "/api/v1/broker-ai/delegations/"+issued.ID+"/revoke", "", map[string]string{"x-token": parentJWT})
	if revoked.Code != http.StatusNoContent || revoked.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revoke status=%d headers=%v body=%s", revoked.Code, revoked.Header(), revoked.Body.String())
	}
	if response := call(http.MethodPost, "/api/v1/broker-ai/tools/commands.getResult", toolBody, map[string]string{"Authorization": "Bearer " + issued.Token}); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked grant tool status=%d body=%s", response.Code, response.Body.String())
	}

	secondResponse := call(http.MethodPost, "/api/v1/broker-ai/delegations", string(issuerBody), map[string]string{"x-token": parentJWT})
	if secondResponse.Code != http.StatusCreated {
		t.Fatalf("second issue status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	var second brokeraitools.Issued
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &second); err != nil || second.Token == "" {
		t.Fatal("decode second issued grant")
	}
	if err := redisClient.Del(context.Background(), parentJWT).Err(); err != nil {
		t.Fatal("revoke parent session")
	}
	if response := call(http.MethodPost, "/api/v1/broker-ai/tools/commands.getResult", toolBody, map[string]string{"Authorization": "Bearer " + second.Token}); response.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out parent tool status=%d body=%s", response.Code, response.Body.String())
	}

	if response := call(http.MethodPost, "/integration/sentinel", `{"marker":"sentinel-request"}`, nil); response.Code != http.StatusOK {
		t.Fatalf("sentinel status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(logOutput.String(), "sentinel-request") || !strings.Contains(logOutput.String(), "sentinel-response") {
		t.Fatalf("OperationLogs sentinel did not execute: %q", logOutput.String())
	}
	for _, forbidden := range []string{issued.Token, second.Token, parentJWT, "payload must not escape"} {
		if strings.Contains(logOutput.String(), forbidden) {
			t.Fatalf("OperationLogs logger exposed %q", forbidden)
		}
	}

	var logs []model.OperationLog
	if err := db.Order("created_at,id").Find(&logs).Error; err != nil {
		t.Fatal("read private operation logs")
	}
	if len(logs) == 0 {
		t.Fatal("OperationLogs did not persist the sentinel")
	}
	foundSentinel := false
	for _, log := range logs {
		requestMessage, responseMessage := "", ""
		if log.RequestMessage != nil {
			requestMessage = *log.RequestMessage
		}
		if log.ResponseMessage != nil {
			responseMessage = *log.ResponseMessage
		}
		joined := requestMessage + responseMessage
		if strings.Contains(joined, "sentinel-request") && strings.Contains(joined, "sentinel-response") {
			foundSentinel = true
		}
		for _, forbidden := range []string{issued.Token, second.Token, parentJWT, "payload must not escape"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("operation_logs persisted %q", forbidden)
			}
		}
	}
	if !foundSentinel {
		t.Fatalf("normal POST sentinel was not persisted: %d rows", len(logs))
	}
}

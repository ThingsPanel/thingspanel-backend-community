package service

import (
	"context"
	"encoding/json"
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
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"project/internal/brokeraitools"
	"project/internal/brokercommands"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestBrokerAIToolsRealSessionPolicyAndResultScoping(t *testing.T) {
	db := brokerEvidenceTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal("migrate isolated users:", err)
	}
	commandDDL, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "006_commands.sql"))
	if err != nil {
		t.Fatal("read command fixture migration:", err)
	}
	if err := db.Exec(strings.ReplaceAll(string(commandDDL), "public.", "")).Error; err != nil {
		t.Fatal("create isolated command table:", err)
	}

	previousDB, previousRedis, previousEnforcer := global.DB, global.REDIS, global.CasbinEnforcer
	previous := map[string]struct {
		value any
		set   bool
	}{
		"mqtt.input_profile":             {viper.Get("mqtt.input_profile"), viper.IsSet("mqtt.input_profile")},
		"mqtt.route_fencing":             {viper.Get("mqtt.route_fencing"), viper.IsSet("mqtt.route_fencing")},
		"mqtt.ai_tools_enabled":          {viper.Get("mqtt.ai_tools_enabled"), viper.IsSet("mqtt.ai_tools_enabled")},
		"mqtt.command_plans_enabled":     {viper.Get("mqtt.command_plans_enabled"), viper.IsSet("mqtt.command_plans_enabled")},
		"mqtt.semantic_base_url":         {viper.Get("mqtt.semantic_base_url"), viper.IsSet("mqtt.semantic_base_url")},
		"mqtt.semantic_development_http": {viper.Get("mqtt.semantic_development_http"), viper.IsSet("mqtt.semantic_development_http")},
		"jwt.key":                        {viper.Get("jwt.key"), viper.IsSet("jwt.key")},
	}
	redisClient := startBrokerServiceTestRedis(t)
	enforcer, err := casbin.NewEnforcer(filepath.Join("..", "..", "configs", "casbin.conf"))
	if err != nil {
		t.Fatal("load isolated Casbin model:", err)
	}
	global.DB, global.REDIS, global.CasbinEnforcer = db, redisClient, enforcer
	t.Cleanup(func() {
		CloseBrokerSemanticIdleConnections()
		global.DB, global.REDIS, global.CasbinEnforcer = previousDB, previousRedis, previousEnforcer
		_ = redisClient.Close()
		for key, setting := range previous {
			if setting.set {
				viper.Set(key, setting.value)
			} else {
				viper.Set(key, "")
			}
		}
	})

	const jwtKey = "isolated-ai-tool-session-signing-key"
	viper.Set("jwt.key", jwtKey)
	viper.Set("mqtt.input_profile", "ingress_v1")
	viper.Set("mqtt.route_fencing", true)
	viper.Set("mqtt.ai_tools_enabled", false)
	viper.Set("mqtt.command_plans_enabled", false)
	viper.Set("mqtt.semantic_base_url", "")
	if _, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), "no-session", BrokerAIToolGrantRequest{DeviceIDs: []string{"device-a"}, Scopes: []string{brokeraitools.Result}}); !errors.Is(err, ErrBrokerSemanticUnsupported) {
		t.Fatalf("AI tools default-off issue error=%v, want unsupported", err)
	}
	viper.Set("mqtt.ai_tools_enabled", true)

	status, admin, userAuthority := "N", "TENANT_ADMIN", "TENANT_USER"
	seedUser := func(id, tenant, authority string) {
		t.Helper()
		st, auth, ten := status, authority, tenant
		if err := db.Create(&model.User{ID: id, TenantID: &ten, Status: &st, Authority: &auth, PhoneNumber: "fixture", Email: id + "@example.invalid", Password: "unused"}).Error; err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	seedUser("ai-owner", "tenant-a", admin)
	seedUser("ai-other", "tenant-a", admin)
	seedUser("ai-cross", "tenant-b", admin)
	seedUser("ai-policy-missing", "tenant-a", admin)
	if err := db.Create(&model.Device{ID: "device-b", TenantID: "tenant-b", DeviceNumber: "device-number-b", IsEnabled: "enabled", ActivateFlag: "active", Voucher: `{}`}).Error; err != nil {
		t.Fatal("seed cross-tenant device:", err)
	}
	if err := db.Create(&model.Device{ID: "device-disabled", TenantID: "tenant-a", DeviceNumber: "device-number-disabled", IsEnabled: "disabled", ActivateFlag: "active", Voucher: `{}`}).Error; err != nil {
		t.Fatal("seed disabled device:", err)
	}

	installPolicy := func(user, operation string) {
		t.Helper()
		route := BrokerAIToolPolicyRoute(operation)
		if _, err := enforcer.AddNamedGroupingPolicy("g2", route, route); err != nil {
			t.Fatalf("install explicit route capability %s: %v", operation, err)
		}
		if _, err := enforcer.AddPolicy("role-ai-tools", route, "allow"); err != nil {
			t.Fatalf("install explicit allow policy %s: %v", operation, err)
		}
		if _, err := enforcer.AddGroupingPolicy(user, "role-ai-tools"); err != nil {
			t.Fatalf("assign tool role for %s: %v", user, err)
		}
	}
	for _, user := range []string{"ai-owner", "ai-other", "ai-cross"} {
		installPolicy(user, brokeraitools.Result)
	}
	installPolicy("ai-owner", brokeraitools.Describe)

	tokenFor := func(user, tenant, authority string) string {
		t.Helper()
		token := commandPlanSessionToken(t, jwtKey, user, tenant, authority)
		if err := redisClient.Set(context.Background(), token, "1", time.Hour).Err(); err != nil {
			t.Fatal("create current Redis session:", err)
		}
		return token
	}
	issue := func(token string, devices []string, scope string) brokeraitools.Issued {
		t.Helper()
		issued, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), token, BrokerAIToolGrantRequest{DeviceIDs: devices, Scopes: []string{scope}})
		if err != nil {
			t.Fatalf("issue %s grant: %v", scope, err)
		}
		return issued
	}
	inputForResult := func(device, id string) BrokerAIToolInput { return BrokerAIToolInput{DeviceID: device, MessageID: id} }
	device := &Device{}

	ownerToken := tokenFor("ai-owner", "tenant-a", admin)
	if _, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), ownerToken, BrokerAIToolGrantRequest{DeviceIDs: []string{"device-a"}, Scopes: []string{brokeraitools.Preview}, Replay: true}); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("replay preview issue error=%v, want unauthorized", err)
	}
	if _, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), ownerToken, BrokerAIToolGrantRequest{DeviceIDs: []string{"device-b"}, Scopes: []string{brokeraitools.Result}}); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("cross-tenant device issue error=%v, want unauthorized", err)
	}
	if _, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), tokenFor("ai-cross", "tenant-b", admin), BrokerAIToolGrantRequest{DeviceIDs: []string{"device-a"}, Scopes: []string{brokeraitools.Result}}); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("cross-tenant caller issue error=%v, want unauthorized", err)
	}
	if _, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), ownerToken, BrokerAIToolGrantRequest{DeviceIDs: []string{"device-disabled"}, Scopes: []string{brokeraitools.Result}}); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("disabled device issue error=%v, want unauthorized", err)
	}
	if _, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), tokenFor("ai-policy-missing", "tenant-a", admin), BrokerAIToolGrantRequest{DeviceIDs: []string{"device-a"}, Scopes: []string{brokeraitools.Result}}); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("unconfigured explicit tool policy error=%v, want unauthorized", err)
	}
	issued := issue(ownerToken, []string{"device-a"}, brokeraitools.Result)
	if issued.TenantID != "tenant-a" || issued.SubjectID != "ai-owner" {
		t.Fatalf("issued binding does not match verified session: tenant=%q subject=%q", issued.TenantID, issued.SubjectID)
	}
	issuedJSON, err := json.Marshal(issued)
	if err != nil || strings.Contains(string(issuedJSON), ownerToken) {
		t.Fatalf("issued response serialization leaked parent JWT: marshalErr=%v", err)
	}

	messageID := uuid.NewString()
	row := brokercommands.Row{ID: messageID, TenantID: "tenant-a", CallerID: "ai-owner", Intent: "fixture", IdempotencyKey: uuid.NewString(), DeviceID: "device-a", TargetDeviceID: "device-a", RouteCell: "cell-a", RouteEpoch: 1, CanonicalRequest: []byte(`{"private":"request-param"}`), CanonicalSHA256: make([]byte, 32), Payload: []byte("private-downlink-payload"), ExpiresAt: time.Now().Add(time.Minute), State: brokercommands.StateFailed, LastError: "private raw backend error", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("seed scoped result row:", err)
	}
	crossTenantRow := row
	crossTenantRow.ID, crossTenantRow.TenantID, crossTenantRow.CallerID = uuid.NewString(), "tenant-b", "ai-cross"
	crossTenantRow.IdempotencyKey = uuid.NewString()
	if err := db.Create(&crossTenantRow).Error; err != nil {
		t.Fatal("seed cross-tenant command row:", err)
	}
	result, err := device.ExecuteBrokerAITool(context.Background(), issued.Token, brokeraitools.Result, inputForResult("device-a", messageID))
	if err != nil || result.Operation != brokeraitools.Result || result.DeviceID != "device-a" {
		t.Fatalf("same-caller scoped result=%+v err=%v", result, err)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil || strings.Contains(string(resultJSON), ownerToken) || strings.Contains(string(resultJSON), issued.Token) || strings.Contains(string(resultJSON), "private-downlink-payload") || strings.Contains(string(resultJSON), "private raw backend error") || strings.Contains(string(resultJSON), "request-param") {
		t.Fatalf("getResult response exposed secret fields: marshalErr=%v", err)
	}

	otherToken := tokenFor("ai-other", "tenant-a", admin)
	otherGrant := issue(otherToken, []string{"device-a"}, brokeraitools.Result)
	if _, err := device.ExecuteBrokerAITool(context.Background(), otherGrant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("other caller result error=%v, want unauthorized", err)
	}
	if _, err := device.ExecuteBrokerAITool(context.Background(), issued.Token, brokeraitools.Result, inputForResult("device-b", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("ungranted device result error=%v, want unauthorized", err)
	}
	if _, err := device.ExecuteBrokerAITool(context.Background(), issued.Token, brokeraitools.Result, inputForResult("device-a", uuid.NewString())); !errors.Is(err, brokercommands.ErrNotFound) {
		t.Fatalf("missing scoped command error=%v, want tenant-scoped not-found without legacy fallback", err)
	}
	if _, err := device.ExecuteBrokerAITool(context.Background(), issued.Token, brokeraitools.Result, inputForResult("device-a", crossTenantRow.ID)); !errors.Is(err, brokercommands.ErrNotFound) {
		t.Fatalf("cross-tenant command error=%v, want tenant-scoped not-found", err)
	}
	if err := GroupApp.Device.RevokeBrokerAIToolDelegation(context.Background(), otherToken, issued.ID); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("wrong-owner service revoke error=%v, want unauthorized", err)
	}
	revokeGrant := issue(ownerToken, []string{"device-a"}, brokeraitools.Result)
	if err := GroupApp.Device.RevokeBrokerAIToolDelegation(context.Background(), ownerToken, revokeGrant.ID); err != nil {
		t.Fatalf("owner service revoke: %v", err)
	}
	if _, err := device.ExecuteBrokerAITool(context.Background(), revokeGrant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
		t.Fatalf("execute after service revoke=%v, want unauthorized", err)
	}

	// Each mutation happens after issuance; Execute must revalidate the parent
	// session, current user row, and explicit capability on every delegated call.
	t.Run("revoked parent JWT session", func(t *testing.T) {
		token := tokenFor("ai-owner", "tenant-a", admin)
		grant := issue(token, []string{"device-a"}, brokeraitools.Result)
		if err := redisClient.Del(context.Background(), token).Err(); err != nil {
			t.Fatal(err)
		}
		if _, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
			t.Fatalf("execution after session revoke=%v, want unauthorized", err)
		}
	})
	t.Run("disabled user", func(t *testing.T) {
		token := tokenFor("ai-owner", "tenant-a", admin)
		grant := issue(token, []string{"device-a"}, brokeraitools.Result)
		if err := db.Model(&model.User{}).Where("id=?", "ai-owner").Update("status", "F").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
			t.Fatalf("execution after user disable=%v, want unauthorized", err)
		}
		_ = db.Model(&model.User{}).Where("id=?", "ai-owner").Update("status", status).Error
	})
	t.Run("disabled device", func(t *testing.T) {
		token := tokenFor("ai-owner", "tenant-a", admin)
		grant := issue(token, []string{"device-a"}, brokeraitools.Result)
		if err := db.Model(&model.Device{}).Where("id=?", "device-a").Update("is_enabled", "disabled").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
			t.Fatalf("execution after device disable=%v, want unauthorized", err)
		}
		_ = db.Model(&model.Device{}).Where("id=?", "device-a").Update("is_enabled", "enabled").Error
	})
	t.Run("changed tenant", func(t *testing.T) {
		token := tokenFor("ai-owner", "tenant-a", admin)
		grant := issue(token, []string{"device-a"}, brokeraitools.Result)
		if err := db.Model(&model.User{}).Where("id=?", "ai-owner").Update("tenant_id", "tenant-b").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
			t.Fatalf("execution after tenant change=%v, want unauthorized", err)
		}
		_ = db.Model(&model.User{}).Where("id=?", "ai-owner").Update("tenant_id", "tenant-a").Error
	})
	t.Run("changed authority", func(t *testing.T) {
		token := tokenFor("ai-owner", "tenant-a", admin)
		grant := issue(token, []string{"device-a"}, brokeraitools.Result)
		if err := db.Model(&model.User{}).Where("id=?", "ai-owner").Update("authority", userAuthority).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
			t.Fatalf("execution after authority change=%v, want unauthorized", err)
		}
		_ = db.Model(&model.User{}).Where("id=?", "ai-owner").Update("authority", admin).Error
	})
	t.Run("revoked capability", func(t *testing.T) {
		token := tokenFor("ai-owner", "tenant-a", admin)
		grant := issue(token, []string{"device-a"}, brokeraitools.Result)
		route := BrokerAIToolPolicyRoute(brokeraitools.Result)
		if _, err := enforcer.RemoveNamedGroupingPolicy("g2", route, route); err != nil {
			t.Fatal(err)
		}
		if _, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Result, inputForResult("device-a", messageID)); !errors.Is(err, brokeraitools.ErrUnauthorized) {
			t.Fatalf("execution after capability revoke=%v, want unauthorized", err)
		}
	})

	for _, escaped := range []bool{false, true} {
		t.Run(fmt.Sprintf("semantic upstream echo rejected escaped=%t", escaped), func(t *testing.T) {
			echoToken := tokenFor("ai-owner", "tenant-a", admin)
			echoJSON := fmt.Sprintf("%q", echoToken)
			if escaped {
				var encoded strings.Builder
				encoded.WriteByte('"')
				for _, char := range echoToken {
					fmt.Fprintf(&encoded, `\u%04x`, char)
				}
				encoded.WriteByte('"')
				echoJSON = encoded.String()
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+echoToken {
					http.Error(w, "unexpected authorization", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/devices/device-a":
					fmt.Fprint(w, `{"id":"device-a","tenant_id":"tenant-a","device_template_id":"template-a"}`)
				case "/api/device-templates/template-a":
					fmt.Fprint(w, `{"id":"template-a","tenant_id":"tenant-a","thing_model_id":"model-a","thing_model_snapshot_id":"snapshot-a"}`)
				case "/api/v2/thing-models/model-a/versions":
					fmt.Fprint(w, `{"items":[{"publishedRevision":1,"snapshotID":"snapshot-a"}],"page":1,"pageSize":200,"total":1}`)
				case "/api/v2/thing-models/model-a/versions/1":
					fmt.Fprintf(w, `{"modelID":"model-a","publishedRevision":1,"snapshotID":"snapshot-a","model":{"session_echo":%s},"resources":{"modelID":"model-a","revision":1}}`, echoJSON)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			viper.Set("mqtt.semantic_base_url", server.URL)
			viper.Set("mqtt.semantic_development_http", true)
			grant, err := GroupApp.Device.IssueBrokerAIToolDelegation(context.Background(), echoToken, BrokerAIToolGrantRequest{DeviceIDs: []string{"device-a"}, Scopes: []string{brokeraitools.Describe}})
			if err != nil {
				t.Fatalf("issue describe grant with local semantic fixture: %v", err)
			}
			t.Cleanup(func() {
				if err := device.RevokeBrokerAIToolDelegation(context.Background(), echoToken, grant.ID); err != nil {
					t.Error("clean up isolated echo-test delegation")
				}
			})
			out, err := device.ExecuteBrokerAITool(context.Background(), grant.Token, brokeraitools.Describe, BrokerAIToolInput{DeviceID: "device-a"})
			if !errors.Is(err, brokeraitools.ErrUnavailable) || len(out.Data) != 0 {
				t.Fatalf("upstream parent-session echo output=%+v err=%v, want unavailable with no data", out, err)
			}
		})
	}
}

func brokerAITestJWT(t *testing.T, key, userID, tenant, authority string) string {
	t.Helper()
	claims := utils.UserClaims{ID: userID, TenantID: tenant, Authority: authority, StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix()}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal("sign isolated AI tool session:", err)
	}
	return token
}

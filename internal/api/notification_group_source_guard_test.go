package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"project/internal/dal"
	"project/internal/middleware/response"
	"project/internal/model"
	"project/internal/query"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const approvedNotificationGroupSourceDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"

func TestLegacyNotificationGroupHTTPRetiresWritesAndPreservesExistingData(t *testing.T) {
	db := openNotificationGroupSourceFixture(t)
	legacyConfig := `{"EMAIL":"ops@example.test"}`
	appConfig := `{"APP":true}`
	for _, row := range []struct{ id, kind, config string }{
		{"migrated-email", model.NoticeType_Email, legacyConfig},
		{"legacy-app", model.NoticeType_APP, appConfig},
	} {
		if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, 'OPEN', ?, 'tenant-a', now(), now())`, row.id, row.id, row.kind, row.config).Error; err != nil {
			t.Fatal("seed isolated notification group")
		}
	}
	key := dal.SourceRouteKey{DeploymentID: "deployment-http-guard", TenantID: "tenant-a", LegacyGroup: "migrated-email"}
	if err := dal.SwitchSourceRoute(context.Background(), key, 0, 7, "native-group", "projection-http-guard-0001"); err != nil {
		t.Fatal("make fixture group Encore-owned")
	}

	bridge, err := service.NewSourceBridge(service.SourceBridgeConfig{
		Enabled: true, BaseURL: "https://core-fixture.example", DeploymentID: key.DeploymentID,
		SourceBearerToken: "fixture-source-token", ProjectionBearerToken: "fixture-projection-token",
	}, groupGuardChecker{})
	if err != nil {
		t.Fatal("construct non-networking enabled bridge fixture")
	}
	defer bridge.Close()
	service.SetActiveSourceBridge(bridge)
	t.Cleanup(func() { service.SetActiveSourceBridge(nil) })

	manager := errcode.NewErrorManager("", "")
	engine := gin.New()
	engine.Use((&response.Handler{ErrManager: manager}).Middleware())
	engine.Use(func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: "actor", TenantID: "tenant-a", Authority: "TENANT_ADMIN"})
		c.Next()
	})
	api := &NotificationGroupApi{}
	engine.POST("/api/v1/notification_group", api.CreateNotificationGroup)
	engine.PUT("/api/v1/notification_group/:id", api.UpdateNotificationGroup)
	engine.DELETE("/api/v1/notification_group/:id", api.DeleteNotificationGroup)
	engine.GET("/api/v1/notification_group/:id", api.HandleNotificationGroupById)
	request := func(method, target, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		var envelope map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode HTTP response envelope: %v", err)
		}
		return recorder.Code, envelope
	}

	for _, write := range []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v1/notification_group", `{"name":"must-not-create"}`},
		{http.MethodPut, "/api/v1/notification_group/migrated-email", `{"name":"must-not-change"}`},
		{http.MethodDelete, "/api/v1/notification_group/migrated-email", ""},
	} {
		status, denied := request(write.method, write.target, write.body)
		if status != http.StatusGone || denied["reason"] != "legacy_notification_group_retired" {
			t.Fatalf("legacy %s was not retired for tenant admin: status=%d response=%v", write.method, status, denied)
		}
	}
	var persistedName string
	if err := db.Raw(`SELECT name FROM notification_groups WHERE id = 'migrated-email'`).Row().Scan(&persistedName); err != nil || persistedName != "migrated-email" {
		t.Fatal("denied HTTP edit changed the Encore-owned legacy row")
	}
	var groupCount int
	if err := db.Raw(`SELECT count(*) FROM notification_groups`).Row().Scan(&groupCount); err != nil || groupCount != 2 {
		t.Fatalf("retired HTTP create/delete changed existing groups: count=%d err=%v", groupCount, err)
	}
	status, read := request(http.MethodGet, "/api/v1/notification_group/migrated-email", "")
	if status != http.StatusOK || read["code"] != float64(errcode.CodeSuccess) {
		t.Fatalf("tenant-scoped legacy group GET was not preserved: status=%d response=%v", status, read)
	}

	status, retiredAPP := request(http.MethodPut, "/api/v1/notification_group/legacy-app", `{"description":"still legacy"}`)
	if status != http.StatusGone || retiredAPP["reason"] != "legacy_notification_group_retired" {
		t.Fatalf("unmigrated APP legacy write was not retired: status=%d response=%v", status, retiredAPP)
	}
	var routeType string
	if err := db.Raw(`SELECT notification_type FROM notification_groups WHERE id = 'legacy-app'`).Row().Scan(&routeType); err != nil || routeType != model.NoticeType_APP {
		t.Fatalf("APP group type changed unexpectedly: type=%q err=%v", routeType, err)
	}
	legacyRoute, err := dal.WithLockedSourceRoute(context.Background(), dal.SourceRouteKey{DeploymentID: key.DeploymentID, TenantID: key.TenantID, LegacyGroup: "legacy-app"}, func(_ *gorm.DB, _ dal.SourceRouteSnapshot) error { return nil })
	if err != nil || legacyRoute.Engine != "legacy" {
		t.Fatalf("unmigrated APP group no longer selects legacy engine: route=%+v err=%v", legacyRoute, err)
	}
}

type groupGuardChecker struct{}

func (groupGuardChecker) CheckEncoreCompatibility(context.Context, *model.NotificationGroup, service.SourceGroupProjectionRequest) error {
	return nil
}

func openNotificationGroupSourceFixture(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN fixture not supplied")
	}
	if dsn != approvedNotificationGroupSourceDSN {
		t.Fatal("refusing non-approved notification fixture DSN")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open approved notification fixture")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal("read approved notification fixture pool")
	}
	var database, user string
	if err := admin.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("notification fixture database identity mismatch")
	}
	schema := "group_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create isolated notification group schema")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse approved notification DSN")
	}
	queryValues := parsed.Query()
	queryValues.Set("options", "-c search_path="+schema)
	parsed.RawQuery = queryValues.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated notification group schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("read isolated notification group pool")
	}
	previousDB := global.DB
	global.DB = db
	query.SetDefault(db)
	t.Cleanup(func() {
		global.DB = previousDB
		if previousDB != nil {
			query.SetDefault(previousDB)
		}
		_ = pool.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	})
	if err := db.Exec(`CREATE TABLE notification_groups (
		id text PRIMARY KEY, name text NOT NULL, notification_type text NOT NULL,
		status text NOT NULL, notification_config text, description text, tenant_id text NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, remark text)`).Error; err != nil {
		t.Fatal("create isolated legacy notification group table")
	}
	if err := db.Exec(`CREATE TABLE casbin_rule (ptype text NOT NULL, v0 text, v1 text, v2 text, v3 text, v4 text, v5 text)`).Error; err != nil {
		t.Fatal("create isolated Casbin policy table")
	}
	for _, file := range []string{"../../sql/23.sql", "../../sql/24.sql", "../../sql/25.sql"} {
		migration, err := os.ReadFile(file)
		if err != nil {
			t.Fatal("read source route migration")
		}
		migrationSQL := strings.ReplaceAll(string(migration), "public.", `"`+schema+`".`)
		if strings.HasSuffix(file, "25.sql") {
			lines := strings.Split(migrationSQL, "\n")
			filtered := lines[:0]
			for _, line := range lines {
				if !strings.HasPrefix(strings.TrimSpace(line), "--") {
					filtered = append(filtered, line)
				}
			}
			for _, statement := range strings.Split(strings.Join(filtered, "\n"), ";") {
				if statement = strings.TrimSpace(statement); statement != "" && db.Exec(statement).Error != nil {
					t.Fatal("apply isolated default policy migration")
				}
			}
		} else if err := db.Exec(migrationSQL).Error; err != nil {
			t.Fatal("apply isolated source route migration")
		}
	}
	return db
}

func TestAllLegacyNotificationGroupPublicWritesAreGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		method  string
		handler gin.HandlerFunc
	}{
		{http.MethodPost, (&NotificationGroupApi{}).CreateNotificationGroup},
		{http.MethodPut, (&NotificationGroupApi{}).UpdateNotificationGroup},
		{http.MethodDelete, (&NotificationGroupApi{}).DeleteNotificationGroup},
	} {
		t.Run(test.method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(test.method, "/api/v1/notification_group", strings.NewReader("{}"))
			test.handler(c)
			if recorder.Code != http.StatusGone || !c.IsAborted() || !strings.Contains(recorder.Body.String(), "legacy_notification_group_retired") {
				t.Fatalf("retired write must stop before any service or database mutation: status=%d", recorder.Code)
			}
		})
	}
}

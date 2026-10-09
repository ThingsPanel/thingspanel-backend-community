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

func TestLegacyNotificationGroupHTTPRejectsEncoreOwnerAndKeepsAPPOnLegacy(t *testing.T) {
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
	engine.PUT("/api/v1/notification_group/:id", func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: "actor", TenantID: "tenant-a", Authority: "TENANT_ADMIN"})
		(&NotificationGroupApi{}).UpdateNotificationGroup(c)
	})
	request := func(id, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/notification_group/"+id, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		var envelope map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode HTTP response envelope: %v", err)
		}
		return recorder.Code, envelope
	}

	status, denied := request("migrated-email", `{"name":"must-not-change"}`)
	if status != http.StatusOK || denied["code"] != float64(errcode.CodeOpDenied) {
		t.Fatalf("legacy HTTP edit of Encore-owned group was not denied: status=%d response=%v", status, denied)
	}
	var persistedName string
	if err := db.Raw(`SELECT name FROM notification_groups WHERE id = 'migrated-email'`).Row().Scan(&persistedName); err != nil || persistedName != "migrated-email" {
		t.Fatal("denied HTTP edit changed the Encore-owned legacy row")
	}

	status, allowed := request("legacy-app", `{"description":"still legacy"}`)
	if status != http.StatusOK || allowed["code"] != float64(errcode.CodeSuccess) {
		t.Fatalf("unmigrated APP legacy edit was not preserved: status=%d response=%v", status, allowed)
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
	for _, file := range []string{"../../sql/23.sql", "../../sql/24.sql"} {
		migration, err := os.ReadFile(file)
		if err != nil {
			t.Fatal("read source route migration")
		}
		migrationSQL := strings.ReplaceAll(string(migration), "public.", `"`+schema+`".`)
		if err := db.Exec(migrationSQL).Error; err != nil {
			t.Fatal("apply isolated source route migration")
		}
	}
	return db
}

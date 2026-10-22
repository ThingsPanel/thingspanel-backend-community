package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const approvedMemberReaderTestDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"

func openNotificationMemberReaderHTTPFixture(t *testing.T) *gorm.DB {
	t.Helper()
	previous := global.DB
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN fixture not supplied")
	}
	if dsn != approvedMemberReaderTestDSN {
		t.Fatal("refusing non-approved notification fixture DSN")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("could not open approved notification fixture")
	}
	adminPool, err := adminDB.DB()
	if err != nil {
		t.Fatal("could not access approved notification fixture")
	}
	var database, user string
	if err := adminDB.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("fixture connection did not match approved database identity")
	}
	schema := "member_reader_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("could not create isolated member-reader schema")
	}
	t.Cleanup(func() {
		global.DB = previous
		_ = adminDB.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("could not parse approved fixture endpoint")
	}
	query := parsed.Query()
	query.Set("options", "-c search_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("could not open isolated member-reader schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("could not access isolated member-reader schema")
	}
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8)
	global.DB = db
	if err := db.Exec(`CREATE TABLE users (
		id text PRIMARY KEY, tenant_id text NOT NULL, status text NOT NULL,
		authority text NOT NULL, email text NOT NULL, phone_number text NOT NULL)`).Error; err != nil {
		t.Fatal("could not create users fixture")
	}
	users := []struct{ id, tenant, status, authority, email, phone string }{
		{"member-active", "tenant-active", "N", "TENANT_USER", "member@example.test", "+12025550111"},
		{"member-inactive", "tenant-active", "F", "TENANT_USER", "inactive@example.test", "+12025550112"},
		{"member-empty", "tenant-active", "N", "TENANT_USER", "   ", "+12025550113"},
		{"member-other", "tenant-other", "N", "TENANT_USER", "other@example.test", "+12025550117"},
		{"tenant-admin-active", "tenant-active", "N", "TENANT_ADMIN", "admin@example.test", "+12025550114"},
		{"tenant-admin-inactive", "tenant-inactive", "F", "TENANT_ADMIN", "admin2@example.test", "+12025550115"},
		{"tenant-user-only", "tenant-no-admin", "N", "TENANT_USER", "user@example.test", "+12025550116"},
	}
	for _, user := range users {
		if err := db.Exec(`INSERT INTO users (id, tenant_id, status, authority, email, phone_number) VALUES (?, ?, ?, ?, ?, ?)`, user.id, user.tenant, user.status, user.authority, user.email, user.phone).Error; err != nil {
			t.Fatal("could not insert users fixture")
		}
	}
	return db
}

func memberReaderFixtureRouter(reader *NotificationMemberReaderApi) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/notification/source-members/:id", reader.GetMemberContact)
	engine.GET("/api/v1/notification/source-tenants/:id", reader.GetTenantPresence)
	return engine
}

func memberReaderRequest(engine http.Handler, method, path, token, deployment string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if deployment != "" {
		request.Header.Set("X-Notification-Deployment", deployment)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestNotificationMemberReaderPrivateAuthenticationRunsBeforeDatabase(t *testing.T) {
	previous := global.DB
	global.DB = nil
	t.Cleanup(func() { global.DB = previous })
	reader := NewNotificationMemberReader(NotificationMemberReaderConfig{
		Token:                "0123456789abcdef0123456789abcdef",
		AllowedDeploymentIDs: []string{"deployment-a"},
		AllowedTenantIDs:     []string{"tenant-a"},
	})
	engine := memberReaderFixtureRouter(reader)
	if recorder := memberReaderRequest(engine, http.MethodGet, "/api/v1/notification/source-members/user-a?tenantId=tenant-a&contactField=email", "", "deployment-a"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential status=%d", recorder.Code)
	}
	if recorder := memberReaderRequest(engine, http.MethodGet, "/api/v1/notification/source-members/user-a?tenantId=tenant-a&contactField=email", "wrong-token", "deployment-a"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credential status=%d", recorder.Code)
	}
	if recorder := memberReaderRequest(engine, http.MethodGet, "/api/v1/notification/source-tenants/tenant-a", "wrong-token", "deployment-a"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("tenant reader checked the database before authentication: status=%d", recorder.Code)
	}
	if recorder := memberReaderRequest(engine, http.MethodGet, "/api/v1/notification/source-members/user-a?tenantId=tenant-a&contactField=email", "0123456789abcdef0123456789abcdef", "deployment-other"); recorder.Code != http.StatusForbidden {
		t.Fatalf("unapproved deployment status=%d", recorder.Code)
	}
}

func TestNotificationMemberAndTenantReaderHTTPWithApprovedFixture(t *testing.T) {
	openNotificationMemberReaderHTTPFixture(t)
	const token = "0123456789abcdef0123456789abcdef"
	reader := NewNotificationMemberReader(NotificationMemberReaderConfig{
		Token:                token,
		AllowedDeploymentIDs: []string{"deployment-a"},
		AllowedTenantIDs:     []string{"tenant-active", "tenant-other", "tenant-inactive", "tenant-no-admin", "tenant-missing"},
	})
	engine := memberReaderFixtureRouter(reader)
	request := func(path string) *httptest.ResponseRecorder {
		return memberReaderRequest(engine, http.MethodGet, path, token, "deployment-a")
	}

	email := request("/api/v1/notification/source-members/member-active?tenantId=tenant-active&contactField=email")
	if email.Code != http.StatusOK {
		t.Fatalf("active member email status=%d body=%s", email.Code, email.Body.String())
	}
	var emailBody map[string]interface{}
	if err := json.Unmarshal(email.Body.Bytes(), &emailBody); err != nil {
		t.Fatal("member response was not valid JSON")
	}
	if len(emailBody) != 4 || emailBody["userId"] != "member-active" || emailBody["tenantId"] != "tenant-active" || emailBody["contactField"] != "email" || emailBody["address"] != "member@example.test" {
		t.Fatalf("member response included wrong or extra fields: %v", emailBody)
	}
	phone := request("/api/v1/notification/source-members/member-active?tenantId=tenant-active&contactField=phone")
	if phone.Code != http.StatusOK || !strings.Contains(phone.Body.String(), `"address":"+12025550111"`) {
		t.Fatalf("active member phone status=%d body=%s", phone.Code, phone.Body.String())
	}
	for _, path := range []string{
		"/api/v1/notification/source-members/member-active?tenantId=tenant-other&contactField=email",
		"/api/v1/notification/source-members/member-inactive?tenantId=tenant-active&contactField=email",
		"/api/v1/notification/source-members/member-empty?tenantId=tenant-active&contactField=email",
		"/api/v1/notification/source-members/member-active?tenantId=tenant-active&contactField=applicationUserId",
		"/api/v1/notification/source-members/missing?tenantId=tenant-active&contactField=email",
	} {
		response := request(path)
		if response.Code != http.StatusNotFound && response.Code != http.StatusBadRequest {
			t.Errorf("unresolvable member request %q status=%d", path, response.Code)
		}
	}
	for _, path := range []string{
		"/api/v1/notification/source-members/member-active?tenantId=tenant-active&contactField=email&extra=x",
		"/api/v1/notification/source-members/member-active?tenantId=tenant-active&tenantId=tenant-active&contactField=email",
	} {
		if response := request(path); response.Code != http.StatusBadRequest {
			t.Errorf("non-strict query %q status=%d", path, response.Code)
		}
	}
	if response := request("/api/v1/notification/source-members/member-active?tenantId=tenant-outside-allowlist&contactField=email"); response.Code != http.StatusForbidden {
		t.Fatalf("tenant outside operator allowlist status=%d", response.Code)
	}

	activeTenant := request("/api/v1/notification/source-tenants/tenant-active")
	if activeTenant.Code != http.StatusOK {
		t.Fatalf("active tenant status=%d body=%s", activeTenant.Code, activeTenant.Body.String())
	}
	var tenantBody map[string]interface{}
	if err := json.Unmarshal(activeTenant.Body.Bytes(), &tenantBody); err != nil {
		t.Fatal("tenant response was not valid JSON")
	}
	if len(tenantBody) != 2 || tenantBody["tenantId"] != "tenant-active" || tenantBody["exists"] != true {
		t.Fatalf("tenant response included wrong or extra fields: %v", tenantBody)
	}
	for _, tenantID := range []string{"tenant-inactive", "tenant-no-admin", "tenant-missing"} {
		if response := request("/api/v1/notification/source-tenants/" + tenantID); response.Code != http.StatusNotFound {
			t.Errorf("inactive or unknown tenant %q status=%d", tenantID, response.Code)
		}
	}
	if response := request("/api/v1/notification/source-tenants/tenant-active?extra=x"); response.Code != http.StatusBadRequest {
		t.Fatalf("tenant route accepted query parameters: status=%d", response.Code)
	}
	if response := request("/api/v1/notification/source-tenants/tenant-outside-allowlist"); response.Code != http.StatusForbidden {
		t.Fatalf("tenant outside allowlist status=%d", response.Code)
	}
	if response := request("/api/v1/notification/source-tenants/tenant-active"); response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("tenant response is missing no-store")
	}
	if response := request("/api/v1/notification/source-members/member-active?tenantId=tenant-active&contactField=email"); response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("member response is missing no-store")
	}
}

func TestNotificationMemberReaderDisabledAndDatabaseFailureResponses(t *testing.T) {
	previous := global.DB
	global.DB = nil
	t.Cleanup(func() { global.DB = previous })
	engine := memberReaderFixtureRouter(NewNotificationMemberReader(NotificationMemberReaderConfig{}))
	if response := memberReaderRequest(engine, http.MethodGet, "/api/v1/notification/source-members/member-a?tenantId=tenant-a&contactField=email", "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured reader did not stay disabled: status=%d", response.Code)
	}
	reader := NewNotificationMemberReader(NotificationMemberReaderConfig{Token: "0123456789abcdef0123456789abcdef", AllowedDeploymentIDs: []string{"deployment-a"}, AllowedTenantIDs: []string{"tenant-a"}})
	engine = memberReaderFixtureRouter(reader)
	response := memberReaderRequest(engine, http.MethodGet, "/api/v1/notification/source-members/member-a?tenantId=tenant-a&contactField=email", "0123456789abcdef0123456789abcdef", "deployment-a")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("database failure status=%d", response.Code)
	}
}

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const approvedSourceCompatDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"

func openSourceCompatPGFixture(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN fixture not supplied")
	}
	if dsn != approvedSourceCompatDSN {
		t.Fatal("refusing non-approved notification fixture DSN")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("could not open approved notification fixture")
	}
	var database, user string
	if err := adminDB.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("fixture connection did not match approved database identity")
	}
	schema := "t08compat_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("could not create isolated fixture schema")
	}
	adminPool, err := adminDB.DB()
	if err != nil {
		t.Fatal("could not access approved fixture pool")
	}
	_ = adminPool.Close()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("could not parse approved fixture DSN")
	}
	queryValues := parsed.Query()
	queryValues.Set("options", "-c search_path="+schema)
	parsed.RawQuery = queryValues.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("could not open isolated fixture schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("could not access isolated fixture pool")
	}
	pool.SetMaxOpenConns(4)
	previousDB := global.DB
	global.DB = db
	query.SetDefault(db)
	t.Cleanup(func() {
		global.DB = previousDB
		if previousDB != nil {
			query.SetDefault(previousDB)
		}
		_ = pool.Close()
		cleanup, cleanupErr := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
		if cleanupErr == nil {
			_ = cleanup.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
			if cleanupPool, poolErr := cleanup.DB(); poolErr == nil {
				_ = cleanupPool.Close()
			}
		}
	})
	if err := db.Exec(`CREATE TABLE notification_groups (
		id text PRIMARY KEY, name text NOT NULL, notification_type text NOT NULL,
		status text NOT NULL, notification_config text, description text, tenant_id text NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, remark text)`).Error; err != nil {
		t.Fatal("could not create legacy group fixture")
	}
	config := `{"EMAIL":"ops@example.test"}`
	if err := db.Create(&model.NotificationGroup{ID: "legacy-a", Name: "legacy", NotificationType: model.NoticeType_Email, Status: "OPEN", NotificationConfig: &config, TenantID: "tenant-a", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal("could not create legacy group row")
	}
	if err := db.Exec(`CREATE TABLE notification_services_config (
		id text PRIMARY KEY, config text, notice_type text NOT NULL, status text NOT NULL, remark text)`).Error; err != nil {
		t.Fatal("could not create legacy service config fixture")
	}
	if err := db.Exec(`INSERT INTO notification_services_config (id, config, notice_type, status) VALUES (?, ?, 'EMAIL', 'OPEN')`, "email-config-a", `{"host":"smtp.example.test","port":587,"from_password":"fixture-only-value","from_email":"sender@example.test","ssl":false}`).Error; err != nil {
		t.Fatal("could not create legacy email account fixture")
	}
	migration, err := os.ReadFile("../../sql/23.sql")
	if err != nil {
		t.Fatal("could not read source route migration")
	}
	migrationSQL := strings.ReplaceAll(string(migration), "public.", `"`+schema+`".`)
	if err := db.Exec(migrationSQL).Error; err != nil {
		t.Fatal("could not create isolated source route tables")
	}
	migration24, err := os.ReadFile("../../sql/24.sql")
	if err != nil {
		t.Fatal("could not read source rollback migration")
	}
	migration24SQL := strings.ReplaceAll(string(migration24), "public.", `"`+schema+`".`)
	if err := db.Exec(migration24SQL).Error; err != nil {
		t.Fatal("could not apply source rollback migration")
	}
}

func TestSwitchToEncoreRequiresExactRevisionAndRechecksLegacyGroupAtCAS(t *testing.T) {
	openSourceCompatPGFixture(t)
	posted := 0
	var snapshotRequest int
	snapshot := emailSnapshotFixture()
	snapshot.SourceDeploymentID = "deployment-test"
	snapshot.TenantID = "tenant-a"
	snapshot.Group.ID = "native-a"
	snapshot.Group.Revision = 6
	snapshot.Group.Bindings = []sourceSnapshotBinding{{BindingID: "binding-a", InstanceID: "instance-a", RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"ops@example.test"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"{{subject}}","text":"{{text}}\n\n---\nThis email was sent by ThingsPanel"}`)}}
	concurrentEdit := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted++
			var request SourceGroupProjectionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode projection request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sourceEnvelope[SourceGroupProjection]{Code: 200, Message: "ok", RequestID: "request-post", Data: SourceGroupProjection{SourceDeploymentID: request.SourceDeploymentID, TenantID: request.TenantID, LegacyGroupID: request.LegacyGroupID, NotificationGroupID: request.NotificationGroupID, GroupRevision: request.GroupRevision, CreatedAt: "2026-10-09T00:00:00Z"}})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == sourceGroupSnapshotPath+"native-a" {
			snapshotRequest++
			if concurrentEdit {
				if err := global.DB.Exec(`UPDATE notification_groups SET notification_config = ?, updated_at = now() + interval '1 second' WHERE id = ?`, `{"EMAIL":"changed@example.test"}`, "legacy-a").Error; err != nil {
					t.Errorf("simulate concurrent legacy edit: %v", err)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sourceGroupSnapshotEnvelope{Code: 200, Message: "ok", RequestID: "request-a", Data: snapshot})
			return
		}
		t.Errorf("unexpected endpoint request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	checker := &EmailSourceCompatibilityChecker{
		fetchSnapshot: func(ctx context.Context, request SourceGroupProjectionRequest) (sourceGroupSnapshot, error) {
			return fetchSourceGroupSnapshot(ctx, client, base, "projection-fixture-token", request)
		},
		legacyConfig: loadLegacyEmailConfig,
	}
	bridge := testSourceBridge(t, server)
	bridge.check = checker
	request := SourceGroupProjectionRequest{SourceDeploymentID: "deployment-test", TenantID: "tenant-a", LegacyGroupID: "legacy-a", NotificationGroupID: "native-a", GroupRevision: 7}
	if err := bridge.SwitchToEncore(context.Background(), request, "projection-key-0001", 0); err == nil {
		t.Fatal("stale native revision was accepted")
	}
	if snapshotRequest != 1 || posted != 0 {
		t.Fatalf("incompatible revision should GET once and never POST projection; GET=%d POST=%d", snapshotRequest, posted)
	}
	snapshot.Group.Revision = 7
	concurrentEdit = true
	if err := bridge.SwitchToEncore(context.Background(), request, "projection-key-0002", 0); err == nil {
		t.Fatal("concurrent legacy recipient edit did not block route switch")
	}
	if snapshotRequest != 2 || posted != 1 {
		t.Fatalf("compatible projection should POST then fail local CAS; GET=%d POST=%d", snapshotRequest, posted)
	}
	var encoreRoutes int64
	if err := global.DB.Raw(`SELECT count(*) FROM notification_source_group_routes WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ? AND engine = 'encore'`, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID).Scan(&encoreRoutes).Error; err != nil || encoreRoutes != 0 {
		t.Fatalf("concurrent legacy edit must leave route on legacy, found encoreRows=%d err=%v", encoreRoutes, err)
	}
}

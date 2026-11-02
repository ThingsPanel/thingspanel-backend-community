package router

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func runtimeRouterFixture(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	root, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal("open isolated PostgreSQL")
	}
	schema := "runtime_gateway_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e = root.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal("create isolated schema")
	}
	db, e := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal("open isolated schema")
	}
	oldDB, oldEnforcer := global.DB, global.CasbinEnforcer
	oldProfile := viper.Get("mqtt.input_profile")
	viper.Set("mqtt.input_profile", "legacy")
	global.DB = db
	enforcer, e := casbin.NewEnforcer("../configs/casbin.conf")
	if e != nil {
		t.Fatal(e)
	}
	global.CasbinEnforcer = enforcer
	t.Cleanup(func() {
		global.DB, global.CasbinEnforcer = oldDB, oldEnforcer
		viper.Set("mqtt.input_profile", oldProfile)
		if conn, e := db.DB(); e == nil {
			conn.Close()
		}
		root.Exec("DROP SCHEMA " + schema + " CASCADE")
		if conn, e := root.DB(); e == nil {
			conn.Close()
		}
	})
	if e := db.AutoMigrate(&model.Device{}, &dal.DeviceIDReservation{}, &model.CommandSetLog{}); e != nil {
		t.Fatal("migrate isolated runtime fixture")
	}
	for _, d := range []model.Device{
		{ID: "local", TenantID: "tenant-a", Voucher: "fixture-local", DeviceNumber: "local", IsEnabled: "enabled", ActivateFlag: "active"},
		{ID: "foreign", TenantID: "tenant-b", Voucher: "fixture-foreign", DeviceNumber: "foreign", IsEnabled: "enabled", ActivateFlag: "active"},
		{ID: "disabled", TenantID: "tenant-a", Voucher: "fixture-disabled", DeviceNumber: "disabled", IsEnabled: "disabled", ActivateFlag: "active"},
		{ID: "tombstoned", TenantID: "tenant-a", Voucher: "fixture-deleted", DeviceNumber: "deleted", IsEnabled: "enabled", ActivateFlag: "active"},
	} {
		if e := db.Create(&d).Error; e != nil {
			t.Fatal(e)
		}
		state := "active"
		if d.ID == "tombstoned" {
			state = "tombstoned"
		}
		if e := db.Create(&dal.DeviceIDReservation{ID: d.ID, TenantID: d.TenantID, State: state, CreatedAt: time.Now()}).Error; e != nil {
			t.Fatal(e)
		}
	}
	return db
}

func runtimeRequest(id, source string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return runtimeRequestContext(context.Background(), id, source, handler)
}
func runtimeRequestContext(ctx context.Context, id, source string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	path := "/api/v1/runtime-scoped/tenant-a/device/detail/" + id
	c.Request = httptest.NewRequest("GET", path, nil).WithContext(ctx)
	c.Params = gin.Params{{Key: "tenantID", Value: "tenant-a"}, {Key: "id", Value: id}, {Key: "message_id", Value: id}}
	c.Set("claims", &utils.UserClaims{ID: "caller", TenantID: "tenant-a", Authority: "TENANT_ADMIN"})
	runtimeScope(source, false, handler)(c)
	return w
}

func TestRuntimeScopedPostgresReadsAndReceiptsEnforceTenantLifecycle(t *testing.T) {
	db := runtimeRouterFixture(t)
	for _, id := range []string{"local", "foreign", "disabled", "tombstoned", "missing"} {
		called := false
		w := runtimeRequest(id, "param:id", func(c *gin.Context) { called = true; c.Status(204); c.Writer.WriteHeaderNow() })
		if id == "local" {
			if !called || w.Code != 204 {
				t.Fatal("authorized read rejected")
			}
		} else if called || w.Code != 403 {
			t.Fatalf("foreign/lifecycle read accepted: %s code=%d", id, w.Code)
		}
	}
	for _, id := range []string{"local", "foreign"} {
		messageID := "message-" + id
		if err := db.Create(&model.CommandSetLog{ID: messageID, DeviceID: id, MessageID: &messageID, CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
		called := false
		w := runtimeRequest(messageID, "command", func(c *gin.Context) { called = true; c.Status(204); c.Writer.WriteHeaderNow() })
		if (id == "local") != called {
			t.Fatalf("receipt ownership confusion: %s status=%d", id, w.Code)
		}
	}
}

func TestRuntimeScopedPostgresReadFencePreventsMidRequestTransfer(t *testing.T) {
	db := runtimeRouterFixture(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan *httptest.ResponseRecorder, 1)
	defer close(release)
	go func() {
		done <- runtimeRequest("local", "param:id", func(c *gin.Context) { close(entered); <-release; c.Status(204); c.Writer.WriteHeaderNow() })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("read fence not entered")
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SET LOCAL lock_timeout='150ms'").Error; e != nil {
			return e
		}
		return tx.Model(&model.Device{}).Where("id=?", "local").Update("tenant_id", "tenant-b").Error
	})
	if err == nil {
		t.Fatal("tenant changed while authorized read held fence")
	}
	release <- struct{}{}
	select {
	case w := <-done:
		if w.Code != 204 {
			t.Fatalf("read failed: %d", w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read fence failed to release")
	}
}

func TestRuntimeScopedPostgresCancellationDoesNotReleaseFenceEarly(t *testing.T) {
	db := runtimeRouterFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan *httptest.ResponseRecorder, 1)
	defer close(release)
	go func() {
		done <- runtimeRequestContext(ctx, "local", "param:id", func(c *gin.Context) { close(entered); <-release; c.Status(204); c.Writer.WriteHeaderNow() })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("read fence not entered")
	}
	cancel()
	err := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SET LOCAL lock_timeout='150ms'").Error; e != nil {
			return e
		}
		return tx.Model(&model.Device{}).Where("id=?", "local").Update("tenant_id", "tenant-b").Error
	})
	if err == nil {
		t.Fatal("request cancellation released scope fence while legacy handler was still running")
	}
	release <- struct{}{}
	select {
	case w := <-done:
		if w.Code != 204 {
			t.Fatalf("read failed: %d", w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled read did not finish")
	}
}

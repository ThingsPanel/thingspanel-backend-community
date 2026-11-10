package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"project/internal/middleware"
	"project/internal/model"
	"project/internal/pluginidentity"
	"project/internal/query"
	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercises the shared HTTP routes with a real local SQL store, without Redis,
// Docker or a running HA instance. In particular, target IDs cannot bypass HA auth.
func TestLegacyPluginCompatibilityAndHAFencing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.ServicePlugin{}, &model.ServiceAccess{}, &model.Device{}, &pluginidentity.Record{}); err != nil {
		t.Fatal(err)
	}
	oldDB := global.DB
	global.DB = db
	query.SetDefault(db)
	t.Cleanup(func() {
		global.DB = oldDB
		if oldDB != nil {
			query.SetDefault(oldDB)
		}
		conn, _ := db.DB()
		conn.Close()
	})
	for _, name := range []string{"homeassistant", "http", "modbus"} {
		p := model.ServicePlugin{ID: name, Name: name, ServiceIdentifier: name, ServiceType: 2}
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		a := model.ServiceAccess{ID: name, ServicePluginID: name, TenantID: "tenant-a", Voucher: "secret-" + name}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		id := name
		d := model.Device{ID: name, TenantID: "tenant-a", Voucher: "device-" + name, DeviceNumber: "number-" + name, ServiceAccessID: &id}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := pluginidentity.NewStore(func() *gorm.DB { return db })
	token, _, err := store.Issue("", "ha", "homeassistant", 1, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	guard := middleware.ManagedPluginRuntimeOrLegacy()
	sp, sa := &ServicePluginApi{}, &ServiceAccessApi{}
	respond := func(c *gin.Context) {
		if !c.IsAborted() {
			data, _ := c.Get("data")
			c.JSON(200, data)
		}
	}
	r.POST("/heartbeat", guard, sp.Heartbeat, respond)
	r.POST("/access/list", guard, sa.HandlePluginServiceAccessList, respond)
	r.POST("/access", guard, sa.HandlePluginServiceAccess, respond)
	r.POST("/devices", guard, func(c *gin.Context) {
		var req model.GetDevicesByProtocolPluginReq
		if err := c.ShouldBindJSON(&req); err != nil {
			t.Fatal(err)
		}
		if _, ok := requirePluginService(c, req.ServiceIdentifier); ok {
			c.JSON(200, gin.H{})
		}
	})
	r.POST("/device/config", guard, func(c *gin.Context) {
		var req model.GetDeviceConfigReq
		if err := c.ShouldBindJSON(&req); err != nil {
			t.Fatal(err)
		}
		if pluginMayReadDevice(c, req) {
			c.JSON(200, gin.H{})
		}
	})
	request := func(path, body, auth string, want int) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s: status=%d want=%d body=%s", path, w.Code, want, w.Body.String())
		}
		return w.Body.String()
	}
	for _, name := range []string{"http", "modbus", "homeassistant"} {
		want := 200
		if name == "homeassistant" {
			want = 503
		}
		for _, path := range []string{"/heartbeat", "/access/list", "/devices"} {
			request(path, `{"service_identifier":"`+name+`"}`, "", want)
		}
		body := request("/access", `{"service_access_id":"`+name+`"}`, "", want)
		if name == "homeassistant" && strings.Contains(body, "secret-homeassistant") {
			t.Fatal("HA credential leaked")
		}
		for _, selector := range []string{`"device_id":"` + name + `"`, `"voucher":"device-` + name + `"`, `"device_number":"number-` + name + `"`} {
			request("/device/config", "{"+selector+"}", "", want)
		}
	}
	request("/access/list", `{"service_identifier":"homeassistant"}`, "Bearer "+token, 200)
	request("/access", `{"service_access_id":"homeassistant"}`, "Bearer "+token, 200)
	request("/access", `{"service_access_id":"http"}`, "Bearer "+token, 403)
	request("/access/list", `{"service_identifier":"http"}`, "Bearer invalid", 503)
	if _, err := store.Revoke("", "ha", time.Now()); err != nil {
		t.Fatal(err)
	}
	request("/access", `{"service_access_id":"homeassistant"}`, "Bearer "+token, 503)
}

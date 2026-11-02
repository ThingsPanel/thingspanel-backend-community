package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/api"
	bc "project/internal/brokercontract"
	"project/internal/dal"
	"project/internal/middleware"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

// Runtime scoped routes are an explicit internal capability list. Never proxy
// arbitrary suffixes or fall back to the legacy URL after a scope rejection.
func registerRuntimeScoped(v1 *gin.RouterGroup) {
	g := v1.Group("runtime-scoped/:tenantID", middleware.JWTAuth())
	d := api.Controllers
	add := func(method, path, source string, ingress bool, handler gin.HandlerFunc) {
		g.Handle(method, path, runtimeScope(source, ingress, handler))
	}
	add("GET", "device/detail/:id", "param:id", false, d.DeviceApi.HandleDeviceByID)
	add("GET", "device/online/status/:id", "param:id", false, d.DeviceApi.HandleDeviceOnlineStatus)
	add("GET", "telemetry/datas/current/:id", "param:id", false, api.RuntimeTelemetryCurrent)
	add("GET", "attribute/datas/:id", "param:id", false, d.AttributeDataApi.HandleDataList)
	add("GET", "event/datas", "query:device_id", false, d.EventDataApi.HandleEventDatasListByPage)
	add("GET", "telemetry/datas/history/page", "query:device_id", false, api.RuntimeTelemetryHistory)
	add("GET", "device/status/history", "query:device_id", false, d.DeviceApi.GetDeviceStatusHistory)
	add("GET", "command/datas/status/:message_id", "command", false, api.RuntimeCommandStatus)
	// The durable command owner must atomically prove active reservation and
	// activation lifecycle before this capability can be enabled. Its current
	// tenant/epoch checks alone are not that proof; never replace them with an
	// unlocked gateway precheck.
	add("POST", "command/datas/pub", "unsupported", false, nil)
	add("POST", "expected/data", "owner:body:device_id", true, d.ExpectedDataApi.CreateExpectedData)
	add("GET", "expected/data/list", "query:device_id", false, d.ExpectedDataApi.HandleExpectedDataList)
	// Debug writes only Redis. They do not upgrade the locked device row.
	add("POST", "device/:device_id/debug", "param:device_id", false, api.RuntimeDeviceDebugSet)
	add("GET", "device/:device_id/debug/status", "param:device_id", false, api.RuntimeDeviceDebugStatus)
	add("GET", "device/:device_id/debug/logs", "param:device_id", false, api.RuntimeDeviceDebugLogs)

	add("PUT", "device", "owner:body:id", false, api.RuntimeUpdateDevice)
	add("DELETE", "device/:id", "owner:param:id", false, d.DeviceApi.DeleteDevice)
	add("POST", "device/update/voucher", "owner:body:device_id", false, api.RuntimeUpdateVoucher)
	add("PUT", "device/sub-remove", "owner:body:sub_device_id", false, api.RuntimeRemoveSubDevice)
	add("DELETE", "expected/data/:id", "owner:param:id", false, api.RuntimeDeleteExpected)
	// Simulation accepts caller-selected transport destinations and publishes
	// asynchronously. Its eventual ingest cannot be fenced by this transaction.
	add("POST", "telemetry/datas/simulation/send", "unsupported", false, nil)
}

func runtimeReject(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": status, "message": message})
}

func runtimeScope(source string, ingress bool, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := c.Get("claims")
		claims, valid := raw.(*utils.UserClaims)
		tenant := c.Param("tenantID")
		if !ok || !valid || claims == nil || strings.TrimSpace(tenant) == "" || tenant != claims.TenantID {
			runtimeReject(c, 403, "runtime tenant scope mismatch")
			return
		}
		// This is an internal admin capability, not a device-user API. Legacy
		// Casbin policy absence is not proof of permission. API-key authentication
		// deliberately supplies TENANT_ADMIN under its existing verified contract.
		if claims.Authority != "TENANT_ADMIN" {
			runtimeReject(c, 403, "runtime scoped tenant administrator required")
			return
		}
		if source == "unsupported" || (ingress && viper.GetString("mqtt.input_profile") != "ingress_v1") {
			runtimeReject(c, 503, "runtime scoped atomic writer unavailable")
			return
		}
		owner := strings.HasPrefix(source, "owner:")
		source = strings.TrimPrefix(source, "owner:")
		deviceID, err := runtimeDeviceID(c, source)
		if err != nil {
			runtimeReject(c, 400, "ambiguous or missing runtime device identity")
			return
		}
		if global.DB == nil || global.CasbinEnforcer == nil {
			runtimeReject(c, 503, "runtime scope unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		if owner {
			if ctx.Err() != nil {
				runtimeReject(c, 408, "runtime request canceled")
				return
			}
			runtimeLegacyRBAC(c, tenant)
			if !c.IsAborted() {
				handler(c)
			}
			return
		}
		// The device SHARE lock covers the entire read or external Redis effect.

		// Scoped reads propagate the bounded request context. The lock-owning
		// transaction must outlive their callback even if the request is canceled;
		// database/sql would otherwise roll it back early and drop authorization
		// locks while the handler was still reading. Lock acquisition itself is
		// bounded below with PostgreSQL lock_timeout.
		fenceContext := context.WithoutCancel(ctx)
		// Acquire the connection with the request deadline before detaching the
		// transaction context; pool exhaustion must not create an unbounded wait.
		sqlDB, dbErr := global.DB.DB()
		if dbErr != nil {
			runtimeReject(c, 503, "runtime scope unavailable")
			return
		}
		conn, dbErr := sqlDB.Conn(ctx)
		if dbErr != nil {
			runtimeReject(c, 503, "runtime scope unavailable")
			return
		}
		defer conn.Close()
		fenceDB := global.DB.Session(&gorm.Session{NewDB: true, Context: fenceContext})
		fenceDB.Statement.ConnPool = conn
		err = fenceDB.Transaction(func(tx *gorm.DB) error {
			if tx.Dialector.Name() != "postgres" {
				return errors.New("runtime scope requires PostgreSQL row locking")
			}
			if err := tx.Exec("SET LOCAL lock_timeout = '3s'").Error; err != nil {
				return err
			}
			if source == "command" {
				deviceID, err = runtimeCommandDevice(tx, c.Param("message_id"), tenant)
				if err != nil {
					return err
				}
			}
			var device model.Device
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND is_enabled=? AND activate_flag=?", deviceID, tenant, "enabled", "active").Take(&device).Error; err != nil {
				return err
			}
			var reservation dal.DeviceIDReservation
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND state='active'", deviceID, tenant).Take(&reservation).Error; err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			runtimeLegacyRBAC(c, tenant)
			if !c.IsAborted() {
				handler(c)
				if ctx.Err() != nil {
					c.Set("data", nil)
					return ctx.Err()
				}
			}
			return nil
		})
		if err != nil && !c.Writer.Written() {
			runtimeReject(c, 403, "runtime device unavailable in tenant")
		}
	}
}

func runtimeDeviceID(c *gin.Context, source string) (string, error) {
	bad := errors.New("invalid device identity")
	// ParseQuery reports invalid escaping instead of silently discarding it.
	values, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil {
		return "", bad
	}
	for _, key := range []string{"device_id", "id", "tenant_id", "tenantID", "parent_id", "sub_device_id", "device_ids"} {
		if v, ok := values[key]; ok && (source != "query:"+key || len(v) != 1) {
			return "", bad
		}
	}
	switch {
	case strings.HasPrefix(source, "param:"):
		id := c.Param(strings.TrimPrefix(source, "param:"))
		if strings.TrimSpace(id) == "" {
			return "", bad
		}
		return id, nil
	case strings.HasPrefix(source, "query:"):
		id := values.Get(strings.TrimPrefix(source, "query:"))
		if strings.TrimSpace(id) == "" {
			return "", bad
		}
		return id, nil
	case source == "command":
		if strings.TrimSpace(c.Param("message_id")) == "" {
			return "", bad
		}
		return "", nil
	case strings.HasPrefix(source, "body:"):
		data, e := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if e != nil || len(data) > 1<<20 || bc.ValidateUniqueJSON(data) != nil {
			return "", bad
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(data, &body) != nil || body == nil {
			return "", bad
		}
		key := strings.TrimPrefix(source, "body:")
		// encoding/json matches struct fields case-insensitively. Reject aliases
		// so authorization and the eventual handler cannot see different IDs.
		for k := range body {
			for _, identity := range []string{"device_id", "id", "tenant_id", "parent_id", "sub_device_id", "device_ids"} {
				if strings.EqualFold(k, identity) && k != key && !(source == "body:id" && k == "parent_id") {
					return "", bad
				}
			}
		}
		var id string
		if json.Unmarshal(body[key], &id) != nil || strings.TrimSpace(id) == "" {
			return "", bad
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(data))
		return id, nil
	}
	return "", bad
}

// Resolve receipt ownership on the server; caller-supplied device IDs are never
// accepted for a message ID. Lock the receipt so its association cannot move.
func runtimeCommandDevice(tx *gorm.DB, messageID, tenant string) (string, error) {
	var row struct{ DeviceID string }
	if viper.GetString("mqtt.input_profile") == "ingress_v1" {
		err := tx.Table("broker_command_requests").Clauses(clause.Locking{Strength: "SHARE"}).Select("device_id").Where("id=? AND tenant_id=?", messageID, tenant).Take(&row).Error
		if err == nil {
			return row.DeviceID, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
	}
	err := tx.Table("command_set_logs").Clauses(clause.Locking{Strength: "SHARE"}).Select("device_id").Where("message_id=?", messageID).Take(&row).Error
	return row.DeviceID, err
}

// Apply the original resource policy after scope authorization; a new prefix
// must not bypass existing Casbin policy lookup.
func runtimeLegacyRBAC(c *gin.Context, tenant string) {
	saved := c.Request.URL.Path
	parts := strings.SplitN(saved, "/runtime-scoped/"+tenant+"/", 2)
	if len(parts) != 2 {
		runtimeReject(c, 400, "invalid runtime scoped path")
		return
	}
	c.Request.URL.Path = "/api/v1/" + parts[1]
	middleware.CasbinRBAC()(c)
	c.Request.URL.Path = saved
}

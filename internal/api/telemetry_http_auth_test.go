package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware"
	"project/internal/middleware/response"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
	tptodb "project/third_party/grpc/tptodb_client"
	pb "project/third_party/grpc/tptodb_client/grpc_tptodb"
)

type telemetryCountingConn struct{ methods []string }

func (f *telemetryCountingConn) Invoke(_ context.Context, method string, _, reply interface{}, _ ...grpc.CallOption) error {
	f.methods = append(f.methods, method)
	switch r := reply.(type) {
	case *pb.GetDeviceAttributesCurrentsReply:
		r.Data = "[]"
	case *pb.GetDeviceHistoryReply:
		r.Data = "[]"
	case *pb.GetDeviceHistoryWithPageAndPageReply:
		r.Data = "[]"
	case *pb.GetDeviceKVDataWithNoAggregateReply:
		r.Data = "[]"
	case *pb.GetDeviceKVDataWithAggregateReply:
		r.Data = "[]"
	default:
		return fmt.Errorf("unexpected RPC %s", method)
	}
	return nil
}

func (*telemetryCountingConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("unexpected streaming RPC")
}

// This fixture is a deliberately limited subset of sql/1.sql, not a migration test.
func openTelemetryReadGuardPostgres(t *testing.T, recorder logger.Interface) *gorm.DB {
	t.Helper()
	const fixtureDSN = "postgres://telemetry_read_guard_test:fixture-only-password@127.0.0.1:25671/telemetry_read_guard_test?sslmode=disable"
	dsn := os.Getenv("TELEMETRY_READ_GUARD_TEST_DSN")
	if dsn == "" {
		t.Skip("TELEMETRY_READ_GUARD_TEST_DSN not supplied")
	}
	if dsn != fixtureDSN {
		t.Fatal("refusing non-fixture telemetry PostgreSQL DSN")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated PostgreSQL fixture")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal("get fixture pool")
	}
	t.Cleanup(func() { _ = adminPool.Close() })
	var database, user string
	if err := admin.Raw("SELECT current_database(), current_user").Row().Scan(&database, &user); err != nil || database != "telemetry_read_guard_test" || user != "telemetry_read_guard_test" {
		t.Fatal("fixture database identity mismatch")
	}
	schema := "telemetry_guard_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create isolated schema")
	}
	t.Cleanup(func() {
		if err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; err != nil {
			t.Error("drop isolated schema failed")
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse fixture DSN")
	}
	q := u.Query()
	q.Set("options", "-c search_path="+schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: recorder})
	if err != nil {
		t.Fatal("open isolated schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("get isolated schema pool")
	}
	t.Cleanup(func() { _ = pool.Close() })
	for _, ddl := range []string{
		`CREATE TABLE devices (id varchar(36) PRIMARY KEY, tenant_id varchar(36) NOT NULL, voucher varchar(500) NOT NULL UNIQUE, device_number varchar(36) NOT NULL UNIQUE, is_enabled varchar(36) NOT NULL, activate_flag varchar(36) NOT NULL, is_online int2 NOT NULL DEFAULT 0)`,
		`CREATE TABLE telemetry_current_datas (device_id varchar(36) NOT NULL, "key" varchar(255) NOT NULL, ts timestamptz(6) NOT NULL, bool_v bool, number_v float8, string_v text, tenant_id varchar(36), UNIQUE(device_id, "key"))`,
		`CREATE TABLE telemetry_datas (device_id varchar(36) NOT NULL, "key" varchar(255) NOT NULL, ts bigint NOT NULL, bool_v bool, number_v float8, string_v text, tenant_id varchar(36), UNIQUE(device_id, "key", ts))`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal("create limited fixture tables")
		}
	}
	return db
}

func TestTelemetryHTTPDeviceAuthorization(t *testing.T) {
	runTelemetryHTTPDeviceAuthorization(t, false)
}

func TestTelemetryHTTPDeviceAuthorizationPostgres(t *testing.T) {
	runTelemetryHTTPDeviceAuthorization(t, true)
}

func runTelemetryHTTPDeviceAuthorization(t *testing.T, usePostgres bool) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	redisClient := startWSAuthTestRedis(t)
	t.Setenv("TP_AUTH_OWNER", "")
	recorder := &wsAuthSQLRecorder{Interface: logger.New(log.New(io.Discard, "", 0), logger.Config{LogLevel: logger.Info})}
	var db *gorm.DB
	var err error
	if usePostgres {
		db = openTelemetryReadGuardPostgres(t, recorder)
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: recorder})
	}
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if !usePostgres {
		if err = db.AutoMigrate(&model.Device{}, &model.TelemetryCurrentData{}, &model.TelemetryData{}); err != nil {
			t.Fatal(err)
		}
	}
	oldQ, oldDevice, oldCurrent, oldTelemetry := *query.Q, query.Device, query.TelemetryCurrentData, query.TelemetryData
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	query.TelemetryCurrentData = &query.Q.TelemetryCurrentData
	query.TelemetryData = &query.Q.TelemetryData
	oldDB, oldRedis := global.DB, global.REDIS
	global.DB, global.REDIS = db, redisClient
	t.Cleanup(func() {
		*query.Q = oldQ
		query.Device, query.TelemetryCurrentData, query.TelemetryData = oldDevice, oldCurrent, oldTelemetry
		global.DB, global.REDIS = oldDB, oldRedis
	})
	previousType, previousJWT, previousTimeout := viper.Get("grpc.tptodb_type"), viper.Get("jwt.key"), viper.Get("session.timeout")
	viper.Set("jwt.key", "isolated-http-test-signing-key")
	viper.Set("session.timeout", 60)
	t.Cleanup(func() {
		viper.Set("grpc.tptodb_type", previousType)
		viper.Set("jwt.key", previousJWT)
		viper.Set("session.timeout", previousTimeout)
	})
	adminToken, err := utils.NewJWT([]byte("isolated-http-test-signing-key")).GenerateToken(utils.UserClaims{ID: "root", Authority: "SYS_ADMIN"})
	if err != nil {
		t.Fatal(err)
	}
	if err := redisClient.Set(context.Background(), adminToken, "1", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []model.Device{{ID: "own", TenantID: "tenant-a", DeviceNumber: "own", Voucher: "voucher-own"}, {ID: "foreign", TenantID: "tenant-b", DeviceNumber: "foreign", Voucher: "voucher-foreign"}} {
		if err := db.Exec("INSERT INTO devices (id, tenant_id, device_number, voucher, is_enabled, activate_flag, is_online) VALUES (?, ?, ?, ?, ?, ?, ?)", d.ID, d.TenantID, d.DeviceNumber, d.Voucher, "enabled", "active", 0).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Revocation checks use the primary database; cached allow entries alone
	// must never grant access.
	if err := db.AutoMigrate(&model.OpenAPIKey{}); err != nil {
		t.Fatal(err)
	}
	status, creator := int16(1), "user-a"
	if err := db.Create(&model.OpenAPIKey{ID: "fixture-key", APIKey: "test-key", TenantID: "tenant-a", Name: "fixture", Status: &status, CreatedID: &creator}).Error; err != nil {
		t.Fatal(err)
	}
	handler, err := response.NewHandler("../../configs/messages.yaml", "../../configs/messages_str.yaml")
	if err != nil {
		t.Fatal(err)
	}
	telemetry, device := &TelemetryDataApi{}, &DeviceApi{}
	type endpoint struct {
		name, path, query string
		handler           gin.HandlerFunc
	}
	endpoints := []endpoint{
		{"current", "/current/:id", "", telemetry.HandleCurrentData},
		{"keys", "/keys", "device_id=%s&keys=temp", telemetry.HandleCurrentDataKeys},
		{"detail", "/detail/:id", "", telemetry.ServeCurrentDetailData},
		{"history", "/history", "device_id=%s&key=temp&start_time=1&end_time=2", telemetry.ServeHistoryData},
		{"pagination", "/pagination", "device_id=%s&key=temp&start_time=1&end_time=2", telemetry.ServeHistoryDataByPage},
		{"page", "/page", "device_id=%s&key=temp&start_time=1&end_time=2&export_excel=true", telemetry.ServeHistoryDataByPage},
		{"page-v2", "/page-v2", "device_id=%s&key=temp&start_time=1&end_time=2", telemetry.ServeHistoryDataByPageV2},
		{"statistic", "/statistic", "device_id=%s&key=temp&time_range=last_1h&aggregate_window=no_aggregate", telemetry.ServeStatisticData},
		{"map", "/map/:id", "", device.HandleMapTelemetry},
		{"chart", "/chart", "device_id=%s&data_type=telemetry&data_mode=latest&key=temp", device.HandleDeviceMetricsChart},
	}
	claims := &utils.UserClaims{ID: "user-a", TenantID: "tenant-a", Authority: "TENANT_ADMIN"}
	call := func(ep endpoint, id string, who *utils.UserClaims, key bool) (int, []string) {
		t.Helper()
		r := gin.New()
		r.Use(handler.Middleware())
		r.Use(func(c *gin.Context) {
			if key || (who != nil && who.Authority == "SYS_ADMIN") {
				middleware.JWTAuth()(c)
				return
			} else if who != nil {
				c.Set("claims", who)
			}
			c.Next()
		})
		r.GET(ep.path, ep.handler)
		path := strings.Replace(ep.path, ":id", url.PathEscape(id), 1)
		if ep.query != "" {
			path += "?" + fmt.Sprintf(ep.query, url.QueryEscape(id))
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if key {
			req.Header.Set("x-api-key", "test-key")
		} else if who != nil && who.Authority == "SYS_ADMIN" {
			req.Header.Set("x-token", adminToken)
		}
		out := httptest.NewRecorder()
		recorder.reset()
		r.ServeHTTP(out, req)
		var payload struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s response %s: %v", ep.name, out.Body.String(), err)
		}
		return payload.Code, recorder.snapshot()
	}
	fake := &telemetryCountingConn{}
	oldClient := tptodb.TptodbClient
	tptodb.TptodbClient = pb.NewThingsPanelClient(fake)
	t.Cleanup(func() { tptodb.TptodbClient = oldClient })
	for _, tc := range []struct {
		ep     endpoint
		method string
	}{
		{endpoints[0], pb.ThingsPanel_GetDeviceAttributesCurrents_FullMethodName},
		{endpoints[1], pb.ThingsPanel_GetDeviceAttributesCurrents_FullMethodName},
		{endpoints[2], pb.ThingsPanel_GetDeviceAttributesCurrents_FullMethodName},
		{endpoints[3], pb.ThingsPanel_GetDeviceHistory_FullMethodName},
		{endpoints[4], pb.ThingsPanel_GetDeviceHistoryWithPageAndPage_FullMethodName},
		{endpoints[6], pb.ThingsPanel_GetDeviceHistoryWithPageAndPage_FullMethodName},
		{endpoints[7], pb.ThingsPanel_GetDeviceKVDataWithNoAggregate_FullMethodName},
		{endpoint{"statistic-aggregate", "/statistic", "device_id=%s&key=temp&time_range=last_1h&aggregate_window=1m&aggregate_function=avg", telemetry.ServeStatisticData}, pb.ThingsPanel_GetDeviceKVDataWithAggregate_FullMethodName},
	} {
		t.Run("tsdb-"+tc.ep.name, func(t *testing.T) {
			viper.Set("grpc.tptodb_type", "TSDB")
			before := len(fake.methods)
			code, _ := call(tc.ep, "own", claims, false)
			if code != errcode.CodeSuccess || len(fake.methods) != before+1 || fake.methods[before] != tc.method {
				t.Fatalf("owner code=%d RPCs=%v", code, fake.methods[before:])
			}
			for _, id := range []string{"foreign", "missing"} {
				before = len(fake.methods)
				code, _ = call(tc.ep, id, claims, false)
				if code != errcode.CodeNoPermission || len(fake.methods) != before {
					t.Fatalf("id=%s code=%d RPCs=%v", id, code, fake.methods[before:])
				}
			}
		})
	}
	if usePostgres {
		t.Run("database-error-fails-closed", func(t *testing.T) {
			if err := db.Exec("ALTER TABLE devices RENAME TO fixture_hidden_devices").Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Exec("ALTER TABLE fixture_hidden_devices RENAME TO devices").Error; err != nil {
					t.Error(err)
				}
			}()
			viper.Set("grpc.tptodb_type", "TSDB")
			before := len(fake.methods)
			code, sql := call(endpoints[0], "own", claims, false)
			if code != errcode.CodeNoPermission || len(fake.methods) != before || len(sql) == 0 {
				t.Fatalf("DB error code=%d RPCs=%v SQL=%v", code, fake.methods[before:], sql)
			}
		})
	}
	for _, tc := range []struct {
		mode    string
		methods []string
	}{
		{"latest", []string{pb.ThingsPanel_GetDeviceAttributesCurrents_FullMethodName}},
		{"history", []string{pb.ThingsPanel_GetDeviceAttributesCurrents_FullMethodName, pb.ThingsPanel_GetDeviceKVDataWithNoAggregate_FullMethodName}},
	} {
		t.Run("tsdb-chart-"+tc.mode, func(t *testing.T) {
			viper.Set("grpc.tptodb_type", "TSDB")
			ep := endpoint{"chart-" + tc.mode, "/chart", "device_id=%s&data_type=telemetry&data_mode=" + tc.mode + "&key=temp", device.HandleDeviceMetricsChart}
			before := len(fake.methods)
			code, _ := call(ep, "own", claims, false)
			got := fake.methods[before:]
			if code != errcode.CodeSuccess || len(got) != len(tc.methods) {
				t.Fatalf("owner code=%d RPCs=%v", code, got)
			}
			for i, method := range tc.methods {
				if got[i] != method {
					t.Fatalf("RPCs=%v expected=%v", got, tc.methods)
				}
			}
			for _, id := range []string{"foreign", "missing"} {
				before = len(fake.methods)
				code, _ = call(ep, id, claims, false)
				if code != errcode.CodeNoPermission || len(fake.methods) != before {
					t.Fatalf("id=%s code=%d RPCs=%v", id, code, fake.methods[before:])
				}
			}
		})
	}
	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			for _, id := range []string{"foreign", "missing"} {
				viper.Set("grpc.tptodb_type", "TSDB")
				code, sql := call(ep, id, claims, false)
				if code != errcode.CodeNoPermission {
					t.Fatalf("id=%s code=%d", id, code)
				}
				for _, q := range sql {
					if strings.Contains(q, "telemetry_") || strings.Contains(q, "attribute_datas") || strings.Contains(q, "event_datas") {
						t.Fatalf("unauthorized data query: %s", q)
					}
				}
			}
			viper.Set("grpc.tptodb_type", "")
			// The chart handler requires a claims object before this guard. Exercise
			// an invalid identity there; other handlers also tolerate missing claims.
			var invalidClaims *utils.UserClaims
			if ep.name == "chart" {
				invalidClaims = &utils.UserClaims{}
			}
			code, _ := call(ep, "own", invalidClaims, false)
			if code != errcode.CodeNoPermission {
				t.Fatalf("missing identity code=%d", code)
			}
		})
	}
	current := endpoints[0]
	viper.Set("grpc.tptodb_type", "")
	for _, tc := range []struct {
		name, id string
		who      *utils.UserClaims
		key      bool
		allowed  bool
	}{{"owner", "own", claims, false, true}, {"foreign key", "foreign", nil, true, false}, {"owner key", "own", nil, true, true}, {"admin", "foreign", &utils.UserClaims{ID: "root", Authority: "SYS_ADMIN"}, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := call(current, tc.id, tc.who, tc.key)
			if (code == errcode.CodeSuccess) != tc.allowed {
				t.Fatalf("code=%d allowed=%v", code, tc.allowed)
			}
		})
	}
	batch := endpoint{"batch", "/batch", "", telemetry.ServeStatisticDataByDeviceId}
	for _, ids := range []string{"device_ids=own&device_ids=foreign&keys=a&keys=b", "device_ids=own&device_ids=missing&keys=a&keys=b"} {
		r := gin.New()
		r.Use(handler.Middleware())
		r.Use(func(c *gin.Context) { c.Set("claims", claims); c.Next() })
		r.GET(batch.path, batch.handler)
		out := httptest.NewRecorder()
		recorder.reset()
		r.ServeHTTP(out, httptest.NewRequest("GET", "/batch?"+ids+"&time_type=hour&aggregate_method=count", nil))
		var p struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &p); err != nil || p.Code != errcode.CodeNoPermission {
			t.Fatalf("batch response=%s err=%v", out.Body.String(), err)
		}
		for _, q := range recorder.snapshot() {
			if strings.Contains(q, "telemetry_datas") {
				t.Fatalf("batch leaked data query %s", q)
			}
		}
	}
	for _, kind := range []string{"attribute", "event", "command"} {
		ep := endpoint{"chart-" + kind, "/chart", "device_id=%s&data_type=" + kind + "&data_mode=latest&key=temp", device.HandleDeviceMetricsChart}
		code, sql := call(ep, "foreign", claims, false)
		if code != errcode.CodeNoPermission {
			t.Fatalf("chart %s code=%d", kind, code)
		}
		for _, q := range sql {
			if strings.Contains(q, "attribute_datas") || strings.Contains(q, "event_datas") || strings.Contains(q, "telemetry_") {
				t.Fatalf("unauthorized chart data query: %s", q)
			}
		}
	}
	// Duplicate device IDs are retained because each position carries a distinct key.
	r := gin.New()
	r.Use(handler.Middleware())
	r.Use(func(c *gin.Context) { c.Set("claims", claims); c.Next() })
	r.GET(batch.path, batch.handler)
	out := httptest.NewRecorder()
	recorder.reset()
	r.ServeHTTP(out, httptest.NewRequest("GET", "/batch?device_ids=own&device_ids=own&keys=a&keys=b&time_type=hour&aggregate_method=count", nil))
	var payload struct {
		Code int `json:"code"`
		Data []struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &payload); err != nil || payload.Code != errcode.CodeSuccess || len(payload.Data) != 2 || payload.Data[0].Key != "a" || payload.Data[1].Key != "b" {
		t.Fatalf("batch pair order response=%s err=%v", out.Body.String(), err)
	}
}

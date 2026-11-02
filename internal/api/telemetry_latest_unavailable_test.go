package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware/response"
	"project/internal/query"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
	tptodb "project/third_party/grpc/tptodb_client"
	pb "project/third_party/grpc/tptodb_client/grpc_tptodb"
)

func TestTenantLatestTelemetryUnavailable(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	recorder := &wsAuthSQLRecorder{Interface: logger.New(log.New(io.Discard, "", 0), logger.Config{LogLevel: logger.Info})}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: recorder})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	oldDB, oldQ, oldDevice, oldCurrent := global.DB, *query.Q, query.Device, query.TelemetryCurrentData
	global.DB = db
	*query.Q = *query.Use(db)
	query.Device, query.TelemetryCurrentData = &query.Q.Device, &query.Q.TelemetryCurrentData
	t.Cleanup(func() {
		global.DB, *query.Q = oldDB, oldQ
		query.Device, query.TelemetryCurrentData = oldDevice, oldCurrent
	})
	oldType := viper.Get("grpc.tptodb_type")
	t.Cleanup(func() { viper.Set("grpc.tptodb_type", oldType) })
	fake, oldClient := &telemetryCountingConn{}, tptodb.TptodbClient
	tptodb.TptodbClient = pb.NewThingsPanelClient(fake)
	t.Cleanup(func() { tptodb.TptodbClient = oldClient })
	handler, err := response.NewHandler("../../configs/messages.yaml", "../../configs/messages_str.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, dbType := range []string{"", "TSDB", "KINGBASE", "POLARDB", "unknown"} {
		for _, tenant := range []string{"tenant-a", ""} {
			for _, lang := range []struct{ header, message string }{{"zh-CN", "服务暂时不可用"}, {"en-US", "Service Temporarily Unavailable"}} {
				t.Run(dbType+"/"+tenant+"/"+lang.header, func(t *testing.T) {
					viper.Set("grpc.tptodb_type", dbType)
					r := gin.New()
					r.Use(handler.Middleware())
					r.Use(func(c *gin.Context) {
						c.Set("claims", &utils.UserClaims{ID: "fixture-user", TenantID: tenant, Authority: "TENANT_ADMIN"})
						c.Next()
					})
					r.GET("/latest", (&DeviceApi{}).HandleTenantTelemetryData)
					req := httptest.NewRequest(http.MethodGet, "/latest", nil)
					req.Header.Set("Accept-Language", lang.header)
					out := httptest.NewRecorder()
					recorder.reset()
					before := len(fake.methods)
					r.ServeHTTP(out, req)
					var body map[string]interface{}
					if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
						t.Fatalf("response %s: %v", out.Body.String(), err)
					}
					_, hasData := body["data"]
					if out.Code != http.StatusOK || body["code"] != float64(errcode.CodeServiceUnavailable) || body["message"] != lang.message || hasData || len(recorder.snapshot()) != 0 || len(fake.methods) != before {
						t.Fatalf("status=%d body=%s SQL=%v RPC=%v", out.Code, out.Body.String(), recorder.snapshot(), fake.methods[before:])
					}
					result, err := service.GroupApp.Device.GetTenantTelemetryData(tenant)
					e, ok := err.(*errcode.Error)
					if result != nil || !ok || e.Code != errcode.CodeServiceUnavailable || len(recorder.snapshot()) != 0 || len(fake.methods) != before {
						t.Fatalf("direct result=%v error=%v SQL=%v RPC=%v", result, err, recorder.snapshot(), fake.methods[before:])
					}
				})
			}
		}
	}
}

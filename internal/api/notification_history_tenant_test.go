package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/middleware/response"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestLegacyNotificationHistoryHTTPUsesPrincipalTenantAndHidesUnscopedDemo(t *testing.T) {
	db := openNotificationGroupSourceFixture(t)
	if err := db.Exec(`CREATE TABLE notification_histories (
		id text PRIMARY KEY, send_time timestamptz NOT NULL, send_content text, send_target text NOT NULL,
		send_result text, notification_type text NOT NULL, tenant_id text NOT NULL, remark text)`).Error; err != nil {
		t.Fatal("create isolated notification history table")
	}
	for _, row := range []struct{ id, tenant, result string }{
		{"history-tenant-a", "tenant-a", "SUCCESS"},
		{"history-tenant-b", "tenant-b", "SUCCESS"},
		{"history-unscoped-demo", "", "SUCCESS"},
	} {
		if err := db.Exec(`INSERT INTO notification_histories (id, send_time, send_content, send_target, send_result, notification_type, tenant_id)
			VALUES (?, now(), ?, ?, ?, 'EMAIL', ?)`, row.id, "fixture history body", "ops@example.test", row.result, row.tenant).Error; err != nil {
			t.Fatal("seed isolated legacy history row")
		}
	}

	gin.SetMode(gin.TestMode)
	manager := errcode.NewErrorManager("", "")
	engine := gin.New()
	engine.Use((&response.Handler{ErrManager: manager}).Middleware())
	engine.GET("/api/v1/notification_history/list", func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: "fixture-actor", TenantID: "tenant-a", Authority: "TENANT_ADMIN"})
		(&NotificationHistoryApi{}).HandleNotificationHistoryListByPage(c)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/notification_history/list?page=1&page_size=20&tenant_id=tenant-b", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("legacy history endpoint returned HTTP %d", recorder.Code)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Total int `json:"total"`
			List  []struct {
				ID         string `json:"id"`
				TenantID   string `json:"tenant_id"`
				SendResult string `json:"send_result"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal("decode legacy history response")
	}
	if envelope.Code != errcode.CodeSuccess || envelope.Data.Total != 1 || len(envelope.Data.List) != 1 {
		t.Fatalf("legacy history tenant filter leaked rows: code=%d total=%d count=%d", envelope.Code, envelope.Data.Total, len(envelope.Data.List))
	}
	row := envelope.Data.List[0]
	if row.ID != "history-tenant-a" || row.TenantID != "tenant-a" || row.SendResult != "SUCCESS" {
		t.Fatalf("legacy history identity/status changed or crossed tenant: id=%s tenant=%s status=%s", row.ID, row.TenantID, row.SendResult)
	}
	if strings.Contains(recorder.Body.String(), "history-tenant-b") || strings.Contains(recorder.Body.String(), "history-unscoped-demo") {
		t.Fatal("legacy history response exposed another tenant or unscoped demo row")
	}
}

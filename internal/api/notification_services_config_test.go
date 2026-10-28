package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/dal"
	"project/internal/middleware/response"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestSendTestEmailRequiresSysAdminBeforeBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &NotificationServicesConfigApi{}
	request := func(claims any, body string, setClaims bool) *httptest.ResponseRecorder {
		t.Helper()
		engine := gin.New()
		engine.Use((&response.Handler{ErrManager: errcode.NewErrorManager("", "")}).Middleware())
		engine.POST("/api/v1/notification/services/config/e-mail/test", func(c *gin.Context) {
			if setClaims {
				c.Set("claims", claims)
			}
			handler.SendTestEmail(c)
		})
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/notification/services/config/e-mail/test", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	for _, test := range []struct {
		name   string
		claims any
		body   string
		set    bool
	}{
		{
			name:   "tenant malformed attacker-shaped body",
			claims: &utils.UserClaims{Authority: "TENANT_ADMIN"},
			body:   `{"email":{"$ne":""},"body":"attempt"}`,
			set:    true,
		},
		{
			name:   "tenant empty body",
			claims: &utils.UserClaims{Authority: "TENANT_ADMIN"},
			body:   "",
			set:    true,
		},
		{
			name:   "wrong claims type",
			claims: "not-user-claims",
			body:   "",
			set:    true,
		},
		{name: "missing claims", body: "", set: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := request(test.claims, test.body, test.set)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("non-SYSADMIN request was not rejected before binding: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}

	admin := request(&utils.UserClaims{Authority: dal.SYS_ADMIN}, `{"email":`, true)
	if admin.Code != http.StatusOK {
		t.Fatalf("SYSADMIN malformed body did not reach normal validation: status=%d body=%s", admin.Code, admin.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(admin.Body.Bytes(), &envelope); err != nil {
		t.Fatal("decode SYSADMIN validation response")
	}
	if envelope.Code != errcode.CodeParamError {
		t.Fatalf("SYSADMIN malformed body was not rejected by request binding: code=%d body=%s", envelope.Code, admin.Body.String())
	}
}

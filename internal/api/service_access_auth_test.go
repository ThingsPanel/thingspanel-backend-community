package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"project/internal/middleware/response"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestServiceAccessHTTPRejectsMissingOrInvalidClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/service/access", `{"name":"fixture","service_plugin_id":"plugin","voucher":"fixture","operation_key":"original-key"}`},
		{http.MethodGet, "/api/v1/service/access/operations/original-key", ""},
		{http.MethodPut, "/api/v1/service/access", `{"id":"access-1"}`},
		{http.MethodDelete, "/api/v1/service/access/access-1", ""},
		{http.MethodGet, "/api/v1/service/access/list?page=1&page_size=10", ""},
	} {
		for _, wrongType := range []bool{false, true} {
			t.Run(tc.method+"/wrongType="+strconv.FormatBool(wrongType), func(t *testing.T) {
				engine := gin.New()
				engine.Use((&response.Handler{ErrManager: errcode.NewErrorManager("", "")}).Middleware())
				engine.Use(func(c *gin.Context) {
					if wrongType {
						c.Set("claims", "invalid")
					}
					c.Next()
				})
				api := &ServiceAccessApi{}
				engine.POST("/api/v1/service/access", api.Create)
				engine.GET("/api/v1/service/access/operations/:key", api.HandleCreateOperation)
				engine.PUT("/api/v1/service/access", api.Update)
				engine.DELETE("/api/v1/service/access/:id", api.Delete)
				engine.GET("/api/v1/service/access/list", api.HandleList)
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				engine.ServeHTTP(res, req)
				var envelope struct {
					Code int `json:"code"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if envelope.Code != errcode.CodeNoPermission {
					t.Fatalf("status=%d code=%d body=%s", res.Code, envelope.Code, res.Body.String())
				}
			})
		}
	}
}

func TestServiceAccessHTTPRejectsConflictingOperationKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use((&response.Handler{ErrManager: errcode.NewErrorManager("", "")}).Middleware())
	engine.Use(func(c *gin.Context) { c.Set("claims", &utils.UserClaims{ID: "subject", TenantID: "tenant"}); c.Next() })
	engine.POST("/api/v1/service/access", (&ServiceAccessApi{}).Create)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/service/access", strings.NewReader(`{"name":"fixture","service_plugin_id":"plugin","voucher":"fixture","operation_key":"body-key"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "header-key")
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != errcode.CodeParamError {
		t.Fatalf("conflicting keys accepted: status=%d code=%d", res.Code, envelope.Code)
	}
}

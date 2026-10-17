package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestFactoryGrantResponseIsNotLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logged bytes.Buffer
	previous := logrus.StandardLogger().Out
	logrus.SetOutput(&logged)
	defer logrus.SetOutput(previous)
	router := gin.New()
	router.Use(OperationLogs())
	router.POST("/api/v1/product/factory-batches/batch-01/stations", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"token": "factory-secret-test-token"})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/v1/product/factory-batches/batch-01/stations",
		strings.NewReader(`{"stationId":"station-01"}`)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "factory-secret-test-token") {
		t.Fatalf("工位授权响应异常: %d", response.Code)
	}
	if strings.Contains(logged.String(), "factory-secret-test-token") || strings.Contains(logged.String(), "station-01") {
		t.Fatal("出厂预制令牌或请求正文进入操作日志")
	}
}

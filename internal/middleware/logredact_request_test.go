package middleware

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/logredact"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOperationLogRedactsEncodedFormVoucherAndKeepsQueryOutOfPath(t *testing.T) {
	previousMode := gin.Mode()
	previousWriter := gin.DefaultWriter
	previousLogrusWriter := logrus.StandardLogger().Out
	previousQuery := *query.Q
	t.Cleanup(func() {
		gin.SetMode(previousMode)
		gin.DefaultWriter = previousWriter
		logrus.SetOutput(previousLogrusWriter)
		*query.Q = previousQuery
	})
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatal(err)
	}
	query.SetDefault(db)

	var output bytes.Buffer
	gin.DefaultWriter = &output
	logrus.SetOutput(&output)
	const querySecret = "fake-query-credential-314159"
	const formSecret = "fake-form-credential-271828"
	const quotedSecret = "fake-quoted-credential-161803"
	const route = "/api/v1/service/access"
	engine := gin.New()
	engine.Use(AccessLogger(), OperationLogs())
	engine.POST(route, func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: "user-test", TenantID: "tenant-test"})
		c.Status(204)
	})
	body := "voucher%5Btoken%5D=" + formSecret + "&voucher=\"" + quotedSecret + "\""
	request := httptest.NewRequest("POST", route+"?v%6fucher="+querySecret, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("unexpected response status: %d", response.Code)
	}

	var operationLog model.OperationLog
	if err := db.First(&operationLog).Error; err != nil {
		t.Fatal(err)
	}
	if operationLog.Path == nil || *operationLog.Path != route {
		t.Fatalf("operation log path should exclude query credentials: %#v", operationLog.Path)
	}
	if operationLog.RequestMessage == nil || strings.Contains(*operationLog.RequestMessage, formSecret) || strings.Contains(*operationLog.RequestMessage, quotedSecret) || !strings.Contains(*operationLog.RequestMessage, logredact.Mask) {
		t.Fatalf("encoded form credential was not redacted in operation log: %#v", operationLog.RequestMessage)
	}
	for _, secret := range []string{querySecret, formSecret, quotedSecret} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("credential leaked into logs: %s", output.String())
		}
	}
}

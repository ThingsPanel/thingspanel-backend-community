package middleware

import (
	"bytes"
	"io"
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
	const emptyBracketSecret = "fake-empty-bracket-credential-141421"
	const codeBracketSecret = "fake-code-bracket-credential-173205"
	const userBracketSecret = "fake-user-bracket-credential-223607"
	const nestedBracketSecret = "fake-nested-bracket-credential-244949"
	const nestedPayloadSecret = "fake-nested-payload-credential-264575"
	const nestedURLSecret = "fake-return-to-token-282843"
	const escapedJSONSecret = "fake-escaped-json-token-316227"
	const route = "/api/v1/service/access"
	var handlerBody string
	engine := gin.New()
	engine.Use(AccessLogger(), OperationLogs())
	engine.POST(route, func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		handlerBody = string(body)
		c.Set("claims", &utils.UserClaims{ID: "user-test", TenantID: "tenant-test"})
		c.Status(204)
	})
	body := "voucher%5Btoken%5D=" + formSecret + "&voucher=\"" + quotedSecret + "\"" +
		"&voucher[]=" + emptyBracketSecret + "&voucher[code]=" + codeBracketSecret +
		"&user[voucher]=" + userBracketSecret + "&plugin[voucher][id]=" + nestedBracketSecret +
		"&payload=%7B%22voucher%22%3A%22" + nestedPayloadSecret + "%22%7D" +
		"&inner=%7B%22blob%22%3A%22%7B%5C%22token%5C%22%3A%5C%22" + escapedJSONSecret + "%5C%22%7D%22%7D" +
		"&profile=%7B%22name%22%3A%22alice%22%7D&filter[code]=visible-filter&user[name]=alice"
	request := httptest.NewRequest("POST", route+"?v%6fucher="+querySecret+
		"&payload=%7B%22voucher%22%3A%22"+nestedPayloadSecret+"%22%7D"+
		"&return_to=https%3A%2F%2Finternal.test%2F%3Ftoken%3D"+nestedURLSecret+"&normal=a+b", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("unexpected response status: %d", response.Code)
	}
	if handlerBody != body {
		t.Fatalf("logging middleware altered the request received by the handler: got %q want %q", handlerBody, body)
	}

	var operationLog model.OperationLog
	if err := db.First(&operationLog).Error; err != nil {
		t.Fatal(err)
	}
	if operationLog.Path == nil || *operationLog.Path != route {
		t.Fatalf("operation log path should exclude query credentials: %#v", operationLog.Path)
	}
	if operationLog.RequestMessage == nil || !strings.Contains(*operationLog.RequestMessage, logredact.Mask) || !strings.Contains(*operationLog.RequestMessage, "visible-filter") || !strings.Contains(*operationLog.RequestMessage, "alice") {
		t.Fatalf("encoded form credential was not redacted in operation log: %#v", operationLog.RequestMessage)
	}
	for _, secret := range []string{querySecret, formSecret, quotedSecret, emptyBracketSecret, codeBracketSecret, userBracketSecret, nestedBracketSecret, nestedPayloadSecret, nestedURLSecret, escapedJSONSecret} {
		if strings.Contains(*operationLog.RequestMessage, secret) {
			t.Fatalf("credential leaked into operation log: %s", *operationLog.RequestMessage)
		}
		if strings.Contains(output.String(), secret) {
			t.Fatalf("credential leaked into logs: %s", output.String())
		}
	}
	if !strings.Contains(output.String(), "normal=a+b") {
		t.Fatalf("access log lost ordinary plus-encoded query value: %s", output.String())
	}
}

func TestAccessLoggerRedactsEncodedPathWithoutChangingRequest(t *testing.T) {
	previousMode := gin.Mode()
	previousWriter := gin.DefaultWriter
	t.Cleanup(func() {
		gin.SetMode(previousMode)
		gin.DefaultWriter = previousWriter
	})
	gin.SetMode(gin.TestMode)
	const secret = "FAKE_ACCESS_PATH_SECRET"
	var output bytes.Buffer
	gin.DefaultWriter = &output
	type observedRequest struct{ path, escapedPath, rawQuery string }
	var observed []observedRequest
	engine := gin.New()
	engine.Use(AccessLogger())
	engine.GET("/api/*path", func(c *gin.Context) {
		observed = append(observed, observedRequest{path: c.Request.URL.Path, escapedPath: c.Request.URL.EscapedPath(), rawQuery: c.Request.URL.RawQuery})
		c.Status(204)
	})
	cases := []struct {
		target, path, escapedPath, rawQuery string
	}{
		{"/api/%3Ftoken%3D" + secret, "/api/?token=" + secret, "/api/%3Ftoken%3D" + secret, ""},
		{"/api/token=" + secret + "?foo=ok", "/api/token=" + secret, "/api/token=" + secret, "foo=ok"},
	}
	for _, tc := range cases {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest("GET", tc.target, nil))
		if response.Code != 204 {
			t.Fatalf("unexpected response status: %d", response.Code)
		}
	}
	if len(observed) != len(cases) {
		t.Fatalf("handler saw %d requests, want %d", len(observed), len(cases))
	}
	for i, tc := range cases {
		if observed[i].path != tc.path || observed[i].escapedPath != tc.escapedPath || observed[i].rawQuery != tc.rawQuery {
			t.Fatalf("access logger changed routed request %d: got %#v want %#v", i, observed[i], tc)
		}
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("encoded path credential leaked into access log: %s", output.String())
	}
	if !strings.Contains(output.String(), "foo=ok") || !strings.Contains(output.String(), "%2A%2A%2A") {
		t.Fatalf("access log lost ordinary query or redaction marker: %s", output.String())
	}
}

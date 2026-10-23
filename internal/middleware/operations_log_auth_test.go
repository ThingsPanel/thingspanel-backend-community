package middleware

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountPayloadNeverEntersOperationLogs(t *testing.T) {
	var output bytes.Buffer
	previous := logrus.StandardLogger().Out
	logrus.SetOutput(&output)
	defer logrus.SetOutput(previous)
	for _, path := range []string{"/api/v1/login", "/api/v1/reset/password", "/api/v1/user/transform", "/api/v1/auth/wechat-mini/bind"} {
		engine := gin.New()
		engine.Use(OperationLogs())
		engine.POST(path, func(c *gin.Context) {
			b, _ := io.ReadAll(c.Request.Body)
			if !bytes.Contains(b, []byte("sensitive-password")) {
				t.Error("handler lost request")
			}
			c.JSON(200, gin.H{"token": "sensitive-token"})
		})
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"password":"sensitive-password"}`))
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "sensitive-token") {
			t.Fatal("handler response altered")
		}
	}
	if strings.Contains(output.String(), "sensitive-password") || strings.Contains(output.String(), "sensitive-token") {
		t.Fatal("account secret logged")
	}
}

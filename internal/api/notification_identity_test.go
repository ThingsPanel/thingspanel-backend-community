package api

import (
	"net/http/httptest"
	"testing"

	"project/pkg/global"

	"github.com/gin-gonic/gin"
)

func TestNotificationSessionContextFailsClosedWithoutTokenOrRedis(t *testing.T) {
	previous := global.REDIS
	global.REDIS = nil
	t.Cleanup(func() { global.REDIS = previous })
	gin.SetMode(gin.TestMode)
	for _, token := range []string{"", "fixture-api-key"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/api/v1/notification/session-context", nil)
		if token != "" {
			ctx.Request.Header.Set("x-token", token)
		}
		(&NotificationIdentityApi{}).SessionContext(ctx)
		if recorder.Code != 401 {
			t.Fatalf("token presence %t status=%d", token != "", recorder.Code)
		}
	}
}

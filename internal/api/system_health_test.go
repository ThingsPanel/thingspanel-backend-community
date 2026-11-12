package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"project/pkg/global"

	"github.com/gin-gonic/gin"
)

// With no database and no Redis configured the endpoint must answer 503 and
// name both components; it must not report ready.
func TestHealthCheckNotReadyWithoutBackends(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedDB, savedRedis := global.DB, global.REDIS
	global.DB, global.REDIS = nil, nil
	defer func() { global.DB, global.REDIS = savedDB, savedRedis }()

	r := gin.New()
	r.GET("/health", (&SystemApi{}).HealthCheck)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var body struct {
		Data struct {
			Ready  bool `json:"ready"`
			Checks []struct {
				Name string `json:"name"`
				OK   bool   `json:"ok"`
			} `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Ready || len(body.Data.Checks) != 2 || body.Data.Checks[0].Name != "database" || body.Data.Checks[1].Name != "redis" {
		t.Fatalf("body=%s", w.Body)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/internal/middleware/response"
	"project/pkg/utils"
)

func TestNativeManualSceneUnavailableSubprocess(t *testing.T) {
	if os.Getenv("TP_TEST_MANUAL_GATE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestNativeManualSceneUnavailableSubprocess$")
		cmd.Env = append(os.Environ(), "TP_TEST_MANUAL_GATE=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v: %s", err, out)
		}
		return
	}
	t.Setenv("GOTP_AUTOMATION_MODE", "native")
	t.Setenv("GOTP_AUTOMATION_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("s", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "fixture")
	gin.SetMode(gin.TestMode)
	h, err := response.NewHandler("../../configs/messages.yaml", "../../configs/messages_str.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(h.Middleware())
	r.Use(func(c *gin.Context) { c.Set("claims", &utils.UserClaims{ID: "user", TenantID: "tenant"}); c.Next() })
	r.POST("/scene/:id", (&SceneApi{}).ActiveScene)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/scene/fixture", nil))
	var body map[string]interface{}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if out.Code != http.StatusOK || body["code"] != float64(100001) || body["message"] != "服务暂时不可用" {
		t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
	}
	if _, present := body["data"]; present {
		t.Fatal("unavailable response contained data")
	}
}

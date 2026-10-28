package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/internal/brokercommands"
	"project/internal/brokersemantic"
)

func TestCommandPlanBodyIsBoundedUniqueAndStrict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "valid", body: `{"device_id":"d","identify":"power","value":"on"}`, want: true},
		{name: "duplicate-key", body: `{"device_id":"d","device_id":"x","identify":"power"}`},
		{name: "unknown-field", body: `{"device_id":"d","identify":"power","unexpected":true}`},
		{name: "trailing-value", body: `{"device_id":"d","identify":"power"} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/", func(c *gin.Context) {
				var req struct {
					DeviceID string  `json:"device_id"`
					Identify string  `json:"identify"`
					Value    *string `json:"value"`
				}
				if decodeCommandPlanBody(c, &req) {
					c.Status(204)
				}
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)))
			if tc.want && w.Code != 204 {
				t.Fatalf("valid body status=%d want 204", w.Code)
			}
			if !tc.want && w.Code != 400 {
				t.Fatalf("invalid body status=%d want 400", w.Code)
			}
		})
	}
	t.Run("too-large", func(t *testing.T) {
		r := gin.New()
		r.POST("/", func(c *gin.Context) {
			var req map[string]any
			decodeCommandPlanBody(c, &req)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"x":"`+strings.Repeat("a", maxCommandPlanBody)+`"}`)))
		if w.Code != 400 {
			t.Fatalf("oversize status=%d want 400", w.Code)
		}
	})
}

func TestCommandPlanHumanSessionAndSafeErrorCategories(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/plans/preview", (CommandPlanApi{}).Preview)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/plans/preview", strings.NewReader(`{}`)))
	if w.Code != 401 || strings.Contains(w.Body.String(), "SQL") {
		t.Fatalf("missing verified user session status=%d body=%s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{brokercommands.ErrConflict, 409},
		{brokercommands.ErrFenced, 409},
		{brokersemantic.ErrInvalid, 400},
		{brokersemantic.ErrSemanticUnavailable, 409},
		{brokersemantic.ErrCommandInvalid, 400},
		{brokersemantic.ErrCommandUnsupported, 409},
	} {
		status, _ := commandPlanStatus(tc.err)
		if status != tc.want {
			t.Fatalf("error %v mapped status=%d want %d", tc.err, status, tc.want)
		}
	}
}

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDashboardExportKeepsPlatformAndThingsVisCredentialsSeparate(t *testing.T) {
	for _, test := range []struct {
		name, platform, authorization, thingsvis, want string
	}{
		{"new browser contract", "platform-token", "Bearer platform-token", "Bearer thingsvis-token", "Bearer thingsvis-token"},
		{"legacy explicit pair", "platform-token", "Bearer thingsvis-token", "", "Bearer thingsvis-token"},
		{"platform mirror is not ThingsVis", "platform-token", "Bearer platform-token", "", ""},
		{"no implicit internal fallback", "", "", "", ""},
		{"single bearer lacks independent platform transport", "", "Bearer thingsvis-token", "", ""},
		{"invalid explicit credential cannot fall back", "platform-token", "Bearer thingsvis-token", "Basic invalid", ""},
		{"empty explicit bearer", "platform-token", "Bearer platform-token", "Bearer ", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/device/market/dashboard-bundles/analyze", nil)
			c.Request.Header.Set("x-token", test.platform)
			c.Request.Header.Set("Authorization", test.authorization)
			c.Request.Header.Set("X-ThingsVis-Authorization", test.thingsvis)
			actual, err := dashboardExportAuthorization(c)
			if test.want == "" {
				if err == nil || actual != "" {
					t.Fatalf("implicit/invalid credential accepted: %q %v", actual, err)
				}
			} else if err != nil || actual != test.want {
				t.Fatalf("credential=%q want=%q err=%v", actual, test.want, err)
			}
			if c.Request.Header.Get("x-token") != test.platform || c.Request.Header.Get("Authorization") != test.authorization {
				t.Fatal("platform authentication headers were mutated")
			}
		})
	}
}

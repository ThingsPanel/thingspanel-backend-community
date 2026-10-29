package automation

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerTickRejectsUserSessionsAndTenantScopeBeforeDatabase(t *testing.T) {
	token := strings.Repeat("o", 32)
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment-a")
	for _, tc := range []struct {
		token, dep, tenant string
		status             int
	}{
		{"user-jwt", "deployment-a", "", 401}, {token, "deployment-b", "", 401}, {token, "deployment-a", "tenant-a", 400}, {token, "deployment-a", "", 503},
	} {
		r := httptest.NewRequest("POST", "/internal/automation/tick", nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("X-Automation-Deployment", tc.dep)
		r.Header.Set("X-Automation-Tenant", tc.tenant)
		w := httptest.NewRecorder()
		var service *Service
		service.OwnerTick(w, r)
		if w.Code != tc.status {
			t.Fatalf("unexpected worker authorization status %d, want %d", w.Code, tc.status)
		}
	}
}

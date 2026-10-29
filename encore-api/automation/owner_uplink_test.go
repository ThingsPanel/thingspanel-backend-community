package automation

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerUplinkRejectsBrowserAndMismatchedDeployment(t *testing.T) {
	token := strings.Repeat("o", 32)
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "deployment-a")
	for _, tc := range []struct {
		token, dep, tenant string
		valid              bool
	}{{token, "deployment-a", "tenant-a", true}, {"user-jwt", "deployment-a", "tenant-a", false}, {token, "deployment-b", "tenant-a", false}, {token, "deployment-a", "", false}} {
		r := httptest.NewRequest("POST", "/internal/automation/uplink", nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("X-Automation-Deployment", tc.dep)
		r.Header.Set("X-Automation-Tenant", tc.tenant)
		tenant, ok := authorizeOwnerUplink(r)
		if ok != tc.valid || (ok && tenant != tc.tenant) {
			t.Fatalf("unexpected owner authorization result")
		}
	}
}

package automationbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceIdentityRequiresCredentialDeploymentAndTenant(t *testing.T) {
	token := strings.Repeat("t", 32)
	t.Setenv("GOTP_AUTOMATION_TOKEN", token)
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment-a")
	for _, tc := range []struct {
		token, dep, tenant string
		valid              bool
	}{{token, "deployment-a", "tenant-a", true}, {"browser-jwt", "deployment-a", "tenant-a", false}, {token, "deployment-b", "tenant-a", false}, {token, "deployment-a", "", false}} {
		r := httptest.NewRequest("POST", "/internal/automation/devices", nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set(DeploymentHeader, tc.dep)
		r.Header.Set(TenantHeader, tc.tenant)
		tenant, err := Authenticate(r)
		if (err == nil) != tc.valid || (tc.valid && tenant != tc.tenant) {
			t.Fatalf("identity result tenant=%q err=%v", tenant, err)
		}
	}
}

func TestDeliverCarriesTrustedTenantOutsideTheBody(t *testing.T) {
	token := strings.Repeat("s", 32)
	t.Setenv("GOTP_AUTOMATION_TOKEN", token)
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment-a")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, err := Authenticate(r)
		if err != nil || tenant != "tenant-a" || r.URL.Path != "/internal/automation/uplink" {
			t.Errorf("invalid owner request")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["tenant_id"]; ok {
			t.Error("tenant must come from authenticated owner context")
		}
		if body["message_id"] != "effect-1" {
			t.Error("stable effect ID lost")
		}
		_, _ = w.Write([]byte(`{"class":"telemetry","accepted":1,"duplicates":0}`))
	}))
	defer server.Close()
	t.Setenv("GOTP_AUTOMATION_BASE_URL", server.URL)
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Deliver(context.Background(), Uplink{TenantID: "tenant-a", DeviceID: "device-a", MessageID: "effect-1", Class: "telemetry", Values: `{"x":1}`}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliverDoesNotTreatHTML200AsAccepted(t *testing.T) {
	t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("s", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment-a")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>frontend</html>")) }))
	defer server.Close()
	t.Setenv("GOTP_AUTOMATION_BASE_URL", server.URL)
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Deliver(context.Background(), Uplink{TenantID: "tenant-a", Class: "telemetry"}); err == nil {
		t.Fatal("unrelated HTML 200 was treated as an ingress receipt")
	}
}

func TestNativeConfigurationDoesNotSilentlySelectLegacy(t *testing.T) {
	t.Setenv("GOTP_AUTOMATION_BASE_URL", "")
	t.Setenv("GOTP_AUTOMATION_TOKEN", "")
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "")
	if _, err := NewClient(); err == nil {
		t.Fatal("missing native service configuration accepted")
	}
}

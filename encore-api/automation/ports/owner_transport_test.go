package ports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandUsesServiceChannelAndCarriesTenant(t *testing.T) {
	token := strings.Repeat("t", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/automation/api/v1/telemetry/datas/pub" || r.Header.Get("X-Automation-Tenant") != "tenant-a" || r.Header.Get("X-Automation-Deployment") != "deployment-a" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("x-token") != "" {
			t.Error("command did not use independent owner authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"command_id":"command-a"}}`))
	}))
	defer server.Close()
	owner := NewHTTPCommandOwner(Config{CommunityBaseURL: server.URL, CommunityToken: token, DeploymentID: "deployment-a"})
	result, err := owner.Dispatch(context.Background(), CommandRequest{TenantID: "tenant-a", DeviceID: "device-a", Kind: CommandTelemetry, Value: `{"switch":1}`, IdempotencyKey: "action-a", ScopeKey: "scope-a", Generation: 1})
	if err != nil || result.State != CommandAccepted || result.Ref != "command-a" {
		t.Fatalf("command result: %+v %v", result, err)
	}
}

func TestDirectoryRejectsCrossTenantResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/automation/devices" || r.Header.Get("X-Automation-Tenant") != "tenant-a" {
			t.Error("directory owner context lost")
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"list":[{"id":"device-b","tenant_id":"tenant-b"}],"total":1}}`))
	}))
	defer server.Close()
	directory := NewHTTPDirectory(Config{CommunityBaseURL: server.URL})
	if devices, err := directory.ByIDs(context.Background(), "tenant-a", []string{"device-b"}); err == nil || len(devices) != 0 {
		t.Fatal("cross tenant directory response accepted")
	}
}

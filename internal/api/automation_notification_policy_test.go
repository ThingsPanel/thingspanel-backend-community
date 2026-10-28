package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/internal/service"
)

func TestAutomationPolicySelection(t *testing.T) {
	ready := service.DefaultPolicySummary{NativeGroupID: "native-a", AliasGroupID: "alias-a", GroupRevision: 3, Ready: true}
	view := service.TenantDefaultPolicyView{Selected: &ready, AvailablePolicies: []service.DefaultPolicySummary{ready}}
	if p, ok := selectAutomationPolicy(view, ""); !ok || p.NativeGroupID != "native-a" {
		t.Fatal("default should resolve")
	}
	if p, ok := selectAutomationPolicy(view, "alias-a"); !ok || p.GroupRevision != 3 {
		t.Fatal("explicit should resolve")
	}
	if _, ok := selectAutomationPolicy(view, "unavailable"); ok {
		t.Fatal("explicit must not fall back to default")
	}
	ready.Ready = false
	if _, ok := selectAutomationPolicy(view, ""); ok {
		t.Fatal("unavailable default must be blocked")
	}
	if p, ok := selectAutomationPolicy(service.TenantDefaultPolicyView{}, ""); !ok || p != nil {
		t.Fatal("no default means record only")
	}
}

func TestAutomationPolicyReaderBoundaries(t *testing.T) {
	token := strings.Repeat("x", 40)
	r := NewNotificationMemberReader(NotificationMemberReaderConfig{Token: token, AllowedDeploymentIDs: []string{"dep-a"}, AllowedTenantIDs: []string{"tenant-a"}})
	calls := 0
	r.policyView = func(_ context.Context, deployment, tenant string) (service.TenantDefaultPolicyView, error) {
		calls++
		if deployment != "dep-a" || tenant != "tenant-a" {
			t.Fatal("identity changed")
		}
		return service.TenantDefaultPolicyView{}, nil
	}
	engine := gin.New()
	engine.GET("/policies/:id", r.GetAutomationPolicy)
	for _, tc := range []struct {
		path, token, deployment string
		status                  int
	}{
		{"/policies/tenant-a", "wrong", "dep-a", 401},
		{"/policies/tenant-a", token, "dep-b", 403},
		{"/policies/tenant-b", token, "dep-a", 403},
		{"/policies/tenant-a?alias=a&alias=b", token, "dep-a", 400},
		{"/policies/tenant-a?extra=x", token, "dep-a", 400},
		{"/policies/tenant-a", token, "dep-a", 200},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-Notification-Deployment", tc.deployment)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d want %d", tc.path, w.Code, tc.status)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("policy responses must not be cached")
		}
	}
	if calls != 1 {
		t.Fatalf("unauthorized requests reached policy owner: %d", calls)
	}
}

package api

import (
	"net/http/httptest"
	"testing"

	"project/internal/middleware"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestPluginIdentityAccessScope(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		request  string
		want     bool
	}{
		{name: "tenant identity own tenant", tenantID: "tenant-a", request: "tenant-a", want: true},
		{name: "tenant identity cannot cross tenant", tenantID: "tenant-a", request: "tenant-b", want: false},
		{name: "tenant identity rejects empty tenant", tenantID: "tenant-a", request: "", want: false},
		{name: "platform identity can serve tenant a", tenantID: "", request: "tenant-a", want: true},
		{name: "platform identity can serve tenant b", tenantID: "", request: "tenant-b", want: true},
		{name: "platform identity rejects missing tenant", tenantID: "", request: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := &middleware.PluginRuntimeIdentity{TenantID: tt.tenantID}
			if got := pluginIdentityCanAccessTenant(identity, tt.request); got != tt.want {
				t.Fatalf("scope %q to %q = %v, want %v", tt.tenantID, tt.request, got, tt.want)
			}
		})
	}
}

func TestManagedIdentityIssueScopeByRole(t *testing.T) {
	tests := []struct {
		name      string
		claims    utils.UserClaims
		wantScope string
		wantOK    bool
	}{
		{name: "platform administrator gets platform scope", claims: utils.UserClaims{Authority: "SYS_ADMIN"}, wantScope: "", wantOK: true},
		{name: "tenant administrator remains tenant scoped", claims: utils.UserClaims{Authority: "TENANT_ADMIN", TenantID: "tenant-a"}, wantScope: "tenant-a", wantOK: true},
		{name: "tenant user is refused", claims: utils.UserClaims{Authority: "TENANT_USER", TenantID: "tenant-a"}},
		{name: "delegated platform claim is refused", claims: utils.UserClaims{Authority: "SYS_ADMIN", Delegated: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("claims", &tt.claims)
			scope, ok := managedIdentityScope(c)
			if scope != tt.wantScope || ok != tt.wantOK {
				t.Fatalf("scope=%q ok=%v, want scope=%q ok=%v", scope, ok, tt.wantScope, tt.wantOK)
			}
			if tt.wantOK && w.Code != 200 {
				t.Fatalf("successful claim was rejected with status %d", w.Code)
			}
			if !tt.wantOK && w.Code != 403 {
				t.Fatalf("unauthorized claim got status %d, want 403", w.Code)
			}
		})
	}
}

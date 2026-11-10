package middleware

import (
	"net/http"
	"strings"
	"time"

	"project/internal/pluginidentity"
	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The strict managed guard lets a request through only
// when it carries "Authorization: Bearer <instance_id>.<secret>" for a live identity (not revoked, not expired, the
// highest generation of its plugin instance). Everything else - no header, a user JWT, an unknown or revoked token,
// caller-supplied active/instance/tenant headers - gets the same 503 PLUGIN_RUNTIME_IDENTITY_REQUIRED the closed
// entrypoints returned. Shared Community routes use the explicit legacy compatibility wrapper below.
const PluginRuntimeIdentityKey = "plugin_runtime_identity"

// PluginRuntimeIdentity is the authenticated identity stored on the request context.
type PluginRuntimeIdentity = pluginidentity.Identity

func denyPluginRuntime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"code":    http.StatusServiceUnavailable,
		"message": "managed plugin instance authorization is required",
		"data":    gin.H{"state": "blocked", "errorCode": "PLUGIN_RUNTIME_IDENTITY_REQUIRED", "automatic_retry": false},
	})
}

// RequireManagedPluginRuntime guards the five plugin runtime entrypoints using the global database.
func RequireManagedPluginRuntime() gin.HandlerFunc {
	return RequireManagedPluginRuntimeWith(pluginidentity.NewStore(func() *gorm.DB { return global.DB }), time.Now)
}

// RequireManagedPluginRuntimeWith is RequireManagedPluginRuntime with an explicit store and clock (tests).
func RequireManagedPluginRuntimeWith(store *pluginidentity.Store, now func() time.Time) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok {
			denyPluginRuntime(c)
			return
		}
		identity, err := store.Authenticate(token, now())
		if err != nil || identity == nil {
			denyPluginRuntime(c)
			return
		}
		c.Set(PluginRuntimeIdentityKey, identity)
		c.Next()
	}
}

// LegacyPluginRuntimeKey is set only by the compatibility router, never from request headers.
const LegacyPluginRuntimeKey = "legacy_plugin_runtime"

// ManagedPluginRuntimeOrLegacy preserves headerless legacy requests. Handlers
// must resolve the target service and reject Home Assistant without an identity.
// An invalid or revoked credential never falls back to legacy access.
func ManagedPluginRuntimeOrLegacy() gin.HandlerFunc {
	strict := RequireManagedPluginRuntime()
	return func(c *gin.Context) {
		if _, present := c.Request.Header["Authorization"]; present {
			strict(c)
			return
		}
		c.Set(LegacyPluginRuntimeKey, true)
		c.Next()
	}
}

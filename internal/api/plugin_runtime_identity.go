package api

import (
	"errors"
	"net/http"
	"time"

	"project/internal/dal"
	"project/internal/middleware"
	"project/internal/model"
	"project/internal/pluginidentity"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type issuePluginRuntimeIdentityReq struct {
	PluginInstance    string `json:"plugin_instance" binding:"required,max=128"`
	ServiceIdentifier string `json:"service_identifier" binding:"required,max=100"`
	Generation        int64  `json:"generation" binding:"required,min=1"`
	TTLHours          int    `json:"ttl_hours" binding:"required,min=1,max=24"`
}

func identityStore() *pluginidentity.Store {
	return pluginidentity.NewStore(func() *gorm.DB { return global.DB })
}

// managedIdentityScope keeps existing tenant-issued identities tenant-bound. A platform administrator may issue
// an empty-tenant identity for a platform-installed service plugin; the runtime guard interprets that identity as
// the explicitly authorized shared instance.
func managedIdentityScope(c *gin.Context) (string, bool) {
	value, _ := c.Get("claims")
	claims, ok := value.(*utils.UserClaims)
	if !ok || claims.Delegated {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "administrator required"})
		return "", false
	}
	switch claims.Authority {
	case "SYS_ADMIN":
		return "", true
	case "TENANT_ADMIN":
		if claims.TenantID != "" {
			return claims.TenantID, true
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "administrator required"})
	return "", false
}

// IssuePluginRuntimeIdentity: tenant administrators issue tenant-scoped identities; platform administrators issue
// platform-scoped identities for a shared service instance. The token is returned once and never stored. A higher
// generation revokes every lower one of the same plugin instance within that scope.
func IssuePluginRuntimeIdentity(c *gin.Context) {
	tenant, ok := managedIdentityScope(c)
	if !ok {
		return
	}
	var req issuePluginRuntimeIdentityReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	if tenant == "" {
		plugin, err := dal.GetServicePluginByServiceIdentifier(req.ServiceIdentifier)
		if err != nil || plugin == nil || plugin.ServiceType != 2 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "platform identities are limited to registered service plugins"})
			return
		}
	}
	token, rec, err := identityStore().Issue(tenant, req.PluginInstance, req.ServiceIdentifier, req.Generation,
		time.Duration(req.TTLHours)*time.Hour, time.Now().UTC())
	if errors.Is(err, pluginidentity.ErrGenerationNotHigher) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "generation is not higher than an existing one"})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "could not issue identity"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Set("data", gin.H{"token": token, "instance_id": rec.InstanceID, "generation": rec.Generation,
		"service_identifier": rec.ServiceIdentifier, "expires_at": rec.ExpiresAt})
}

// RevokePluginRuntimeIdentity: POST /api/v1/plugin/runtime/identity/revoke {plugin_instance}
func RevokePluginRuntimeIdentity(c *gin.Context) {
	tenant, ok := managedIdentityScope(c)
	if !ok {
		return
	}
	var req struct {
		PluginInstance string `json:"plugin_instance" binding:"required,max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	n, err := identityStore().Revoke(tenant, req.PluginInstance, time.Now().UTC())
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "could not revoke"})
		return
	}
	c.Set("data", gin.H{"revoked": n})
}

func scopeDenied(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "outside this plugin instance's scope",
		"data": gin.H{"errorCode": "PLUGIN_RUNTIME_SCOPE_DENIED"}})
}

// pluginRuntimeIdentity returns the identity the managed middleware stored. A handler reached without it (a routing
// mistake) fails closed with the same 503 as the guard.
func pluginRuntimeIdentity(c *gin.Context) (*middleware.PluginRuntimeIdentity, bool) {
	value, _ := c.Get(middleware.PluginRuntimeIdentityKey)
	identity, ok := value.(*middleware.PluginRuntimeIdentity)
	if !ok || identity == nil {
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable,
			"message": "managed plugin instance authorization is required",
			"data":    gin.H{"state": "blocked", "errorCode": "PLUGIN_RUNTIME_IDENTITY_REQUIRED", "automatic_retry": false}})
		return nil, false
	}
	return identity, true
}

// requirePluginService: the request's service_identifier must be the identity's own service.
func requirePluginService(c *gin.Context, serviceIdentifier string) (*middleware.PluginRuntimeIdentity, bool) {
	identity, ok := pluginRuntimeIdentity(c)
	if !ok {
		return nil, false
	}
	if serviceIdentifier != identity.ServiceIdentifier {
		scopeDenied(c)
		return nil, false
	}
	return identity, true
}

// pluginMayReadDevice: the device must belong to the identity's tenant and use the identity's protocol. A device
// that does not exist falls through to the normal not-found path.
func pluginMayReadDevice(c *gin.Context, req model.GetDeviceConfigReq) bool {
	identity, ok := pluginRuntimeIdentity(c)
	if !ok {
		return false
	}
	var device *model.Device
	var err error
	switch {
	case req.DeviceId != "":
		device, err = dal.GetDeviceByID(req.DeviceId)
	case req.Voucher != "":
		device, err = dal.GetDeviceByVoucher(req.Voucher)
	case req.DeviceNumber != "":
		device, err = dal.GetDeviceByDeviceNumber(req.DeviceNumber)
	}
	if err != nil || device == nil {
		return true
	}
	allowed := device.TenantID == identity.TenantID
	if allowed && device.DeviceConfigID != nil {
		config, configErr := dal.GetDeviceConfigByID(*device.DeviceConfigID)
		allowed = configErr == nil && config.ProtocolType != nil && *config.ProtocolType == identity.ServiceIdentifier
	} else {
		allowed = false
	}
	if !allowed {
		scopeDenied(c)
	}
	return allowed
}

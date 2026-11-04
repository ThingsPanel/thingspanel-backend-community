package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"project/internal/dal"
	"project/internal/middleware"
	"project/internal/model"
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

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

func tenantAdminClaims(c *gin.Context) (*utils.UserClaims, bool) {
	claims, ok := c.MustGet("claims").(*utils.UserClaims)
	if !ok || claims.Authority != "TENANT_ADMIN" || claims.TenantID == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "tenant administrator required"})
		return nil, false
	}
	return claims, true
}

// IssuePluginRuntimeIdentity: POST /api/v1/plugin/runtime/identity. The token is returned once and never stored.
// A higher generation revokes every lower one of the same plugin instance, so an older instance is fenced at once.
func IssuePluginRuntimeIdentity(c *gin.Context) {
	claims, ok := tenantAdminClaims(c)
	if !ok {
		return
	}
	var req issuePluginRuntimeIdentityReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid request"})
		return
	}
	if err := middleware.EnsurePluginRuntimeIdentityTable(); err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "identity store unavailable"})
		return
	}
	id, secret := "pri_"+randomHex(8), randomHex(24)
	conflict := false
	err := global.DB.Transaction(func(tx *gorm.DB) error {
		var highest int64
		if e := tx.Raw(`SELECT coalesce(max(generation),0) FROM plugin_runtime_identities WHERE plugin_instance = ? AND tenant_id = ? FOR UPDATE`, req.PluginInstance, claims.TenantID).Scan(&highest).Error; e != nil {
			return e
		}
		if req.Generation <= highest {
			conflict = true
			return nil
		}
		if e := tx.Exec(`UPDATE plugin_runtime_identities SET revoked_at = now() WHERE plugin_instance = ? AND tenant_id = ? AND revoked_at IS NULL`, req.PluginInstance, claims.TenantID).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO plugin_runtime_identities(instance_id, plugin_instance, service_identifier, tenant_id, generation, secret_sha256, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, id, req.PluginInstance, req.ServiceIdentifier, claims.TenantID, req.Generation, middleware.HashPluginSecret(secret), time.Now().Add(time.Duration(req.TTLHours)*time.Hour)).Error
	})
	if conflict {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": "generation is not higher than an existing one"})
		return
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "could not issue identity"})
		return
	}
	c.Set("data", gin.H{"token": id + "." + secret, "instance_id": id, "generation": req.Generation, "expires_in_hours": req.TTLHours})
}

// RevokePluginRuntimeIdentity: POST /api/v1/plugin/runtime/identity/revoke {plugin_instance}
func RevokePluginRuntimeIdentity(c *gin.Context) {
	claims, ok := tenantAdminClaims(c)
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
	if err := middleware.EnsurePluginRuntimeIdentityTable(); err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "identity store unavailable"})
		return
	}
	result := global.DB.Exec(`UPDATE plugin_runtime_identities SET revoked_at = now() WHERE plugin_instance = ? AND tenant_id = ? AND revoked_at IS NULL`, req.PluginInstance, claims.TenantID)
	if result.Error != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "could not revoke"})
		return
	}
	c.Set("data", gin.H{"revoked": result.RowsAffected})
}

// pluginMayReadDevice enforces the identity's scope on the device-config lookup: the device must belong to the
// identity's tenant and use the identity's protocol. An unknown device falls through to the normal not-found path.
func pluginMayReadDevice(c *gin.Context, req model.GetDeviceConfigReq) bool {
	value, exists := c.Get(middleware.PluginRuntimeIdentityKey)
	identity, ok := value.(*middleware.PluginRuntimeIdentity)
	if !exists || !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "managed plugin instance authorization is required"})
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
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "device is outside this plugin instance's scope"})
	}
	return allowed
}

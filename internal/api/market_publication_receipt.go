package api

import (
	"strings"

	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// QueryDashboardPublicationReceipt is a read-only query under the existing
// publication permission. A missing policy must not inherit legacy fail-open
// behavior for an unregistered receipt URL.
func (*DeviceApi) QueryDashboardPublicationReceipt(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	const publicationResource = "api/v1/device/market/dashboard-bundles"
	value, _ := c.Get("claims")
	claims, _ := value.(*utils.UserClaims)
	if claims == nil || strings.TrimSpace(claims.TenantID) == "" || strings.TrimSpace(claims.ID) == "" ||
		global.CasbinEnforcer == nil || !service.GroupApp.Casbin.GetUrl(publicationResource) ||
		!service.GroupApp.Casbin.Verify(claims.ID, publicationResource) {
		c.Error(errcode.WithData(errcode.CodeNoPermission, "publication query permission required"))
		return
	}
	keys := c.Request.URL.Query()["operationKey"]
	if len(keys) != 1 {
		c.Error(errcode.WithData(errcode.CodeParamError, "one original operationKey is required"))
		return
	}
	// The personal market token is an HTTP header, never a URL/query parameter.
	result, err := service.NewMarketDashboardBundleService("").QueryPublicationReceipt(
		c.Request.Context(), keys[0], c.GetHeader("X-Market-Token"), claims,
	)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", result)
}

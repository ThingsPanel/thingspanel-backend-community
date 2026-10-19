package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"project/internal/service"
	"project/pkg/utils"
)

// BrokerSemanticEvidence returns a tenant-scoped published semantic snapshot
// and current broker status evidence. An API-key identity is not a user
// session and cannot be delegated to the semantic source.
func (*DeviceApi) BrokerSemanticEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token := strings.TrimSpace(c.GetHeader("x-token"))
	value, ok := c.Get("claims")
	claims, claimsOK := value.(*utils.UserClaims)
	if token == "" || !ok || !claimsOK || claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not_authorized"})
		return
	}
	data, err := service.GroupApp.Device.BrokerSemanticEvidence(c.Request.Context(), claims, token, c.Param("id"))
	if err != nil {
		status, category := http.StatusServiceUnavailable, "temporarily_unavailable"
		switch {
		case errors.Is(err, service.ErrBrokerSemanticUnauthorized):
			status, category = http.StatusUnauthorized, "not_authorized"
		case errors.Is(err, service.ErrBrokerSemanticUnsupported):
			status, category = http.StatusConflict, "unsupported_capability"
		case errors.Is(err, service.ErrBrokerSemanticNotFound):
			status, category = http.StatusNotFound, "not_found"
		}
		c.AbortWithStatusJSON(status, gin.H{"error": category})
		return
	}
	c.JSON(http.StatusOK, data)
}

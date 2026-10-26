package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"project/internal/brokerstate"
	"project/internal/service"
	"project/pkg/utils"
)

// BrokerStatusEvidence is a read-only, tenant-scoped evidence endpoint. It is
// deliberately separate from the legacy binary online flag's response shape.
func (*DeviceApi) BrokerStatusEvidence(c *gin.Context) {
	value, _ := c.Get("claims")
	claims, _ := value.(*utils.UserClaims)
	c.Header("Cache-Control", "no-store")
	data, err := service.GroupApp.Device.BrokerStatusEvidence(c.Request.Context(), claims, c.Param("id"))
	if err != nil {
		status, category := http.StatusServiceUnavailable, "temporarily_unavailable"
		switch {
		case errors.Is(err, service.ErrBrokerEvidenceUnauthorized):
			status, category = http.StatusUnauthorized, "not_authorized"
		case errors.Is(err, service.ErrBrokerEvidenceUnsupported):
			status, category = http.StatusConflict, "unsupported_capability"
		case errors.Is(err, brokerstate.ErrDeviceUnavailable):
			status, category = http.StatusNotFound, "not_found"
		}
		c.AbortWithStatusJSON(status, gin.H{"error": category})
		return
	}
	c.Set("data", data)
}

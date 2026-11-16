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

func (*DeviceApi) BrokerStatusEvidenceBatch(c *gin.Context) {
	var request struct {
		DeviceIDs []string `json:"deviceIds"`
	}
	if c.ShouldBindJSON(&request) != nil || len(request.DeviceIDs) == 0 || len(request.DeviceIDs) > 200 {
		c.AbortWithStatusJSON(400, gin.H{"error": "invalid_batch"})
		return
	}
	seen := map[string]bool{}
	for _, id := range request.DeviceIDs {
		if id == "" || len(id) > 36 || seen[id] {
			c.AbortWithStatusJSON(400, gin.H{"error": "invalid_batch"})
			return
		}
		seen[id] = true
	}
	value, _ := c.Get("claims")
	claims, _ := value.(*utils.UserClaims)
	data, err := service.GroupApp.Device.BrokerStatusEvidenceBatch(c.Request.Context(), claims, request.DeviceIDs)
	c.Header("Cache-Control", "no-store")
	if err != nil {
		status := 503
		if errors.Is(err, service.ErrBrokerEvidenceUnauthorized) {
			status = 401
		}
		if errors.Is(err, brokerstate.ErrDeviceUnavailable) {
			status = 404
		}
		c.AbortWithStatusJSON(status, gin.H{"error": "batch_unavailable"})
		return
	}
	c.Set("data", data)
}

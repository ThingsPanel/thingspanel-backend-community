package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"project/internal/service"
	"project/pkg/utils"
)

func parseTelemetryEvidenceQuery(values url.Values) (service.TelemetryEvidenceRequest, error) {
	for key := range values {
		if key != "start" && key != "end" && key != "keys" && key != "limit" {
			return service.TelemetryEvidenceRequest{}, service.ErrTelemetryEvidenceInvalid
		}
	}
	if len(values["start"]) != 1 || len(values["end"]) != 1 || len(values["keys"]) != 1 || len(values["limit"]) > 1 {
		return service.TelemetryEvidenceRequest{}, service.ErrTelemetryEvidenceInvalid
	}
	start, err := time.Parse(time.RFC3339, values.Get("start"))
	if err != nil {
		return service.TelemetryEvidenceRequest{}, service.ErrTelemetryEvidenceInvalid
	}
	end, err := time.Parse(time.RFC3339, values.Get("end"))
	if err != nil {
		return service.TelemetryEvidenceRequest{}, service.ErrTelemetryEvidenceInvalid
	}
	keys := strings.Split(values.Get("keys"), ",")
	limit := 100
	if len(values["limit"]) == 1 {
		limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil {
			return service.TelemetryEvidenceRequest{}, service.ErrTelemetryEvidenceInvalid
		}
	}
	request := service.TelemetryEvidenceRequest{Start: start, End: end, Keys: keys, Limit: limit}
	if err := service.ValidateTelemetryEvidenceRequest(request); err != nil {
		return service.TelemetryEvidenceRequest{}, err
	}
	return request, nil
}

func (*DeviceApi) BrokerTelemetryEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token := strings.TrimSpace(c.GetHeader("x-token"))
	value, ok := c.Get("claims")
	claims, claimsOK := value.(*utils.UserClaims)
	if token == "" || !ok || !claimsOK || claims == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not_authorized"})
		return
	}
	request, err := parseTelemetryEvidenceQuery(c.Request.URL.Query())
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_argument"})
		return
	}
	data, err := service.GroupApp.Device.BrokerTelemetryEvidence(c.Request.Context(), claims, token, c.Param("id"), request)
	if err != nil {
		status, category := http.StatusServiceUnavailable, "temporarily_unavailable"
		switch {
		case errors.Is(err, service.ErrBrokerSemanticUnauthorized):
			status, category = http.StatusUnauthorized, "not_authorized"
		case errors.Is(err, service.ErrBrokerSemanticUnsupported), errors.Is(err, service.ErrTelemetryEvidenceUnsupported):
			status, category = http.StatusConflict, "unsupported_capability"
		case errors.Is(err, service.ErrBrokerSemanticNotFound):
			status, category = http.StatusNotFound, "not_found"
		case errors.Is(err, service.ErrTelemetryEvidenceInvalid):
			status, category = http.StatusBadRequest, "invalid_argument"
		}
		c.AbortWithStatusJSON(status, gin.H{"error": category})
		return
	}
	c.JSON(http.StatusOK, data)
}

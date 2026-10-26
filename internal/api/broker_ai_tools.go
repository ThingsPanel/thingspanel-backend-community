package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"project/internal/brokeraitools"
	"project/internal/brokercommands"
	"project/internal/brokercontract"
	"project/internal/brokersemantic"
	"project/internal/brokerstate"
	"project/internal/service"
)

const (
	maxBrokerAIToolIssuerBody = 16 << 10
	maxBrokerAIToolBody       = 128 << 10
	maxBrokerAIToolToken      = 8192
)

// RegisterBrokerAIToolRoutes registers the credential issuer/revoker and the
// four fixed tool operations. The caller must invoke it before global
// OperationLogs and before the v1 JWTAuth middleware group: OperationLogs
// captures complete POST bodies, while delegated bearer tokens are not JWTs.
func RegisterBrokerAIToolRoutes(router gin.IRoutes) {
	router.POST("/api/v1/broker-ai/delegations", issueBrokerAIToolDelegation)
	router.POST("/api/v1/broker-ai/delegations/:id/revoke", revokeBrokerAIToolDelegation)
	router.POST("/api/v1/broker-ai/tools/devices.describe", executeBrokerAITool("devices.describe"))
	router.POST("/api/v1/broker-ai/tools/telemetry.query", executeBrokerAITool("telemetry.query"))
	router.POST("/api/v1/broker-ai/tools/commands.preview", executeBrokerAITool("commands.preview"))
	router.POST("/api/v1/broker-ai/tools/commands.getResult", executeBrokerAITool("commands.getResult"))
}

func brokerAIToolError(c *gin.Context, status int, category string) {
	c.AbortWithStatusJSON(status, gin.H{"error": category})
}

func brokerAIToolStatus(err error) (int, string) {
	switch {
	case errors.Is(err, brokeraitools.ErrInvalid), errors.Is(err, brokersemantic.ErrInvalid),
		errors.Is(err, brokersemantic.ErrResponse), errors.Is(err, brokersemantic.ErrCommandInvalid),
		errors.Is(err, brokercommands.ErrInvalid), errors.Is(err, brokercommands.ErrRejected),
		errors.Is(err, service.ErrTelemetryEvidenceInvalid):
		return http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, brokeraitools.ErrUnauthorized), errors.Is(err, service.ErrBrokerSemanticUnauthorized):
		return http.StatusUnauthorized, "not_authorized"
	case errors.Is(err, brokeraitools.ErrBudget), errors.Is(err, brokeraitools.ErrCapacity), errors.Is(err, brokercommands.ErrCapacity):
		return http.StatusTooManyRequests, "budget_exceeded"
	case errors.Is(err, service.ErrBrokerSemanticUnsupported), errors.Is(err, brokersemantic.ErrCommandUnsupported),
		errors.Is(err, brokersemantic.ErrSemanticUnavailable), errors.Is(err, service.ErrTelemetryEvidenceUnsupported):
		return http.StatusConflict, "unsupported_capability"
	case errors.Is(err, brokercommands.ErrNotFound), errors.Is(err, service.ErrBrokerSemanticNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, brokercommands.ErrFenced), errors.Is(err, brokerstate.ErrFenced),
		errors.Is(err, brokercommands.ErrConflict):
		return http.StatusConflict, "stale_state"
	default:
		return http.StatusServiceUnavailable, "temporarily_unavailable"
	}
}

func singleHeader(c *gin.Context, name string) (string, bool) {
	values := c.Request.Header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func brokerAIHumanToken(c *gin.Context) (string, bool) {
	if len(c.Request.Header.Values("Authorization")) != 0 || len(c.Request.Header.Values("x-api-key")) != 0 {
		return "", false
	}
	token, ok := singleHeader(c, "x-token")
	if !ok || token == "" || len(token) > maxBrokerAIToolToken || strings.TrimSpace(token) != token {
		return "", false
	}
	return token, true
}

func brokerAIDelegatedToken(c *gin.Context) (string, bool) {
	if len(c.Request.Header.Values("x-token")) != 0 || len(c.Request.Header.Values("x-api-key")) != 0 {
		return "", false
	}
	header, ok := singleHeader(c, "Authorization")
	if !ok {
		return "", false
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" || len(parts[1]) > maxBrokerAIToolToken {
		return "", false
	}
	return parts[1], true
}

func decodeBrokerAIToolJSON(c *gin.Context, limit int64, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(bytes.TrimSpace(body)) == 0 || brokercontract.ValidateUniqueJSON(body) != nil {
		brokerAIToolError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		brokerAIToolError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		brokerAIToolError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func validateEmptyRevokeBody(c *gin.Context) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		brokerAIToolError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return true
	}
	if !bytes.Equal(trimmed, []byte("{}")) || brokercontract.ValidateUniqueJSON(trimmed) != nil {
		brokerAIToolError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func issueBrokerAIToolDelegation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token, ok := brokerAIHumanToken(c)
	if !ok {
		brokerAIToolError(c, http.StatusUnauthorized, "not_authorized")
		return
	}
	var request service.BrokerAIToolGrantRequest
	if !decodeBrokerAIToolJSON(c, maxBrokerAIToolIssuerBody, &request) {
		return
	}
	issued, err := service.GroupApp.Device.IssueBrokerAIToolDelegation(c.Request.Context(), token, request)
	if err != nil {
		status, category := brokerAIToolStatus(err)
		brokerAIToolError(c, status, category)
		return
	}
	// This response is intended for the trusted server-side tool adapter. The
	// token must never be forwarded into a model tool schema or model output.
	c.JSON(http.StatusCreated, issued)
}

func revokeBrokerAIToolDelegation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token, ok := brokerAIHumanToken(c)
	if !ok {
		brokerAIToolError(c, http.StatusUnauthorized, "not_authorized")
		return
	}
	if !validateEmptyRevokeBody(c) {
		return
	}
	if err := service.GroupApp.Device.RevokeBrokerAIToolDelegation(c.Request.Context(), token, c.Param("id")); err != nil {
		status, category := brokerAIToolStatus(err)
		brokerAIToolError(c, status, category)
		return
	}
	c.Status(http.StatusNoContent)
}

func executeBrokerAITool(operation string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		token, ok := brokerAIDelegatedToken(c)
		if !ok {
			brokerAIToolError(c, http.StatusUnauthorized, "not_authorized")
			return
		}
		var input service.BrokerAIToolInput
		if !decodeBrokerAIToolJSON(c, maxBrokerAIToolBody, &input) {
			return
		}
		output, err := service.GroupApp.Device.ExecuteBrokerAITool(c.Request.Context(), token, operation, input)
		if err != nil {
			status, category := brokerAIToolStatus(err)
			brokerAIToolError(c, status, category)
			return
		}
		c.JSON(http.StatusOK, output)
	}
}

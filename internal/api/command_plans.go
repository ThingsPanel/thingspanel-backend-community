package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"project/internal/brokercommands"
	"project/internal/brokercontract"
	"project/internal/brokersemantic"
	"project/internal/brokerstate"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/utils"
)

const maxCommandPlanBody = 128 << 10

type CommandPlanApi struct{}

func decodeCommandPlanBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCommandPlanBody)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 || brokercontract.ValidateUniqueJSON(body) != nil {
		commandPlanError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		commandPlanError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		commandPlanError(c, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func commandPlanError(c *gin.Context, status int, category string) {
	c.AbortWithStatusJSON(status, gin.H{"error": category})
}

func commandPlanStatus(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrBrokerSemanticUnauthorized):
		return http.StatusUnauthorized, "not_authorized"
	case errors.Is(err, service.ErrBrokerSemanticUnsupported):
		return http.StatusConflict, "unsupported_capability"
	case errors.Is(err, brokercommands.ErrFenced), errors.Is(err, brokerstate.ErrFenced):
		return http.StatusConflict, "stale_state"
	case errors.Is(err, brokersemantic.ErrInvalid), errors.Is(err, brokersemantic.ErrResponse):
		return http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, brokersemantic.ErrCommandInvalid):
		return http.StatusBadRequest, "invalid_argument"
	case errors.Is(err, brokersemantic.ErrCommandUnsupported):
		return http.StatusConflict, "unsupported_capability"
	case errors.Is(err, brokersemantic.ErrSemanticUnavailable):
		return http.StatusConflict, "unsupported_capability"
	case errors.Is(err, brokercommands.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, brokercommands.ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, brokercommands.ErrInvalid), errors.Is(err, brokercommands.ErrRejected):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusServiceUnavailable, "temporarily_unavailable"
	}
}

func commandPlanIdentity(c *gin.Context) (*utils.UserClaims, string, bool) {
	claims, ok := c.Get("claims")
	userClaims, typed := claims.(*utils.UserClaims)
	token := strings.TrimSpace(c.GetHeader("x-token"))
	if !ok || !typed || userClaims == nil || token == "" {
		commandPlanError(c, http.StatusUnauthorized, "not_authorized")
		return nil, "", false
	}
	return userClaims, token, true
}

// Preview creates a frozen proposal only; it does not authorize or publish.
func (CommandPlanApi) Preview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	claims, token, ok := commandPlanIdentity(c)
	if !ok {
		return
	}
	var req model.PutMessageForCommand
	if !decodeCommandPlanBody(c, &req) {
		return
	}
	preview, err := service.GroupApp.CommandData.PreviewCommandPlan(c.Request.Context(), claims, token, &req)
	if err != nil {
		status, category := commandPlanStatus(err)
		commandPlanError(c, status, category)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// Authorize records a human confirmation for an exact preview hash.
func (CommandPlanApi) Authorize(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	claims, token, ok := commandPlanIdentity(c)
	if !ok {
		return
	}
	var use service.CommandPlanUse
	if !decodeCommandPlanBody(c, &use) {
		return
	}
	authorizationID, err := service.GroupApp.CommandData.AuthorizeCommandPlan(c.Request.Context(), claims, token, c.Param("plan_id"), use)
	if err != nil {
		status, category := commandPlanStatus(err)
		commandPlanError(c, status, category)
		return
	}
	c.JSON(http.StatusOK, gin.H{"authorization_id": authorizationID, "status": "authorized"})
}

// Submit consumes the authorization and creates one durable command intent.
func (CommandPlanApi) Submit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	claims, token, ok := commandPlanIdentity(c)
	if !ok {
		return
	}
	var use service.CommandPlanUse
	if !decodeCommandPlanBody(c, &use) {
		return
	}
	messageID, err := service.GroupApp.CommandData.SubmitCommandPlan(c.Request.Context(), claims, token, c.Param("plan_id"), use)
	if err != nil {
		status, category := commandPlanStatus(err)
		commandPlanError(c, status, category)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message_id": messageID, "status": "accepted"})
}

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type defaultPolicyPutBody struct {
	NativeGroupID   *string `json:"nativeGroupId"`
	ExpectedVersion *int64  `json:"expectedVersion"`
}

type defaultPolicyEnvelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (*NotificationGroupApi) GetTenantDefaultPolicy(c *gin.Context) {
	claims, ok := defaultPolicyClaims(c)
	if !ok {
		return
	}
	if len(c.Request.URL.Query()) != 0 {
		writeDefaultPolicyError(c, http.StatusBadRequest, 100002, "invalid query")
		return
	}
	c.Header("Cache-Control", "no-store")
	view, err := service.GetTenantDefaultPolicy(c.Request.Context(), claims.TenantID)
	if err != nil {
		writeDefaultPolicyServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, defaultPolicyEnvelope{Code: 200, Message: "Success", Data: view})
}

func (*NotificationGroupApi) PutTenantDefaultPolicy(c *gin.Context) {
	claims, ok := defaultPolicyClaims(c)
	if !ok {
		return
	}
	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		writeDefaultPolicyError(c, http.StatusUnsupportedMediaType, 100002, "application/json is required")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil || !validDefaultPolicyBody(payload) {
		writeDefaultPolicyError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var body defaultPolicyPutBody
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.NativeGroupID == nil || body.ExpectedVersion == nil || *body.ExpectedVersion < 0 || len(*body.NativeGroupID) > 128 || strings.TrimSpace(*body.NativeGroupID) != *body.NativeGroupID {
		writeDefaultPolicyError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	view, err := service.SetTenantDefaultPolicy(c.Request.Context(), claims.TenantID, *body.NativeGroupID, *body.ExpectedVersion)
	if err != nil {
		writeDefaultPolicyServiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, defaultPolicyEnvelope{Code: 200, Message: "Success", Data: view})
}

func validDefaultPolicyBody(payload []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || (key != "nativeGroupId" && key != "expectedVersion") || seen[key] {
			return false
		}
		seen[key] = true
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim('}') && decoder.Decode(new(any)) == io.EOF && seen["nativeGroupId"] && seen["expectedVersion"]
}

func defaultPolicyClaims(c *gin.Context) (*utils.UserClaims, bool) {
	if strings.TrimSpace(c.GetHeader("x-token")) == "" {
		writeDefaultPolicyError(c, http.StatusUnauthorized, 200001, "authenticated user session required")
		return nil, false
	}
	value, exists := c.Get("claims")
	claims, ok := value.(*utils.UserClaims)
	if !exists || !ok || claims == nil || strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.TenantID) == "" {
		writeDefaultPolicyError(c, http.StatusUnauthorized, 200001, "authenticated tenant session required")
		return nil, false
	}
	if authority := strings.ToUpper(strings.TrimSpace(claims.Authority)); authority != "TENANT_ADMIN" && authority != "SYS_ADMIN" {
		writeDefaultPolicyError(c, http.StatusForbidden, 201001, "tenant administrator required")
		return nil, false
	}
	claims.TenantID = strings.TrimSpace(claims.TenantID)
	return claims, true
}

func writeDefaultPolicyServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDefaultPolicyInvalid):
		writeDefaultPolicyError(c, http.StatusNotFound, 100004, "notification policy unavailable")
	case errors.Is(err, service.ErrDefaultPolicyConflict):
		writeDefaultPolicyError(c, http.StatusConflict, 201002, "default policy changed; refresh and retry")
	default:
		writeDefaultPolicyError(c, http.StatusServiceUnavailable, 101001, "notification policy unavailable")
	}
}

func writeDefaultPolicyError(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, defaultPolicyEnvelope{Code: code, Message: message})
}

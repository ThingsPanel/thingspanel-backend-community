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

type nativePublishRequestBody struct {
	Operation            *string `json:"operation,omitempty"`
	NativeGroupID        *string `json:"nativeGroupId"`
	GroupRevision        *int64  `json:"groupRevision,omitempty"`
	Name                 *string `json:"name,omitempty"`
	ExpectedRouteVersion *int64  `json:"expectedRouteVersion"`
}

type nativePublishEnvelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// GetNativePublishStatus returns the authenticated tenant's current alarm
// route for a deterministic legacy alias. It never accepts a tenant ID from
// the query string.
func (*NotificationGroupApi) GetNativePublishStatus(c *gin.Context) {
	claims, ok := nativePublishClaims(c)
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	if len(query) != 1 || len(query["nativeGroupId"]) != 1 || strings.TrimSpace(query.Get("nativeGroupId")) == "" || len(query.Get("nativeGroupId")) > 128 {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid native group query")
		return
	}
	c.Header("Cache-Control", "no-store")
	status, err := service.GetNativePublishStatus(c.Request.Context(), claims.TenantID, query.Get("nativeGroupId"))
	if err != nil {
		writeNativePublishServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, nativePublishEnvelope{Code: 200, Message: "Success", Data: status})
}

// ApplyNativePublish explicitly publishes or stops one tenant-owned Core group
// as a legacy selector alias for future community alarms.
func (*NotificationGroupApi) ApplyNativePublish(c *gin.Context) {
	claims, ok := nativePublishClaims(c)
	if !ok {
		return
	}
	keys := c.Request.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "Idempotency-Key is required")
		return
	}
	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		writeNativePublishError(c, http.StatusUnsupportedMediaType, 100002, "application/json is required")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	payload, readErr := io.ReadAll(c.Request.Body)
	if readErr != nil {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	if !validNativePublishObject(payload) {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var body nativePublishRequestBody
	if err := decoder.Decode(&body); err != nil {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	if body.NativeGroupID == nil || strings.TrimSpace(*body.NativeGroupID) == "" || len(*body.NativeGroupID) > 128 || body.ExpectedRouteVersion == nil || *body.ExpectedRouteVersion < 0 {
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid request body")
		return
	}
	operation := "publish"
	if body.Operation != nil {
		operation = *body.Operation
	}
	request := service.NativePublishRequest{Operation: operation, TenantID: claims.TenantID, NativeGroupID: *body.NativeGroupID,
		ExpectedRouteVersion: *body.ExpectedRouteVersion, IdempotencyKey: keys[0]}
	switch operation {
	case "publish":
		if body.GroupRevision == nil || *body.GroupRevision < 1 || body.Name == nil {
			writeNativePublishError(c, http.StatusBadRequest, 100002, "publish requires name and groupRevision")
			return
		}
		request.GroupRevision = *body.GroupRevision
		request.Name = *body.Name
	case "unpublish":
		if body.GroupRevision != nil || body.Name != nil {
			writeNativePublishError(c, http.StatusBadRequest, 100002, "unpublish accepts only nativeGroupId and expectedRouteVersion")
			return
		}
	default:
		writeNativePublishError(c, http.StatusBadRequest, 100002, "unsupported operation")
		return
	}
	status, replayed, err := service.ApplyNativePublish(c.Request.Context(), request)
	if err != nil {
		writeNativePublishServiceError(c, err)
		return
	}
	code := http.StatusCreated
	if replayed || operation == "unpublish" {
		code = http.StatusOK
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(code, nativePublishEnvelope{Code: 200, Message: "Success", Data: status})
}

func validNativePublishObject(payload []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false
	}
	allowed := map[string]struct{}{"operation": {}, "nativeGroupId": {}, "groupRevision": {}, "name": {}, "expectedRouteVersion": {}}
	seen := make(map[string]struct{}, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		if _, ok := allowed[key]; !ok {
			return false
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	return true
}

func nativePublishClaims(c *gin.Context) (*utils.UserClaims, bool) {
	if strings.TrimSpace(c.GetHeader("x-token")) == "" {
		writeNativePublishError(c, http.StatusUnauthorized, 200001, "authenticated user session required")
		return nil, false
	}
	value, exists := c.Get("claims")
	claims, valid := value.(*utils.UserClaims)
	if !exists || !valid || claims == nil || strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.TenantID) == "" {
		writeNativePublishError(c, http.StatusUnauthorized, 200001, "authenticated user session required")
		return nil, false
	}
	authority := strings.ToUpper(strings.TrimSpace(claims.Authority))
	if authority != "TENANT_ADMIN" && authority != "SYS_ADMIN" {
		writeNativePublishError(c, http.StatusForbidden, 201001, "tenant administrator required")
		return nil, false
	}
	claims.TenantID = strings.TrimSpace(claims.TenantID)
	return claims, true
}

func writeNativePublishServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNativePublishInvalid):
		writeNativePublishError(c, http.StatusBadRequest, 100002, "invalid publish request")
	case errors.Is(err, service.ErrNativePublishConflict):
		writeNativePublishError(c, http.StatusConflict, 201002, "route changed; refresh status and retry")
	default:
		writeNativePublishError(c, http.StatusServiceUnavailable, 101001, "notification route unavailable")
	}
}

func writeNativePublishError(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, nativePublishEnvelope{Code: code, Message: message})
}

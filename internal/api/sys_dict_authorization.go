package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"project/internal/model"
	"project/internal/service"
)

// RegisterDictionaryAuthorizationRoutes must be mounted before operation/body
// logging. This is a purpose-limited assertion from the existing permission
// owner; it is not a dictionary data API and never accepts a user bearer token
// as a substitute for the dedicated deployment credential.
func RegisterDictionaryAuthorizationRoutes(router *gin.Engine) {
	router.POST("/internal/platformconfig/authorize", handleDictionaryAuthorization)
}

func handleDictionaryAuthorization(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	configured := os.Getenv("TP_PLATFORMCONFIG_AUTHORIZATION_TOKEN")
	presented := c.GetHeader("X-TP-Service-Token")
	if os.Getenv("TP_PLATFORMCONFIG_OWNER") != "encore" || len(configured) < 32 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"errorCode": "DICTIONARY_AUTHORIZATION_UNAVAILABLE"})
		return
	}
	if len(configured) != len(presented) || subtle.ConstantTimeCompare([]byte(configured), []byte(presented)) != 1 {
		c.JSON(http.StatusForbidden, gin.H{"errorCode": "DICTIONARY_AUTHORIZATION_DENIED"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request model.DictionaryAuthorizationRequest
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"errorCode": "INVALID_DICTIONARY_AUTHORIZATION"})
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"errorCode": "INVALID_DICTIONARY_AUTHORIZATION"})
		return
	}
	result, err := service.AuthorizeCurrentDictionaryOperation(c.Request.Context(), request)
	if errors.Is(err, service.ErrDictionaryPermissionDenied) {
		c.JSON(http.StatusForbidden, gin.H{"errorCode": "DICTIONARY_AUTHORIZATION_DENIED"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"errorCode": "DICTIONARY_AUTHORIZATION_UNAVAILABLE"})
		return
	}
	c.JSON(http.StatusOK, result)
}
